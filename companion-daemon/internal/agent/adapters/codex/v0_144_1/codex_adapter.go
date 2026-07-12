// Package v0_144_1 implements the T0 contract.AgentAdapter for Codex CLI
// version 0.144.1. It reads the Codex session JSONL
// (~/.codex/sessions/.../rollout-*.jsonl), normalises events into the closed
// common vocabulary, and surfaces bound approvals.
//
// Supported version: 0.144.1 (confirmed by `codex --version` in R1 evidence).
// Every session_meta in a read batch is checked; a single missing / mismatched /
// malformed cli_version forces the ENTIRE batch to EventUnknown + degraded.
//
// Input policy: ReadEvents accepts ORDERED FULL-PREFIX snapshots only. The
// caller must re-send all records from the beginning on every read. The cursor
// encodes the absolute position of the next record to emit plus the content-hash
// anchor of the last emitted record. On re-read the anchor is validated at the
// exact expected position; any mismatch (tampered cursor, anchor loss, stream
// rotation, position/anchor inconsistency) returns 0 events + degraded.
//
// Cursor format: "<pos>:<anchor>" — pos is the absolute next Seq (decimal),
// anchor is the content-hash of the record at pos-1 (empty when pos==0).
// Cursor size is fixed at ~25 bytes regardless of event count.
//
// Version state is NOT carried in the cursor. Every ReadEvents call scans the
// current batch's session_meta records independently. A cursor claiming
// "version confirmed" without actual 0.144.1 evidence in the batch cannot
// activate typed events.
//
// Correlation: unavailable (R1). DiscoverSessions returns nil.
package v0_144_1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
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

// ── Cursor ──
//
// Format: "<pos>:<anchor>"
//
//	pos    = absolute next Seq to assign (decimal, 0 initially)
//	anchor = content-hash of the record at pos-1 (empty when pos==0)
//
// The anchor encodes the stream identity at the exact position so tampering,
// rotation, or position/anchor inconsistency is detected and fails closed.

type cursorState struct {
	nextPos int64
	anchor  string // hex content-hash, "" iff nextPos==0
}

const cursorFieldSep = ":"

func parseCursor(c contract.Cursor) (cursorState, error) {
	if c.IsEmpty() {
		return cursorState{}, nil
	}
	s := string(c)
	idx := strings.IndexByte(s, ':')
	if idx < 0 {
		return cursorState{}, errCursorSyntax
	}
	pos, err := strconv.ParseInt(s[:idx], 10, 64)
	if err != nil || pos < 0 {
		return cursorState{}, errCursorSyntax
	}
	anchor := s[idx+1:]
	// Invariant: pos==0 iff anchor is empty. pos>0 requires anchor.
	if (pos == 0) != (anchor == "") {
		return cursorState{}, errCursorSyntax
	}
	// Anchor must be valid lowercase hex (16 chars) when present.
	if anchor != "" && !isValidAnchor(anchor) {
		return cursorState{}, errCursorSyntax
	}
	return cursorState{nextPos: pos, anchor: anchor}, nil
}

var errCursorSyntax = strconv.ErrSyntax

func isValidAnchor(a string) bool {
	if len(a) != 16 {
		return false
	}
	for i := 0; i < len(a); i++ {
		c := a[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func encodeCursor(st cursorState) contract.Cursor {
	return contract.Cursor(strconv.FormatInt(st.nextPos, 10) + cursorFieldSep + st.anchor)
}

// validateCursorAnchor checks that the cursor anchor matches the record at
// position nextPos-1 in the normalized input. Returns an error string if
// validation fails (empty string on success).
func validateCursorAnchor(cur cursorState, all []normResult) string {
	if cur.nextPos == 0 {
		if cur.anchor != "" {
			return "non-empty anchor at position 0"
		}
		return ""
	}
	if cur.anchor == "" {
		return "missing anchor at non-zero position"
	}
	idx := int(cur.nextPos - 1)
	if idx >= len(all) {
		return "cursor position beyond input length"
	}
	if all[idx].ev.ID != cur.anchor {
		return "cursor anchor mismatch at position " + strconv.FormatInt(cur.nextPos-1, 10)
	}
	return ""
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

type normResult struct {
	ev  contract.AgentEvent
	deg contract.DegradedInfo
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

	degraded := false
	var reasons []string
	if et == agent.EventUnknown && cr.Type != "turn_context" {
		degraded = true
		reasons = append(reasons, "unknown Codex record type: "+safeDiag(cr.Type))
	}

	ev := contract.AgentEvent{
		ID: id, SessionID: sessionID, AgentKind: "codex",
		Type: et, Seq: 0, Timestamp: ts,
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

	// Parse cursor.
	cur, err := parseCursor(in.Cursor)
	if err != nil {
		return contract.ReadResult{Degraded: contract.Degrade("invalid cursor: " + err.Error())}, nil
	}

	// ── Version gate: validate from CURRENT batch only — no cursor authority ──
	batchVersionOK := false
	batchVersionFailed := false
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
			batchVersionOK = true
		} else {
			batchVersionFailed = true
			degraded = true
			diags = append(diags, "unsupported Codex version: "+safeVersionDiag(v))
		}
	}
	if !batchVersionOK || batchVersionFailed {
		batchVersionFailed = true
	}

	// ── Normalize all accepted records ──
	var all []normResult
	for _, rec := range records {
		if !contract.AcceptRecord(rec) {
			degraded = true
			diags = append(diags, "oversized record skipped")
			continue
		}
		ev, recDeg := normalizeCodexEvent(rec, sessionID, rec.Source, batchVersionFailed)
		all = append(all, normResult{ev, recDeg})
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
	}

	// ── Cursor anchor validation ──
	if msg := validateCursorAnchor(cur, all); msg != "" {
		degraded = true
		diags = append(diags, msg)
		deg := contract.Degrade(strings.Join(boundedDiags(diags), "; "))
		return contract.ReadResult{Degraded: deg}, nil
	}

	// ── Emit: skip pos input records, assign Seq from absolute position ──
	limit := contract.EffectiveReadLimit(in.MaxEvents)
	batchSeen := map[string]bool{}
	var out []contract.AgentEvent
	truncated := false
	processed := int64(0)

	for i := int(cur.nextPos); i < len(all); i++ {
		ev := all[i].ev
		if ev.ID == "" || batchSeen[ev.ID] {
			processed++
			continue
		}
		if len(out) >= limit {
			truncated = true
			break
		}
		batchSeen[ev.ID] = true
		ev.Seq = cur.nextPos + int64(len(out))
		out = append(out, ev)
		processed++
	}

	if truncated {
		degraded = true
		diags = append(diags, "event limit truncated")
	}

	// nextPos counts input positions consumed, not emitted events.
	// Anchor = ID of the record at position nextPos-1 (even if batch-dedup'd).
	nextPos := cur.nextPos + processed
	var newAnchor string
	if nextPos > 0 && int(nextPos)-1 < len(all) {
		newAnchor = all[nextPos-1].ev.ID
	}
	if nextPos == 0 || len(out) == 0 && cur.nextPos == nextPos {
		newAnchor = cur.anchor // unchanged
	}

	deg := contract.OK()
	if degraded {
		deg = contract.Degrade(strings.Join(boundedDiags(diags), "; "))
	}

	return contract.ReadResult{
		Events:     out,
		NextCursor: encodeCursor(cursorState{nextPos: nextPos, anchor: newAnchor}),
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

func boundedDiags(diags []string) []string {
	if len(diags) > contract.MaxDiagnostics {
		return diags[:contract.MaxDiagnostics]
	}
	return diags
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
