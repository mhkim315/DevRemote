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
	"encoding/binary"
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
	ev         contract.AgentEvent
	deg        contract.DegradedInfo
	absPos     int64
	contentKey string
	accepted   bool
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
//
// Input policy: ordered full-prefix snapshots.  Cursor encodes the absolute
// position and content-hash anchor of the last consumed input record.
//
// Bounded-window processing (no full-prefix scan, no unbounded prefix map):
//  1. Version: parse ONLY input[0] (session_meta always at position 0).
//  2. Anchor: validate makePositionID(pos-1, input[pos-1]) == anchor (one record).
//  3. Suffix = input[pos:]; BoundBatch only the suffix.
//  4. Normalize suffix, emit with within-batch dedup.
//  5. Cursor position counts consumed suffix positions.
//
// Cross-page content duplicates at different source positions are distinct
// events (position-based identity).  Within-batch content dedup is preserved.

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

	cur, err := parseCursor(in.Cursor)
	if err != nil {
		return contract.ReadResult{Degraded: contract.Degrade("invalid cursor: " + err.Error())}, nil
	}

	input := in.Records

	// ── Version: parse position 0 (session_meta) + scan bounded suffix ──
	batchVersionOK := false
	batchVersionFailed := false
	if len(input) > 0 && contract.AcceptRecord(input[0]) {
		var cr codexRecord
		if err := json.Unmarshal(input[0].Bytes, &cr); err == nil && cr.Type == "session_meta" {
			v := codexPayloadStr(cr, "cli_version")
			if v == supportedCodexVersion {
				batchVersionOK = true
			} else {
				batchVersionFailed = true
				degraded = true
				diags = append(diags, "unsupported Codex version: "+safeVersionDiag(v))
			}
		}
	}

	// ── Bound only the suffix ──
	suffix := input[cur.nextPos:]
	bounded, truncBatch := contract.BoundBatch(suffix)
	if truncBatch {
		degraded = true
		diags = append(diags, "batch truncated at bound")
	}

	// BLOCKER 2: Any session_meta at position > 0 is a stream conflict.
	// Position 0 is the sole version authority.  Find the first conflict
	// position and only process records up to (but not including) it.
	// The cursor stops at the conflict so the next call re-encounters it.
	firstConflict := -1
	for i, rec := range bounded {
		if !contract.AcceptRecord(rec) {
			continue
		}
		var cr codexRecord
		if err := json.Unmarshal(rec.Bytes, &cr); err != nil || cr.Type != "session_meta" {
			continue
		}
		absPos := cur.nextPos + int64(i)
		if absPos == 0 {
			v := codexPayloadStr(cr, "cli_version")
			if v == supportedCodexVersion {
				batchVersionOK = true
			} else if v != "" {
				batchVersionFailed = true
				degraded = true
				diags = append(diags, "unsupported Codex version: "+safeVersionDiag(v))
			}
		} else {
			// Session_meta at non-zero position → stream conflict.
			firstConflict = i
			degraded = true
			diags = append(diags, "unexpected session_meta at position "+strconv.FormatInt(absPos, 10))
			break
		}
	}
	// Only process records up to (but not including) the first conflict.
	processLimit := len(bounded)
	if firstConflict >= 0 {
		processLimit = firstConflict
	}
	if !batchVersionOK || batchVersionFailed {
		batchVersionFailed = true
	}

	// ── Anchor validation: one raw hash at input[pos-1] — bounded, 1 record ──
	if cur.nextPos > 0 {
		idx := int(cur.nextPos - 1)
		if idx >= len(input) || !contract.AcceptRecord(input[idx]) {
			diags = append(diags, "cursor position out of range or oversized anchor record")
			return contract.ReadResult{Degraded: contract.Degrade(strings.Join(boundedDiags(diags), "; "))}, nil
		}
		if makePositionID(cur.nextPos-1, input[idx].Bytes) != cur.anchor {
			diags = append(diags, "cursor anchor mismatch at position "+strconv.FormatInt(cur.nextPos-1, 10))
			return contract.ReadResult{Degraded: contract.Degrade(strings.Join(boundedDiags(diags), "; "))}, nil
		}
	}

	// ── Normalize: one entry per source position (even skipped) ──
	var all []normResult
	for i := 0; i < processLimit; i++ {
		rec := bounded[i]
		absPos := cur.nextPos + int64(i)
		ck := hashBytesID(rec.Bytes)
		if !contract.AcceptRecord(rec) {
			degraded = true
			diags = append(diags, "oversized record skipped")
			all = append(all, normResult{absPos: absPos, contentKey: ck, accepted: false})
			continue
		}
		ev, recDeg := normalizeCodexEvent(rec, sessionID, rec.Source, batchVersionFailed)
		ev.ID = makePositionID(absPos, rec.Bytes)
		all = append(all, normResult{ev: ev, deg: recDeg, absPos: absPos, contentKey: ck, accepted: true})
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

	// ── Emit: adjacent-only duplicate suppression ──
	limit := contract.EffectiveReadLimit(in.MaxEvents)
	var out []contract.AgentEvent
	truncated := false
	processed := int64(0)

	// Previous content hash for adjacent dedup (cross-page boundary).
	var prevCK string
	if cur.nextPos > 0 {
		idx := int(cur.nextPos - 1)
		if idx < len(input) {
			prevCK = hashBytesID(input[idx].Bytes)
		}
	}

	for i := range all {
		nr := &all[i]
		if !nr.accepted {
			processed++
			prevCK = nr.contentKey
			continue
		}
		ev := nr.ev
		if ev.ID == "" {
			processed++
			prevCK = nr.contentKey
			continue
		}
		// Adjacent duplicate suppression.
		if nr.contentKey == prevCK {
			processed++
			continue
		}
		if len(out) >= limit {
			truncated = true
			break
		}
		processed++
		prevCK = nr.contentKey
		ev.Seq = nr.absPos // absolute source position
		out = append(out, ev)
	}

	if truncated {
		degraded = true
		diags = append(diags, "event limit truncated")
	}

	// nextPos = position after last emitted event (absolute source pos).
	lastPos := cur.nextPos - 1
	if len(out) > 0 {
		lastPos = out[len(out)-1].Seq // Seq == absPos
	}
	nextPos := lastPos + 1
	var newAnchor string
	if lastPos >= 0 && int(lastPos) < len(input) {
		newAnchor = makePositionID(lastPos, input[lastPos].Bytes)
	}
	if len(out) == 0 {
		nextPos = cur.nextPos
		newAnchor = cur.anchor
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

// makePositionID returns a position-based event ID:
// SHA-256(pos || content)[:8].  Same content at different source positions
// → different IDs.  This guarantees one-shot and paged reads produce
// identical event sets regardless of MaxEvents.
func makePositionID(pos int64, content []byte) string {
	h := sha256.New()
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(pos))
	h.Write(buf[:])
	h.Write(content)
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:8])
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
