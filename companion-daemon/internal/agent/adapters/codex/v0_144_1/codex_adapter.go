// Package v0_144_1 implements the T0 contract.AgentAdapter for Codex CLI
// version 0.144.1. It reads the Codex session JSONL (~/.codex/sessions/.../rollout-*.jsonl),
// normalises events into the closed common vocabulary, and surfaces bound
// approvals. It does NOT use the app-server JSON-RPC transport; thread/session
// correlation for the app-server was not proven in R1 for ordinary interactive
// TUI sessions.
//
// Supported version: 0.144.1 (confirmed by `codex --version` in R1 evidence).
// Correlation: managed_launch (Pokit launched the process; no proven
// TUI-session↔log binding beyond launch identity).
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

// Adapter implements contract.AgentAdapter for Codex CLI 0.144.1.
type Adapter struct {
	failRead bool // when true, ReadEvents returns degraded (failure isolation)
}

// Ensure Adapter satisfies AgentAdapter at compile time.
var _ contract.AgentAdapter = (*Adapter)(nil)

// ── Descriptor ──

func (a *Adapter) Descriptor() contract.AgentAdapterDescriptor {
	return contract.AgentAdapterDescriptor{
		Name:              "codex",
		Provider:          "Codex CLI",
		ContractVersion:   contract.ContractVersion,
		SupportedVersions: []string{"0.144.1"},
		Capabilities: []contract.AdapterCapability{
			contract.CapEvents,
			contract.CapStatus,
			contract.CapApprovalDetection,
			contract.CapIncrementalRead,
			contract.CapProcessDetection,
			contract.CapLogDetection,
		},
	}
}

// ── Detect ──

func (a *Adapter) Detect(_ context.Context, session contract.SessionContext) (contract.AgentIdentity, error) {
	// Empty context — no evidence, must be unknown + low confidence.
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

	// CWD containing .codex boosts confidence.
	if strings.Contains(session.CWD, ".codex") {
		confidence += 0.15
	}

	// If logs are known to be present, boost slightly.
	// We don't have log refs directly on SessionContext, so we use CWD heuristic.

	if confidence > 1.0 {
		confidence = 1.0
	}

	// Confidence < 0.5 → must be unknown (contract requirement).
	if confidence < 0.5 {
		kind = "unknown"
	}

	return contract.AgentIdentity{
		Kind:        kind,
		DisplayName: "Codex",
		Confidence:  confidence,
	}, nil
}

// ── DiscoverSessions ──

func (a *Adapter) DiscoverSessions(_ context.Context, in contract.DiscoveryInput) ([]contract.DiscoveredSession, error) {
	// Empty / unknown context → no sessions, no invented ownership.
	if in.Session.ProcessName == "" {
		return nil, nil
	}

	procLower := strings.ToLower(in.Session.ProcessName)
	if procLower != "codex" && !strings.Contains(procLower, "codex") {
		return nil, nil
	}

	limit := contract.EffectiveDiscoveryLimit(in.Limit)
	out := []contract.DiscoveredSession{{
		ProviderSessionID: "codex-" + in.Session.SessionID,
		Provider:          "codex",
		ProviderVersion:   "0.144.1",
		Correlation:       contract.CorrelationManagedLaunch,
		Confidence:        0.85,
	}}

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ── NormalizeEvent ──

// codexRecord is the minimal parsed shape of a Codex JSONL record.
type codexRecord struct {
	Timestamp string         `json:"timestamp"`
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload"`
}

func (a *Adapter) NormalizeEvent(_ context.Context, rec contract.RawRecord) (contract.AgentEvent, contract.DegradedInfo) {
	return normalizeCodexEvent(rec, "s", agent.SourceJSONL)
}

func normalizeCodexEvent(rec contract.RawRecord, sessionID string, src contract.AgentEventSource) (contract.AgentEvent, contract.DegradedInfo) {
	prov := contract.ProvenanceNativeLog
	if contract.IsKnownProvenance(rec.Provenance) {
		prov = rec.Provenance
	}

	// AcceptRecord check: oversized records rejected upstream; here we handle parse
	// failures and semantic classification.

	var cr codexRecord
	if err := json.Unmarshal(rec.Bytes, &cr); err != nil {
		return contract.SafeEvent(contract.AgentEvent{
			ID: hashBytesID(rec.Bytes), SessionID: sessionID, AgentKind: "codex",
			Type: agent.EventUnknown, Timestamp: time.Now(), Confidence: 0.2,
			Source: sourceOrDefault(src), Provenance: string(prov),
		}), contract.Degrade("unparseable Codex record")
	}

	// Missing type field → unknown.
	if cr.Type == "" {
		return contract.SafeEvent(contract.AgentEvent{
			ID: hashBytesID(rec.Bytes), SessionID: sessionID, AgentKind: "codex",
			Type: agent.EventUnknown, Timestamp: time.Now(), Confidence: 0.2,
			Source: sourceOrDefault(src), Provenance: string(prov),
		}), contract.Degrade("missing type in Codex record")
	}

	et, conf := classifyCodexRecord(cr)
	id := hashBytesID(rec.Bytes)
	seq := parseTimestampSeq(cr.Timestamp)
	ts := parseTimestamp(cr.Timestamp)

	ev := contract.AgentEvent{
		ID: id, SessionID: sessionID, AgentKind: "codex",
		Type: et, Seq: seq, Timestamp: ts,
		Text: safeCodexText(cr), Confidence: conf,
		Source: sourceOrDefault(src), Provenance: string(prov),
		Metadata: boundedCodexMetadata(cr),
	}

	// approval_requested MUST carry a non-empty ApprovalID (contract requirement).
	if et == agent.EventApprovalRequested {
		aid := codexPayloadStr(cr, "approval_id")
		if aid != "" {
			ev.ApprovalID = aid
		} else {
			// No ApprovalID → cannot be an approval_requested event — degrade to unknown.
			ev.Type = agent.EventUnknown
			ev.Confidence = 0.3
			return ev, contract.Degrade("approval_requested without approval_id")
		}
	}

	return ev, contract.OK()
}

// classifyCodexRecord maps a parsed Codex JSONL record to the closed common
// vocabulary. Unknown discriminators become EventUnknown with low confidence.
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
		// Turn context is structural metadata; not a user-visible event.
		return agent.EventUnknown, 0.3

	default:
		return agent.EventUnknown, 0.3
	}
}

