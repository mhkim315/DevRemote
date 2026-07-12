// Package v0_144_1 implements the T0 contract.AgentAdapter for Codex CLI
// version 0.144.1. It reads the Codex session JSONL
// (~/.codex/sessions/.../rollout-*.jsonl), normalises events into the closed
// common vocabulary, and surfaces bound approvals.  It does NOT use the
// app-server JSON-RPC transport; thread/session correlation was not proven in R1
// for ordinary interactive TUI sessions.
//
// Supported version: 0.144.1 (confirmed by `codex --version` in R1 evidence).
// Every session_meta in a read batch is checked; a single missing / mismatched /
// malformed cli_version forces the ENTIRE batch to EventUnknown + degraded.
//
// Seq / cursor: Seq is a position-based ordinal (monotonically increasing
// append order).  The cursor carries the set of already-emitted record IDs
// (content-hash hex) so re-reads are suppressed: same records ⇒ same IDs ⇒
// deduped against the cursor's seen-set.  The cursor is a bounded sliding
// window — when the ID set exceeds MaxCursorBytes the oldest IDs are dropped,
// which may re-emit very old records but never silently lose new ones.
//
// Correlation: unavailable (R1).  DiscoverSessions returns nil.
package v0_144_1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

const supportedCodexVersion = "0.144.1"

// Adapter implements contract.AgentAdapter for Codex CLI 0.144.1.
type Adapter struct{ failRead bool }

var _ contract.AgentAdapter = (*Adapter)(nil)

// ── Descriptor ──

func (a *Adapter) Descriptor() contract.AgentAdapterDescriptor {
	return contract.AgentAdapterDescriptor{
		Name:              "codex",
		Provider:          "Codex CLI",
		ContractVersion:   contract.ContractVersion,
		SupportedVersions: []string{supportedCodexVersion},
		Capabilities: []contract.AdapterCapability{
			contract.CapEvents,
			contract.CapStatus,
			contract.CapApprovalDetection,
			contract.CapIncrementalRead,
			contract.CapProcessDetection,
		},
	}
}

// ── Detect ──

func (a *Adapter) Detect(_ context.Context, session contract.SessionContext) (contract.AgentIdentity, error) {
	if session.ProcessName == "" && session.CWD == "" {
		return contract.AgentIdentity{Kind: "unknown", Confidence: 0.1}, nil
	}
	confidence := 0.1
	kind := "unknown"
	procLower := strings.ToLower(session.ProcessName)
	switch procLower {
	case "codex":
		kind = "codex"
		confidence = 0.6
	default:
		if procLower != "" && strings.Contains(procLower, "codex") {
			kind = "codex"
			confidence = 0.5
		}
	}
	if strings.Contains(session.CWD, ".codex") {
		confidence += 0.15
	}
	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < 0.5 {
		kind = "unknown"
	}
	return contract.AgentIdentity{Kind: kind, DisplayName: "Codex", Confidence: confidence}, nil
}

// ── DiscoverSessions ──

func (a *Adapter) DiscoverSessions(_ context.Context, in contract.DiscoveryInput) ([]contract.DiscoveredSession, error) {
	return nil, nil
}

// ── Types ──

type codexRecord struct {
	Timestamp string         `json:"timestamp"`
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload"`
}

// ── Cursor encoding ──
//
// The cursor is an opaque comma-separated list of already-emitted content-hash
// IDs, e.g. "a1b2c3d4,e5f6a7b8,9c0d1e2f".  An empty cursor means "start of
// stream".  The next Seq to assign is derived from len(cursorIDs) — i.e. the
// total number of records emitted so far.
//
// When the cursor would exceed MaxCursorBytes we keep only the most recent
// entries so the cursor stays bounded.  A truncated cursor may allow a very old
// record to be re-emitted; this is the contract's intended "compact
// high-water-mark" trade-off.

// parseCursorIDs splits a cursor string into a deduplication set.  Returns nil
// for an empty cursor.
func parseCursorIDs(cursor contract.Cursor) map[string]bool {
	if cursor.IsEmpty() {
		return nil
	}
	parts := strings.Split(string(cursor), ",")
	m := make(map[string]bool, len(parts))
	for _, p := range parts {
		if p != "" {
			m[p] = true
		}
	}
	return m
}

