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
// Without an exact 0.144.1 confirmation no version-specific typed event is
// emitted.
//
// Seq: the first 8 bytes of SHA-256(raw record) as a non-negative int63.  This
// gives a ~2⁶³ collision-free stable ordering key.  Events are sorted by Seq
// before emission; the cursor is a compact high-water-mark (last Seq).
// Same-timestamp records are all preserved; out-of-order timestamps are
// harmless; missing timestamps produce a valid Seq.  Re-reads with the same
// cursor produce zero events.
//
// Correlation: unavailable (R1).  DiscoverSessions returns nil.
package v0_144_1

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

const supportedCodexVersion = "0.144.1"

// Adapter implements contract.AgentAdapter for Codex CLI 0.144.1.
type Adapter struct {
	failRead bool
}

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

// ── NormalizeEvent ──

func (a *Adapter) NormalizeEvent(_ context.Context, rec contract.RawRecord) (contract.AgentEvent, contract.DegradedInfo) {
	return normalizeCodexEvent(rec, "s", rec.Source, false)
}

// normalizeCodexEvent maps one Codex JSONL raw record to a contract AgentEvent.
// When versionFailed is true the output is forced to EventUnknown regardless of
// the record's own discriminators — used when the batch version gate failed.
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

	// Version gate: session_meta carries cli_version.
	if cr.Type == "session_meta" {
		v := codexPayloadStr(cr, "cli_version")
		if v != supportedCodexVersion {
			return unknownRec(rec.Bytes, sessionID, prov, src, "unsupported Codex version: "+safeVersionDiag(v))
		}
	}

	// Batch-wide version failure forces everything to unknown.
	if versionFailed {
		return unknownRec(rec.Bytes, sessionID, prov, src, "version mismatch in batch")
	}

	et, conf := classifyCodexRecord(cr)
	ts := parseTimestamp(cr.Timestamp)
	seq := computeSeq(rec.Bytes)

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

	// B5: approval_id on both requested and resolved.
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

	// Parse cursor watermark (last Seq).  Filter: ev.Seq > watermark.
	watermark := int64(-1)
	if !in.Cursor.IsEmpty() {
		w, err := strconv.ParseInt(string(in.Cursor), 10, 64)
		if err != nil {
			return contract.ReadResult{Degraded: contract.Degrade("invalid cursor: unparseable watermark")}, nil
		}
		watermark = w
	}

	// ── Version gate (B2): scan EVERY session_meta ──
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
	// Without an exact 0.144.1 confirmation, no version-specific typed
	// events may be emitted.  A conflicting or missing version poisons the
	// entire batch.
	if !versionOK || versionFailed {
		versionFailed = true
		versionOK = false
	}

	limit := contract.EffectiveReadLimit(in.MaxEvents)
	seenID := map[string]bool{}

	// Phase 1: normalise ALL records, collect events + degradation.
	var raw []contract.AgentEvent
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
		raw = append(raw, ev)
	}

	// Phase 2: sort by Seq (stable hash-based ordering).
	sort.Slice(raw, func(i, j int) bool { return raw[i].Seq < raw[j].Seq })

	// Phase 3: dedupe + watermark filter + cap.
	var out []contract.AgentEvent
	truncated := false
	for _, ev := range raw {
		if ev.Seq <= watermark || seenID[ev.ID] {
			continue
		}
		if len(out) >= limit {
			truncated = true
			break
		}
		seenID[ev.ID] = true
		out = append(out, ev)
	}

	// Watermark advances to the last emitted Seq.
	for _, ev := range out {
		if ev.Seq > watermark {
			watermark = ev.Seq
		}
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

	return contract.ReadResult{
		Events:     out,
		NextCursor: contract.Cursor(strconv.FormatInt(watermark, 10)),
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

// computeSeq returns a stable, non-negative, collision-free Seq from raw record
// bytes.  Uses the first 8 bytes of SHA-256 as a uint64 with the sign bit
// cleared (int63), giving ~9×10¹⁸ values — collisions are practically
// impossible.  Same bytes ⇒ same Seq.  No timestamp dependency.
func computeSeq(raw []byte) int64 {
	h := sha256.Sum256(raw)
	u := binary.BigEndian.Uint64(h[:8])
	return int64(u & 0x7FFFFFFFFFFFFFFF)
}

func hashBytesID(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

func unknownRec(raw []byte, sessionID string, prov contract.Provenance, src contract.AgentEventSource, reason string) (contract.AgentEvent, contract.DegradedInfo) {
	ev := contract.SafeEvent(contract.AgentEvent{
		ID: hashBytesID(raw), SessionID: sessionID, AgentKind: "codex",
		Type: agent.EventUnknown, Seq: computeSeq(raw),
		Timestamp: timeZero, Confidence: 0.2,
		Source: src, Provenance: string(prov),
	})
	return ev, contract.Degrade(reason)
}

// timeZero is the deterministic fallback for invalid/missing timestamps.
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