// codexPayloadStr extracts a string field from the Codex payload sub-object.
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

// safeCodexText extracts a bounded, redacted text field. It strips user prompt
// content and screens for secrets.
func safeCodexText(cr codexRecord) string {
	text := codexPayloadStr(cr, "message")
	if text == "" {
		text = codexPayloadStr(cr, "text")
	}

	// Do not surface raw prompts / terminal input.
	if cr.Type == "response_item" {
		return "" // user/assistant message content redacted
	}
	if text != "" && contract.ContainsSensitive(text) {
		return ""
	}
	if len(text) > 256 {
		text = text[:256]
	}
	return text
}

// boundedCodexMetadata extracts a small bounded metadata map. Only well-known
// safe fields are included; no raw payload content.
func boundedCodexMetadata(cr codexRecord) map[string]string {
	m := map[string]string{}
	if cr.Payload == nil {
		return m
	}

	// Safe structural fields: turn_id, session_id, model_provider (non-secret).
	if s := codexPayloadStr(cr, "turn_id"); s != "" && !contract.ContainsSensitive(s) {
		m["turn_id"] = boundStr(s, contract.MaxMetadataValueBytes)
	}
	if s := codexPayloadStr(cr, "session_id"); s != "" && !contract.ContainsSensitive(s) {
		m["session_id"] = boundStr(s, contract.MaxMetadataValueBytes)
	}
	if s := codexPayloadStr(cr, "model_provider"); s != "" && !contract.ContainsSensitive(s) {
		m["model_provider"] = boundStr(s, contract.MaxMetadataValueBytes)
	}
	if s := codexPayloadStr(cr, "cli_version"); s != "" {
		m["cli_version"] = boundStr(s, contract.MaxMetadataValueBytes)
	}
	if s := codexPayloadStr(cr, "resolution"); s != "" {
		m["resolution"] = boundStr(s, contract.MaxMetadataValueBytes)
	}

	return m
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

	// Validate cursor.
	if err := contract.ValidateCursor(in.Cursor); err != nil {
		return contract.ReadResult{Degraded: contract.Degrade("invalid cursor: " + err.Error())}, nil
	}

	// Bound the batch (count + byte caps).
	records, truncBatch := contract.BoundBatch(in.Records)
	degraded = degraded || truncBatch

	// Parse cursor watermark (last Seq).
	watermark := int64(-1)
	if !in.Cursor.IsEmpty() {
		if w, err := strconv.ParseInt(string(in.Cursor), 10, 64); err == nil {
			watermark = w
		}
	}

	limit := contract.EffectiveReadLimit(in.MaxEvents)
	src := sourceOrDefaultBatch(in.Records)
	seenID := map[string]bool{}
	var out []contract.AgentEvent
	truncated := false

	for _, rec := range records {
		// Per-record size bound.
		if !contract.AcceptRecord(rec) {
			degraded = true
			continue
		}

		ev, _ := normalizeCodexEvent(rec, sessionID, src)

		// Dedupe + watermark filter.
		if ev.ID == "" || seenID[ev.ID] || ev.Seq <= watermark {
			continue
		}

		if len(out) >= limit {
			truncated = true
			break
		}

		seenID[ev.ID] = true
		if ev.Seq > watermark {
			watermark = ev.Seq
		}
		out = append(out, ev)
	}

	degraded = degraded || truncated
	deg := contract.OK()
	if degraded {
		deg = contract.Degrade("codex read truncated or partially skipped at a bound")
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
			ID:         e.ApprovalID,
			SessionID:  e.SessionID,
			AgentKind:  e.AgentKind,
			Kind:       "approval",
			Status:     "pending",
			Prompt:     "approval requested",
			Source:     e.Source,
			Confidence: e.Confidence,
			CreatedAt:  e.Timestamp,
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

func parseTimestampSeq(ts string) int64 {
	// Parse RFC 3339 timestamp; use UnixNano as a stable ordering key.
	if ts == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return 0
	}
	return t.UnixNano()
}

func parseTimestamp(ts string) time.Time {
	if ts == "" {
		return time.Now()
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Now()
	}
	return t
}

func sourceOrDefault(s contract.AgentEventSource) contract.AgentEventSource {
	if contract.IsKnownEventSource(s) {
		return s
	}
	return agent.SourceJSONL
}

// sourceOrDefault from a batch: use the first record's source if known.
func sourceOrDefaultBatch(recs []contract.RawRecord) contract.AgentEventSource {
	for _, r := range recs {
		if contract.IsKnownEventSource(r.Source) {
			return r.Source
		}
	}
	return agent.SourceJSONL
}

// boundStr caps a string to maxBytes.
func boundStr(s string, maxBytes int) string {
	if len(s) > maxBytes {
		return s[:maxBytes]
	}
	return s
}