// encodeCursor builds a bounded cursor from old seen-IDs plus new IDs.  Oldest
// entries are dropped first when the encoded size would exceed MaxCursorBytes.
func encodeCursor(old map[string]bool, newIDs []string) contract.Cursor {
	// Collect all IDs: old first, then new.  Order matters — old IDs are
	// dropped first on overflow so recent IDs survive.
	all := make([]string, 0, len(old)+len(newIDs))
	for id := range old {
		all = append(all, id)
	}
	all = append(all, newIDs...)

	// Build the cursor string, dropping oldest entries until it fits.
	var cur string
	for i := len(all) - 1; i >= 0; i-- {
		cand := all[i]
		if cand == "" {
			continue
		}
		if cur == "" {
			cur = cand
		} else if len(cur)+1+len(cand) <= contract.MaxCursorBytes {
			cur = cand + "," + cur
		} else {
			break // cursor full — oldest entries dropped
		}
	}
	return contract.Cursor(cur)
}

// cursorBasePos returns the next Seq to assign from a cursor (0 for empty).
func cursorBasePos(cursor contract.Cursor) int {
	if cursor.IsEmpty() {
		return 0
	}
	return len(parseCursorIDs(cursor))
}

// ── NormalizeEvent ──

func (a *Adapter) NormalizeEvent(_ context.Context, rec contract.RawRecord) (contract.AgentEvent, contract.DegradedInfo) {
	return normalizeCodexEvent(rec, "s", rec.Source, false)
}

func normalizeCodexEvent(rec contract.RawRecord, sessionID string, src contract.AgentEventSource, versionFailed bool) (contract.AgentEvent, contract.DegradedInfo) {
	prov := contract.ProvenanceNativeLog
	if contract.IsKnownProvenance(rec.Provenance) {
		prov = rec.Provenance
	}
	src = sourceOrDefault(src)
	id := hashBytesID(rec.Bytes)

	var cr codexRecord
	if err := json.Unmarshal(rec.Bytes, &cr); err != nil {
		return unknownRec(rec.Bytes, sessionID, prov, src, "unparseable Codex record")
	}
	if cr.Type == "" {
		return unknownRec(rec.Bytes, sessionID, prov, src, "missing type in Codex record")
	}

	if cr.Type == "session_meta" {
		v := codexPayloadStr(cr, "cli_version")
		if v != supportedCodexVersion {
			return unknownRec(rec.Bytes, sessionID, prov, src, "unsupported Codex version: "+safeVersionDiag(v))
		}
	}
	if versionFailed {
		return unknownRec(rec.Bytes, sessionID, prov, src, "version mismatch in batch")
	}

	et, conf := classifyCodexRecord(cr)
	ts := parseTimestamp(cr.Timestamp)
	// Seq=0 is a placeholder — ReadEvents assigns the real position-based Seq.
	seq := int64(0)

	degraded := false
	var reasons []string
	if et == agent.EventUnknown && cr.Type != "turn_context" {
		degraded = true
		reasons = append(reasons, "unknown Codex record type: "+safeDiag(cr.Type))
	}

	ev := contract.AgentEvent{
		ID: id, SessionID: sessionID, AgentKind: "codex",
		Type: et, Seq: seq, Timestamp: ts,
		Text: safeCodexText(cr), Confidence: conf,
		Source: src, Provenance: string(prov),
		Metadata: boundedCodexMetadata(cr),
	}

	if et == agent.EventApprovalRequested || et == agent.EventApprovalResolved {
		aid := codexPayloadStr(cr, "approval_id")
		if aid != "" {
			ev.ApprovalID = aid
		} else {
			ev.Type = agent.EventUnknown
			ev.Confidence = 0.25
			degraded = true
			reasons = append(reasons, string(et)+" without approval_id")
		}
	}

	deg := contract.OK()
	if degraded {
		deg = contract.Degrade(strings.Join(reasons, "; "))
	}
	return ev, deg
}

