// Package v0_144_1 implements the T0 contract.AgentAdapter for Codex CLI
// version 0.144.1. It reads the Codex session JSONL
// (~/.codex/sessions/.../rollout-*.jsonl), normalises events into the closed
// common vocabulary, and surfaces bound approvals.
//
// Supported version: 0.144.1 (confirmed by `codex --version` in R1 evidence).
// Every session_meta in a read batch is checked; a single missing / mismatched /
// malformed cli_version forces the ENTIRE batch to EventUnknown + degraded.
//
// Seq / cursor: compact position+anchor cursor (fixed ~50 bytes regardless of
// event count).  Format: "v:<ver>:<pos>:<anchor>" where ver=1 if 0.144.1
// confirmed, pos=next absolute Seq, anchor=content-hash of the last emitted
// record (empty initially).
//
// Two input modes:
//   - Full-prefix (harness): caller re-sends all records from the beginning.
//     The anchor is found in the input → every record through the anchor is
//     skipped → only new records emitted.  Zero re-emission.
//   - Incremental (production): caller sends only new records.  The anchor is
//     NOT found → all records are emitted with advancing Seq.
//
// Anchor mismatch (found in the wrong position) → fail-closed + degraded.
//
// Correlation: unavailable (R1).  DiscoverSessions returns nil.
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

// cursorPrefix / cursorSep delimit cursor fields.
const cursorPrefix = "v:"
const cursorSep = ":"

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

// ── Cursor encoding ──
//
// Format:  v:<version_ok>:<next_pos>:<anchor_id>
//
//	version_ok = "1" if exact 0.144.1 confirmed, "0" otherwise
//	next_pos   = absolute next Seq (decimal)
//	anchor_id  = content-hash of the last emitted record (empty initially)
//
// Cursor size is fixed at ~50 bytes regardless of how many records have been
// processed — it never grows beyond MaxCursorBytes.
//
// On re-read (full-prefix): the anchor is found in the input → all records up
// to and including the anchor are skipped → zero re-emission.
// On append (incremental): the anchor is NOT found → all records are new →
// emitted with advancing Seq.

type cursorState struct {
	versionOK bool
	nextPos   int64
	anchor    string // hex content-hash, "" initially
}

func parseCursor(c contract.Cursor) (cursorState, error) {
	if c.IsEmpty() {
		return cursorState{}, nil
	}
	s := string(c)
	if !strings.HasPrefix(s, cursorPrefix) {
		return cursorState{}, strconv.ErrSyntax
	}
	s = s[len(cursorPrefix):]
	parts := strings.SplitN(s, cursorSep, 3)
	if len(parts) != 3 {
		return cursorState{}, strconv.ErrSyntax
	}
	ver, ok := strconv.Atoi(parts[0])
	if ok != nil || (ver != 0 && ver != 1) {
		return cursorState{}, strconv.ErrSyntax
	}
	pos, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || pos < 0 {
		return cursorState{}, strconv.ErrSyntax
	}
	return cursorState{versionOK: ver == 1, nextPos: pos, anchor: parts[2]}, nil
}

func encodeCursor(st cursorState) contract.Cursor {
	ver := "0"
	if st.versionOK {
		ver = "1"
	}
	return contract.Cursor(cursorPrefix + ver + cursorSep + strconv.FormatInt(st.nextPos, 10) + cursorSep + st.anchor)
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

	// Parse compact position+anchor cursor.
	cur, err := parseCursor(in.Cursor)
	if err != nil {
		return contract.ReadResult{Degraded: contract.Degrade("invalid cursor: " + err.Error())}, nil
	}

	// ── Version gate: scan EVERY session_meta ──
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

	// Resolve effective version state: cursor-carried authority survives
	// incremental batches that lack a session_meta. A conflicting meta in
	// this batch overrides.
	versionOK := cur.versionOK
	if batchVersionFailed {
		versionOK = false
	} else if batchVersionOK {
		versionOK = true
	}
	// If no meta in batch AND cursor didn't confirm → version failed.
	versionFailed := !versionOK

	if !versionOK {
		degraded = true
		diags = append(diags, "version not confirmed for 0.144.1")
	}

	// ── Phase 1: normalise all accepted records ──
	type norm struct {
		ev  contract.AgentEvent
		deg contract.DegradedInfo
	}
	var all []norm
	for _, rec := range records {
		if !contract.AcceptRecord(rec) {
			degraded = true
			diags = append(diags, "oversized record skipped")
			continue
		}
		ev, recDeg := normalizeCodexEvent(rec, sessionID, rec.Source, versionFailed)
		all = append(all, norm{ev, recDeg})
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

	// ── Phase 2: anchor-based skip (full-prefix) or emit-all (incremental) ──
	startIdx := 0
	// Inconsistent cursor: position advanced but anchor lost.
	if cur.anchor == "" && cur.nextPos > 0 {
		degraded = true
		diags = append(diags, "cursor anchor lost")
	}
	if cur.anchor != "" {
		found := -1
		for i := range all {
			if all[i].ev.ID == cur.anchor {
				found = i
				break
			}
		}
		if found >= 0 {
			// Full-prefix: skip records through the anchor.
			startIdx = found + 1
		} else {
			// Anchor expected but not found: incremental input, tampered cursor,
			// or stream rotation.  Emit all records but flag degradation so the
			// caller knows the cursor could not be used for deduplication.
			degraded = true
			diags = append(diags, "cursor anchor not found in input")
		}
	}

	// ── Phase 3: assign Seq, dedupe within batch, cap ──
	limit := contract.EffectiveReadLimit(in.MaxEvents)
	batchSeen := map[string]bool{}
	var out []contract.AgentEvent
	var newAnchor string
	truncated := false

	for i := startIdx; i < len(all); i++ {
		ev := all[i].ev
		if ev.ID == "" || batchSeen[ev.ID] {
			continue
		}
		if len(out) >= limit {
			truncated = true
			break
		}
		batchSeen[ev.ID] = true
		ev.Seq = cur.nextPos + int64(len(out))
		out = append(out, ev)
		newAnchor = ev.ID
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

	// Build next cursor: version state + absolute position + last anchor.
	nextCur := cursorState{
		versionOK: versionOK,
		nextPos:   cur.nextPos + int64(len(out)),
		anchor:    newAnchor,
	}
	// If no events emitted, preserve old anchor so full-prefix skip still works.
	if len(out) == 0 {
		nextCur.anchor = cur.anchor
	}

	return contract.ReadResult{
		Events:     out,
		NextCursor: encodeCursor(nextCur),
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
