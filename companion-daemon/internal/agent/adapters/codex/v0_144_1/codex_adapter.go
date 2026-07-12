// Package v0_144_1 implements the T0 contract.AgentAdapter for Codex CLI
// version 0.144.1. It reads the Codex session JSONL (~/.codex/sessions/.../rollout-*.jsonl),
// normalises events into the closed common vocabulary, and surfaces bound
// approvals. It does NOT use the app-server JSON-RPC transport; thread/session
// correlation for the app-server was not proven in R1 for ordinary interactive
// TUI sessions.
//
// Supported version: 0.144.1 (confirmed by `codex --version` in R1 evidence).
// The cli_version in session_meta payload is checked against this exact version;
// a mismatch or missing version forces the stream into unknown/degraded.
//
// Correlation: unavailable for ordinary interactive TUI. No managed_launch is
// claimed without explicit launch evidence in SessionContext.
package v0_144_1

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// supportedCodexVersion is the exact cli_version this adapter was built against.
const supportedCodexVersion = "0.144.1"

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

	if strings.Contains(session.CWD, ".codex") {
		confidence += 0.15
	}

	if confidence > 1.0 {
		confidence = 1.0
	}
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
	// R1 did not prove ordinary TUI session correlation. Without explicit launch
	// evidence or a real provider session ID we cannot claim managed_launch, and
	// we must never invent a provider session ID from the Pokit session ID.
	//
	// When observation candidates must be exposed, a real native session ID must
	// be extracted from provider records with CorrelationUnavailable. Until then,
	// returning no results is the correct safe default.
	return nil, nil
}

// ── NormalizeEvent ──

// codexRecord is the minimal parsed shape of a Codex JSONL record.
type codexRecord struct {
	Timestamp string         `json:"timestamp"`
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload"`
}

// versionState tracks whether the adapter has confirmed the Codex version in
// the current read batch.
type versionState int

const (
	versionUnknown   versionState = iota // no session_meta seen yet
	versionConfirmed                     // cli_version == supportedCodexVersion
	versionMismatch                      // cli_version missing or != supported
)

func (a *Adapter) NormalizeEvent(_ context.Context, rec contract.RawRecord) (contract.AgentEvent, contract.DegradedInfo) {
	return normalizeCodexEvent(rec, "s", rec.Source, versionUnknown)
}