func classifyCodexRecord(cr codexRecord) (contract.AgentEventType, float64) {
	switch cr.Type {
	case "session_meta":
		return agent.EventAgentStarted, 0.9
	case "event_msg":
		pt := codexPayloadStr(cr, "type")
		switch pt {
		case "task_started":
			return agent.EventAgentStarted, 0.85
		case "waiting_for_approval":
			return agent.EventApprovalRequested, 0.9
		case "approval_resolved":
			return agent.EventApprovalResolved, 0.9
		default:
			return agent.EventUnknown, 0.3
		}
	case "response_item":
		role := codexPayloadStr(cr, "role")
		switch role {
		case "user":
			return agent.EventUserMessage, 0.85
		case "assistant":
			return agent.EventAssistantMessage, 0.85
		default:
			return agent.EventUnknown, 0.3
		}
	case "turn_context":
		return agent.EventUnknown, 0.3
	default:
		return agent.EventUnknown, 0.3
	}
}

// ── ReadEvents ──

func (a *Adapter) ReadEvents(_ context.Context, in contract.ReadInput) (contract.ReadResult, error) {
	if a.failRead {
		return contract.ReadResult{Degraded: contract.Degrade("codex read failure")}, nil
	}

	sessionID := in.Session.SessionID
	if sessionID == "" {
		sessionID = "s"
	}

	degraded := false
	var diags []string

	if err := contract.ValidateCursor(in.Cursor); err != nil {
		return contract.ReadResult{Degraded: contract.Degrade("invalid cursor: " + err.Error())}, nil
	}

	records, truncBatch := contract.BoundBatch(in.Records)
	if truncBatch {
		degraded = true
		diags = append(diags, "batch truncated at bound")
	}

	// Parse cursor: cross-read dedup set + base position.
	cursorSeen := parseCursorIDs(in.Cursor)
	basePos := 0
	if cursorSeen != nil {
		basePos = len(cursorSeen)
	}

	// ── Version gate: scan EVERY session_meta ──
	versionOK := false
	versionFailed := false
	for _, rec := range records {
		if !contract.AcceptRecord(rec) {
			continue
		}
		var cr codexRecord
		if err := json.Unmarshal(rec.Bytes, &cr); err != nil {
			continue
		}
		if cr.Type != "session_meta" {
			continue
		}
		v := codexPayloadStr(cr, "cli_version")
		if v == supportedCodexVersion {
			versionOK = true
		} else {
			versionFailed = true
			degraded = true
			diags = append(diags, "unsupported Codex version: "+safeVersionDiag(v))
		}
	}
	if !versionOK || versionFailed {
		versionFailed = true
	}

	limit := contract.EffectiveReadLimit(in.MaxEvents)
	batchSeen := map[string]bool{}
	var newIDs []string
	var out []contract.AgentEvent
	truncated := false

	for _, rec := range records {
		if !contract.AcceptRecord(rec) {
			degraded = true
			diags = append(diags, "oversized record skipped")
			continue
		}
		ev, recDeg := normalizeCodexEvent(rec, sessionID, rec.Source, versionFailed)
		if recDeg.Degraded {
			degraded = true
			if recDeg.Reason != "" {
				diags = append(diags, recDeg.Reason)
			}
			for _, d := range recDeg.Diagnostics {
				if d != "" {
					diags = append(diags, d)
				}
			}
		}
		if ev.ID == "" {
			continue
		}

		// Dedup: cross-read (cursor) + within-batch.
		if cursorSeen != nil && cursorSeen[ev.ID] {
			continue
		}
		if batchSeen[ev.ID] {
			continue
		}

		if len(out) >= limit {
			truncated = true
			break
		}

		batchSeen[ev.ID] = true
		// Position-based Seq: monotonically increasing append order.
		ev.Seq = int64(basePos + len(out))
		out = append(out, ev)
		newIDs = append(newIDs, ev.ID)
	}

	if truncated {
		degraded = true
		diags = append(diags, "event limit truncated")
	}

	// Cap diagnostics.
	if len(diags) > contract.MaxDiagnostics {
		diags = diags[:contract.MaxDiagnostics]
	}

	deg := contract.OK()
	if degraded {
		reason := strings.Join(diags, "; ")
		if len(reason) > contract.MaxDiagnosticBytes {
			reason = reason[:contract.MaxDiagnosticBytes]
		}
		deg = contract.Degrade(reason)
	}

	// Build cursor: old seen-IDs + new IDs, bounded to MaxCursorBytes.
	nextCursor := encodeCursor(cursorSeen, newIDs)

	return contract.ReadResult{
		Events:     out,
		NextCursor: nextCursor,
		Degraded:   deg,
	}, nil
}

// ── DetectApproval ──

func (a *Adapter) DetectApproval(_ context.Context, events []contract.AgentEvent) ([]contract.AgentApproval, error) {
	var out []contract.AgentApproval
	for _, e := range events {
		if !contract.SafeApprovalGate(e) {
			continue
		}
		out = append(out, contract.AgentApproval{
			ID: e.ApprovalID, SessionID: e.SessionID, AgentKind: e.AgentKind,
			Kind: "approval", Status: "pending", Prompt: "approval requested",
			Source: e.Source, Confidence: e.Confidence, CreatedAt: e.Timestamp,
		})
	}
	return out, nil
}

// ── GetStatus ──

func (a *Adapter) GetStatus(_ context.Context, in contract.StatusInput) (contract.StatusResult, error) {
	if len(in.Evidence) == 0 {
		return contract.StatusResult{
			Status: agent.StatusUnknown, Provenance: contract.ProvenanceUnknown,
			Degraded: contract.Degrade("no status evidence"),
		}, nil
	}
	return contract.ResolveStatus(in.Evidence), nil
}

// ── Helpers ──

func hashBytesID(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

func unknownRec(raw []byte, sessionID string, prov contract.Provenance, src contract.AgentEventSource, reason string) (contract.AgentEvent, contract.DegradedInfo) {
	ev := contract.SafeEvent(contract.AgentEvent{
		ID: hashBytesID(raw), SessionID: sessionID, AgentKind: "codex",
		Type: agent.EventUnknown, Seq: 0,
		Timestamp: timeZero, Confidence: 0.2,
		Source: src, Provenance: string(prov),
	})
	return ev, contract.Degrade(reason)
}

var timeZero = time.Time{}

func parseTimestamp(ts string) time.Time {
	if ts == "" {
		return timeZero
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return timeZero
	}
	return t
}

func codexPayloadStr(cr codexRecord, key string) string {
	if cr.Payload == nil {
		return ""
	}
	v, ok := cr.Payload[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func safeCodexText(cr codexRecord) string {
	if cr.Type == "response_item" || cr.Type == "turn_context" {
		return ""
	}
	text := codexPayloadStr(cr, "message")
	if text == "" {
		text = codexPayloadStr(cr, "text")
	}
	if text != "" && contract.ContainsSensitive(text) {
		return ""
	}
	if len(text) > 256 {
		text = text[:256]
	}
	return text
}

func boundedCodexMetadata(cr codexRecord) map[string]string {
	m := map[string]string{}
	if cr.Payload == nil {
		return m
	}
	if s := codexPayloadStr(cr, "cli_version"); s != "" {
		m["cli_version"] = boundStr(s, contract.MaxMetadataValueBytes)
	}
	if s := codexPayloadStr(cr, "turn_id"); s != "" && !contract.ContainsSensitive(s) {
		m["turn_id"] = boundStr(s, contract.MaxMetadataValueBytes)
	}
	if s := codexPayloadStr(cr, "session_id"); s != "" && !contract.ContainsSensitive(s) {
		m["session_id"] = boundStr(s, contract.MaxMetadataValueBytes)
	}
	if s := codexPayloadStr(cr, "model_provider"); s != "" && !contract.ContainsSensitive(s) {
		m["model_provider"] = boundStr(s, contract.MaxMetadataValueBytes)
	}
	if s := codexPayloadStr(cr, "resolution"); s != "" {
		m["resolution"] = boundStr(s, contract.MaxMetadataValueBytes)
	}
	return m
}

func sourceOrDefault(s contract.AgentEventSource) contract.AgentEventSource {
	if contract.IsKnownEventSource(s) {
		return s
	}
	return agent.SourceJSONL
}

func boundStr(s string, maxBytes int) string {
	if len(s) > maxBytes {
		return s[:maxBytes]
	}
	return s
}

func safeDiag(s string) string {
	if len(s) > 80 {
		return s[:80]
	}
	return s
}

func safeVersionDiag(v string) string {
	if v == "" {
		return "<missing>"
	}
	if len(v) > 32 {
		v = v[:32]
	}
	return v
}