// normalizeCodexEvent maps one Codex JSONL raw record to a contract AgentEvent.
// versionState controls how confident the classification is:
//   - versionUnknown: session_meta is checked; non-meta records are classified but
//     marked degraded (version not yet confirmed).
//   - versionConfirmed: full confident classification.
//   - versionMismatch: everything is forced to EventUnknown + degraded.
func normalizeCodexEvent(rec contract.RawRecord, sessionID string, src contract.AgentEventSource, vs versionState) (contract.AgentEvent, contract.DegradedInfo) {
	prov := contract.ProvenanceNativeLog
	if contract.IsKnownProvenance(rec.Provenance) {
		prov = rec.Provenance
	}

	// Reject non-JSONL sources — this adapter only handles JSONL records.
	if !contract.IsKnownEventSource(src) || (src != agent.SourceJSONL && src != agent.SourceLogFile) {
		src = agent.SourceJSONL
	}

	var cr codexRecord
	if err := json.Unmarshal(rec.Bytes, &cr); err != nil {
		return unknownEvent(rec.Bytes, sessionID, prov, src, 0.2, "unparseable Codex record")
	}

	// Missing type field → unknown.
	if cr.Type == "" {
		return unknownEvent(rec.Bytes, sessionID, prov, src, 0.2, "missing type in Codex record")
	}

	// B1: version gate — session_meta carries the cli_version that gates the
	// entire stream. A non-matching version forces EVERYTHING to EventUnknown.
	if cr.Type == "session_meta" {
		v := codexPayloadStr(cr, "cli_version")
		if v != supportedCodexVersion {
			return unknownEvent(rec.Bytes, sessionID, prov, src, 0.25,
				"unsupported Codex version: "+safeVersionDiag(v))
		}
		// Version confirmed — this event can be classified confidently.
	}

	// versionMismatch was set by a prior session_meta in this batch.
	if vs == versionMismatch {
		return unknownEvent(rec.Bytes, sessionID, prov, src, 0.25, "version mismatch in batch")
	}

	et, conf := classifyCodexRecord(cr)

	// Determine deterministic timestamp. Invalid/missing timestamps use the zero
	// time (Unix epoch), never time.Now(), so repeated reads are byte-identical.
	ts := parseTimestamp(cr.Timestamp)

	// Stable Seq: primary = timestamp (truncated to microsecond), tiebreaker =
	// content hash (0-999 range). Same-timestamp records are all preserved and
	// sort stably by content.
	seq := computeSeq(ts, rec.Bytes)
	id := hashBytesID(rec.Bytes)

	degraded := false
	var diags []string

	// versionUnknown: non-meta records before version is confirmed are classified
	// but flagged as degraded (version not yet confirmed).
	if vs == versionUnknown && cr.Type != "session_meta" {
		degraded = true
		diags = append(diags, "version not yet confirmed")
	}

	// Unknown discriminator in classifyCodexRecord → degraded.
	if et == agent.EventUnknown && cr.Type != "turn_context" {
		degraded = true
		diags = append(diags, "unknown Codex record type: "+safeDiag(cr.Type))
	}

	ev := contract.AgentEvent{
		ID: id, SessionID: sessionID, AgentKind: "codex",
		Type: et, Seq: seq, Timestamp: ts,
		Text: safeCodexText(cr), Confidence: conf,
		Source: sourceOrDefault(src), Provenance: string(prov),
		Metadata: boundedCodexMetadata(cr),
	}

	// B5: approval_id is preserved on BOTH requested and resolved events so the
	// two can be linked as one approval lifecycle.
	if et == agent.EventApprovalRequested || et == agent.EventApprovalResolved {
		aid := codexPayloadStr(cr, "approval_id")
		if aid != "" {
			ev.ApprovalID = aid
		} else {
			// Missing approval_id on a known approval event → degrade to unknown.
			ev.Type = agent.EventUnknown
			ev.Confidence = 0.25
			degraded = true
			diags = append(diags, string(et)+" without approval_id")
		}
	}

	deg := contract.OK()
	if degraded {
		deg = contract.Degrade(strings.Join(diags, "; "))
	}
	return ev, deg
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

	// Validate cursor.
	if err := contract.ValidateCursor(in.Cursor); err != nil {
		return contract.ReadResult{Degraded: contract.Degrade("invalid cursor: " + err.Error())}, nil
	}

	// Bound the batch (count + byte caps).
	records, truncBatch := contract.BoundBatch(in.Records)
	if truncBatch {
		degraded = true
		diags = append(diags, "batch truncated at bound")
	}

	// Parse cursor watermark (last Seq).
	watermark := int64(-1)
	if !in.Cursor.IsEmpty() {
		if w, err := strconv.ParseInt(string(in.Cursor), 10, 64); err == nil {
			watermark = w
		} else {
			return contract.ReadResult{Degraded: contract.Degrade("invalid cursor: unparseable watermark")}, nil
		}
	}

	// --- Version gate (B1): scan for session_meta to determine version state ---
	vs := versionUnknown
	for _, rec := range records {
		if !contract.AcceptRecord(rec) {
			continue
		}
		var cr codexRecord
		if err := json.Unmarshal(rec.Bytes, &cr); err != nil {
			continue
		}
		if cr.Type == "session_meta" {
			v := codexPayloadStr(cr, "cli_version")
			if v == supportedCodexVersion {
				vs = versionConfirmed
			} else {
				vs = versionMismatch
				degraded = true
				diags = append(diags, "unsupported Codex version: "+safeVersionDiag(v))
			}
			break
		}
	}

	limit := contract.EffectiveReadLimit(in.MaxEvents)
	seenID := map[string]bool{}
	var out []contract.AgentEvent
	truncated := false

	for _, rec := range records {
		if !contract.AcceptRecord(rec) {
			degraded = true
			diags = append(diags, "oversized record skipped")
			continue
		}

		// B4: accumulate per-record degradation.
		ev, recDeg := normalizeCodexEvent(rec, sessionID, rec.Source, vs)

		if recDeg.Degraded {
			degraded = true
			diags = append(diags, recDeg.Diagnostics...)
		}

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

	// Sort events by Seq so same-timestamp records with hash tiebreakers are
	// returned in stable order.
	sortEventsBySeq(out)

	// Update watermark to last Seq after sorting.
	if len(out) > 0 {
		watermark = out[len(out)-1].Seq
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
		deg = contract.Degrade(strings.Join(diags, "; "))
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

// hashBytesID returns a stable hex ID from raw record bytes.
func hashBytesID(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

// hashUint16 returns the first 2 bytes of SHA-256(b) as a uint16 tiebreaker.
func hashUint16(b []byte) uint16 {
	h := sha256.Sum256(b)
	return binary.BigEndian.Uint16(h[:2])
}

// computeSeq produces a stable, unique Seq from a timestamp and record content.
// Primary: timestamp truncated to microsecond. Tiebreaker: content hash (0-999).
// Same-timestamp records are all preserved; different content ⇒ different Seq.
// The result is stable across re-reads and fits in int64 for dates through ~2262.
func computeSeq(ts time.Time, raw []byte) int64 {
	base := ts.UnixNano() / 1000 * 1000 // truncate to microsecond
	tie := int64(hashUint16(raw)) % 1000
	return base + tie
}

// sortEventsBySeq sorts a slice of events by Seq in place. This ensures events
// are returned in a stable, strictly-increasing order even when same-timestamp
// records receive hash-based tiebreaker values that are out of input order.
func sortEventsBySeq(events []contract.AgentEvent) {
	for i := 1; i < len(events); i++ {
		for j := i; j > 0 && events[j].Seq < events[j-1].Seq; j-- {
			events[j], events[j-1] = events[j-1], events[j]
		}
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

// unknownEvent builds a safe EventUnknown with the given confidence and
// degradation reason. Timestamp is deterministically zero (Unix epoch) so
// repeated reads produce byte-identical results.
func unknownEvent(raw []byte, sessionID string, prov contract.Provenance, src contract.AgentEventSource, confidence float64, reason string) (contract.AgentEvent, contract.DegradedInfo) {
	ev := contract.SafeEvent(contract.AgentEvent{
		ID: hashBytesID(raw), SessionID: sessionID, AgentKind: "codex",
		Type: agent.EventUnknown, Seq: computeSeq(time.Time{}, raw),
		Timestamp: time.Time{}, Confidence: confidence,
		Source: sourceOrDefault(src), Provenance: string(prov),
	})
	return ev, contract.Degrade(reason)
}

// parseTimestamp parses an RFC 3339 timestamp. Invalid/missing timestamps return
// the zero time (Unix epoch), never time.Now(), so repeated reads are deterministic.
func parseTimestamp(ts string) time.Time {
	if ts == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Time{}
	}
	return t
}

// safeCodexText extracts a bounded, redacted text from event_msg payloads.
// User prompt and terminal input content is never surfaced.
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

// boundedCodexMetadata extracts a small bounded metadata map from well-known
// safe Codex payload fields. No raw prompt, command, or path content is included.
func boundedCodexMetadata(cr codexRecord) map[string]string {
	m := map[string]string{}
	if cr.Payload == nil {
		return m
	}
	// cli_version is recorded as provenance evidence for the version gate.
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
