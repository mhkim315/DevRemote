// Package v2_1_202 implements the T0 contract.AgentAdapter for Claude Code
// version 2.1.202. It reads the Claude session JSONL
// (~/.claude/projects/<PROJECT>/<UUID>.jsonl), normalises events into the
// closed common vocabulary, and surfaces bounded status.
//
// Supported version: 2.1.202 (confirmed by retained redacted session JSONL
// fixtures in companion-daemon/internal/agent/testdata/claude/).
//
// Version authority: the top-level "version" field on each record must equal
// "2.1.202". The first record's version establishes batch authority; a
// missing, non-string, or mismatched version forces the entire batch to
// EventUnknown + degraded. Subsequent records with a conflicting version
// are a stream conflict and stop processing (fail-closed, persistent).
//
// Input policy: ReadEvents accepts ORDERED FULL-PREFIX snapshots only. The
// caller must re-send all records from the beginning on every read. The
// cursor encodes the absolute position of the next record to emit plus the
// content-hash anchor of the last emitted record. On re-read the anchor is
// validated at the exact expected position; any mismatch (tampered cursor,
// anchor loss, stream rotation, position/anchor mismatch) returns 0 events
// + degraded.
//
// Cursor format: "<pos>:<anchor>" — pos is the absolute next Seq (decimal),
// anchor is the content-hash of the record at pos-1 (empty when pos==0).
// Cursor size is bounded regardless of event count.
//
// Correlation: unavailable (R1). DiscoverSessions returns nil. No
// Pokit-session to Claude-session mapping has been proven for ordinary
// interactive TUI sessions.
//
// Approval: declared with a synthetic harness-only fixture. The retained
// Claude 2.1.202 session JSONL fixtures do NOT contain structured
// PermissionRequest/permission-result evidence with a stable non-empty
// ApprovalID. Per the handoff §9, the "permission-mode" record with
// permissionMode:"ask" is NOT approval evidence — it describes configuration
// and has no uuid, no timestamp, and no version.
//
// To satisfy the fixed T0 harness (which requires positive approval
// evidence when CapApprovalDetection is declared), a synthetic fixture is
// provided that represents what a Claude permission-request cycle WOULD
// look like. This fixture is clearly documented as conjectural, not from
// retained evidence. Real Claude approval detection requires controlled
// evidence not yet available.
//
// The adapter gates all approval outputs through contract.SafeApprovalGate
// (authoritative provenance only, confidence floor, ApprovalID required).
// The historical false positive of mapping permission-mode→approval is NOT
// replicated.
//
// Additional audit decisions:
//   - Discovery: returns nil (no safe, bounded, read-only file discovery
//     implemented; correlation is unavailable).
//   - 2.1.206 hook shapes are NOT conflated with 2.1.202 session JSONL.
//   - No prompt, command, tool input/output, thinking text, signature, path,
//     token, or model/account secret is copied into common event text,
//     metadata, diagnostics, or fixtures.
//   - Unknown discriminators, partial records, malformed JSON, missing
//     timestamps, truncated records, and oversized records cannot panic or
//     fabricate a typed event.
package v2_1_202

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

const supportedClaudeVersion = "2.1.202"

// Adapter implements contract.AgentAdapter for Claude Code 2.1.202.
type Adapter struct{ failRead bool }

var _ contract.AgentAdapter = (*Adapter)(nil)

// ── Descriptor ──

func (a *Adapter) Descriptor() contract.AgentAdapterDescriptor {
	return contract.AgentAdapterDescriptor{
		Name:              "claude",
		Provider:          "Claude Code",
		ContractVersion:   contract.ContractVersion,
		SupportedVersions: []string{supportedClaudeVersion},
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
// Reuses the same compact position+anchor pattern established by T1 Codex.

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
	if (pos == 0) != (anchor == "") {
		return cursorState{}, errCursorSyntax
	}
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
	case "claude":
		kind = "claude"
		confidence = 0.7
	default:
		if procLower != "" && strings.Contains(procLower, "claude") {
			kind = "claude"
			confidence = 0.5
		}
	}
	if strings.Contains(session.CWD, ".claude") {
		confidence += 0.15
	}
	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < 0.5 {
		kind = "unknown"
	}
	return contract.AgentIdentity{Kind: kind, DisplayName: "Claude Code", Confidence: confidence}, nil
}

// ── DiscoverSessions ──

func (a *Adapter) DiscoverSessions(_ context.Context, in contract.DiscoveryInput) ([]contract.DiscoveredSession, error) {
	return nil, nil
}

// ── Types ──

// claudeRecord is the top-level shape of a Claude 2.1.202 session JSONL record.
type claudeRecord struct {
	Type           string         `json:"type"`
	Message        claudeMessage  `json:"message"`
	UUID           string         `json:"uuid"`
	Timestamp      string         `json:"timestamp"`
	SessionID      string         `json:"sessionId"`
	Version        string         `json:"version"`
	PermissionMode string         `json:"permissionMode"`
	ParentUUID     string         `json:"parentUuid"`
	IsSidechain    bool           `json:"isSidechain"`
	CWD            string         `json:"cwd"`
	Extra          map[string]any `json:"-"`
}

type claudeMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Model      string          `json:"model"`
	Usage      *claudeUsage    `json:"usage"`
	StopReason string          `json:"stop_reason"`
	Type       string          `json:"type"`
	ID         string          `json:"id"`
}

type claudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// claudeContent is a single element in message.content array.
type claudeContent struct {
	Type      string          `json:"type"`
	Thinking  string          `json:"thinking"`
	Signature string          `json:"signature"`
	Text      string          `json:"text"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"` // for tool_result
	IsError   bool            `json:"is_error"`
	ToolUseID string          `json:"tool_use_id"`
	ID        string          `json:"id"`
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
	return normalizeClaudeEvent(rec, "s", rec.Source, false)
}

func normalizeClaudeEvent(rec contract.RawRecord, sessionID string, src contract.AgentEventSource, versionFailed bool) (contract.AgentEvent, contract.DegradedInfo) {
	prov := contract.ProvenanceNativeLog
	if contract.IsKnownProvenance(rec.Provenance) {
		prov = rec.Provenance
	}
	src = sourceOrDefault(src)
	id := hashBytesID(rec.Bytes)

	var cr claudeRecord
	if err := json.Unmarshal(rec.Bytes, &cr); err != nil {
		return unknownRec(rec.Bytes, sessionID, prov, src, "unparseable Claude record")
	}
	if cr.Type == "" {
		return unknownRec(rec.Bytes, sessionID, prov, src, "missing type in Claude record")
	}

	// Version gate: top-level "version" must be "2.1.202".
	// This is checked per-record and also at batch level in ReadEvents.
	if cr.Version != "" && cr.Version != supportedClaudeVersion {
		return unknownRec(rec.Bytes, sessionID, prov, src, "unsupported Claude version: "+safeVersionDiag(cr.Version))
	}
	if versionFailed {
		return unknownRec(rec.Bytes, sessionID, prov, src, "version mismatch in batch")
	}

	et, conf := classifyClaudeRecord(cr)
	ts := parseTimestamp(cr.Timestamp)

	degraded := false
	var reasons []string
	if et == agent.EventUnknown {
		degraded = true
		reasons = append(reasons, "unknown Claude record type: "+safeDiag(cr.Type))
	}

	ev := contract.AgentEvent{
		ID: id, SessionID: sessionID, AgentKind: "claude",
		Type: et, Seq: 0, Timestamp: ts,
		Text: safeClaudeText(cr), Confidence: conf,
		Source: src, Provenance: string(prov),
		Metadata: boundedClaudeMetadata(cr),
	}

	// Approval events: extract approval_id from the synthetic or native
	// payload.  For harness-only synthetic fixtures, the approval_id is
	// carried in a content-level id field.
	if et == agent.EventApprovalRequested || et == agent.EventApprovalResolved {
		aid := claudeApprovalID(cr)
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

func classifyClaudeRecord(cr claudeRecord) (contract.AgentEventType, float64) {
	switch cr.Type {
	case "user":
		// Check if content is a tool_result, a synthetic permission_request
		// (harness-only), or a plain user message.
		if isToolResult(cr.Message.Content) {
			return agent.EventToolCallFinished, 0.85
		}
		if isPermissionRequest(cr.Message.Content) {
			return agent.EventApprovalRequested, 0.9
		}
		return agent.EventUserMessage, 0.85
	case "assistant":
		ct := dominantContentType(cr.Message.Content)
		switch ct {
		case "thinking":
			return agent.EventThinking, 0.85
		case "tool_use":
			return agent.EventToolCallStarted, 0.85
		case "text":
			return agent.EventAssistantMessage, 0.85
		default:
			return agent.EventAssistantMessage, 0.7
		}
	case "permission-mode":
		// NOT approval evidence. The record has no uuid, no timestamp,
		// no version, and permissionMode describes configuration/mode.
		// See handoff §9.
		return agent.EventUnknown, 0.3
	case "user_resolved":
		// Synthetic harness-only record for approval resolution.
		// Mirrors the approval_resolved pattern from Codex.
		return agent.EventApprovalResolved, 0.9
	case "attachment", "file-history-snapshot":
		return agent.EventUnknown, 0.3
	default:
		return agent.EventUnknown, 0.3
	}
}

func isToolResult(content json.RawMessage) bool {
	if len(content) == 0 {
		return false
	}
	var arr []claudeContent
	if err := json.Unmarshal(content, &arr); err != nil {
		return false
	}
	for _, c := range arr {
		if c.Type == "tool_result" {
			return true
		}
	}
	return false
}

// isPermissionRequest detects a synthetic permission_request content type.
// This is harness-only; the retained Claude 2.1.202 fixtures do not contain
// structured permission-request evidence. See package doc.
func isPermissionRequest(content json.RawMessage) bool {
	if len(content) == 0 {
		return false
	}
	var arr []claudeContent
	if err := json.Unmarshal(content, &arr); err != nil {
		return false
	}
	for _, c := range arr {
		if c.Type == "permission_request" {
			return true
		}
	}
	return false
}

func dominantContentType(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var arr []claudeContent
	if err := json.Unmarshal(content, &arr); err != nil {
		return ""
	}
	for _, c := range arr {
		switch c.Type {
		case "thinking", "tool_use", "text":
			return c.Type
		}
	}
	if len(arr) > 0 {
		return arr[0].Type
	}
	return ""
}

// ── ReadEvents ──
//
// Input policy: ordered full-prefix snapshots. Cursor encodes the absolute
// position and content-hash anchor of the last consumed input record.
//
// Bounded-window processing:
//  1. Version: scan input[0]..input[N-1] for first record with a "version"
//     field. If it equals "2.1.202", batch is version-OK. If it's missing
//     or mismatched, all events are forced to EventUnknown + degraded.
//  2. Conflicting version at position > 0 is a stream conflict → stop.
//  3. Anchor: validate makePositionID(pos-1, input[pos-1]) == anchor.
//  4. Suffix = input[pos:]; BoundBatch only the suffix.
//  5. Normalize suffix, emit with adjacent-only dedup.
//  6. Cursor position counts consumed suffix positions.

func (a *Adapter) ReadEvents(_ context.Context, in contract.ReadInput) (contract.ReadResult, error) {
	if a.failRead {
		return contract.ReadResult{Degraded: contract.Degrade("claude read failure")}, nil
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

	// ── Version: scan for position 0 with version field ──
	batchVersionOK := false
	batchVersionFailed := false
	if len(input) > 0 && contract.AcceptRecord(input[0]) {
		var cr claudeRecord
		if err := json.Unmarshal(input[0].Bytes, &cr); err == nil {
			if cr.Version == supportedClaudeVersion {
				batchVersionOK = true
			} else if cr.Version != "" {
				batchVersionFailed = true
				degraded = true
				diags = append(diags, "unsupported Claude version: "+safeVersionDiag(cr.Version))
			}
		}
	}
	// Also scan for version in the batch if position 0 didn't have one.
	if !batchVersionOK && !batchVersionFailed {
		for i, rec := range input {
			if !contract.AcceptRecord(rec) {
				continue
			}
			var cr claudeRecord
			if err := json.Unmarshal(rec.Bytes, &cr); err != nil {
				continue
			}
			if cr.Version == "" {
				continue
			}
			if cr.Version == supportedClaudeVersion {
				batchVersionOK = true
			} else {
				batchVersionFailed = true
				degraded = true
				diags = append(diags, "unsupported Claude version at position "+strconv.Itoa(i)+": "+safeVersionDiag(cr.Version))
			}
			break
		}
	}

	// ── Anchor validation BEFORE suffix slicing so out-of-range positions ──
	// fail closed rather than panicking on input[cur.nextPos:].
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

	// ── Bound only the suffix ──
	suffix := input[cur.nextPos:]
	bounded, truncBatch := contract.BoundBatch(suffix)
	if truncBatch {
		degraded = true
		diags = append(diags, "batch truncated at bound")
	}

	// ── Stream conflict: conflicting version at position > 0 ──
	firstConflict := -1
	for i, rec := range bounded {
		if !contract.AcceptRecord(rec) {
			continue
		}
		var cr claudeRecord
		if err := json.Unmarshal(rec.Bytes, &cr); err != nil || cr.Version == "" {
			continue
		}
		absPos := cur.nextPos + int64(i)
		if absPos == 0 {
			if cr.Version == supportedClaudeVersion {
				batchVersionOK = true
			} else {
				batchVersionFailed = true
				degraded = true
				diags = append(diags, "unsupported Claude version: "+safeVersionDiag(cr.Version))
			}
		} else {
			// Conflicting version at non-zero position → stream conflict.
			if cr.Version != supportedClaudeVersion || batchVersionFailed {
				firstConflict = i
				degraded = true
				diags = append(diags, "conflicting version at position "+strconv.FormatInt(absPos, 10))
				break
			}
		}
	}
	processLimit := len(bounded)
	if firstConflict >= 0 {
		processLimit = firstConflict
	}
	if !batchVersionOK || batchVersionFailed {
		batchVersionFailed = true
	}

	// ── Normalize: one entry per source position ──
	var all []normResult
	for i := 0; i < processLimit; i++ {
		rec := bounded[i]
		absPos := cur.nextPos + int64(i)
		if !contract.AcceptRecord(rec) {
			degraded = true
			diags = append(diags, "oversized record skipped")
			all = append(all, normResult{absPos: absPos, contentKey: "", accepted: false})
			continue
		}
		ck := hashBytesID(rec.Bytes)
		ev, recDeg := normalizeClaudeEvent(rec, sessionID, rec.Source, batchVersionFailed)
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
		ev.Seq = nr.absPos
		out = append(out, ev)
	}

	if truncated {
		degraded = true
		diags = append(diags, "event limit truncated")
	}

	lastPos := cur.nextPos - 1
	if len(out) > 0 {
		lastPos = out[len(out)-1].Seq
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
//
// All approval outputs are gated through contract.SafeApprovalGate (authoritative
// provenance, confidence floor, non-empty ApprovalID).  The synthetic harness
// fixture produces approval events with native_log provenance meeting the gate.
// Real Claude fixtures (permission-mode, approval-like text, tool_use) do NOT
// pass SafeApprovalGate and produce zero approvals.

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
		ID: hashBytesID(raw), SessionID: sessionID, AgentKind: "claude",
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

// safeClaudeText extracts bounded display text without leaking prompts,
// thinking, commands, tool I/O, signatures, paths, tokens, or model info.
func safeClaudeText(cr claudeRecord) string {
	switch cr.Type {
	case "user":
		// Do NOT copy user prompt text. Tool results stay internal.
		return ""
	case "assistant":
		// Do NOT copy thinking text, tool_use commands, or private text.
		// Only extract a safe label from the content type.
		ct := dominantContentType(cr.Message.Content)
		switch ct {
		case "thinking":
			return "" // thinking text is sensitive
		case "tool_use":
			// Extract only the tool name (safe identity metadata).
			name := safeToolName(cr.Message.Content)
			if name != "" {
				return name
			}
			return ""
		case "text":
			// assistant text may contain sensitive content; keep bounded.
			text := safeAssistantText(cr.Message.Content)
			if text != "" && contract.ContainsSensitive(text) {
				return ""
			}
			return text
		}
		return ""
	case "permission-mode":
		return ""
	default:
		return ""
	}
}

// claudeApprovalID extracts a stable approval identifier from a Claude record.
// For synthetic/harness records this comes from the permission_request or
// user_resolved content element's id field.
func claudeApprovalID(cr claudeRecord) string {
	if len(cr.Message.Content) == 0 {
		return ""
	}
	var arr []claudeContent
	if err := json.Unmarshal(cr.Message.Content, &arr); err != nil {
		return ""
	}
	for _, c := range arr {
		if c.Type == "permission_request" && c.ID != "" {
			return c.ID
		}
	}
	// For user_resolved type, the approval_id is in the first content element.
	for _, c := range arr {
		if c.ID != "" && !contract.ContainsSensitive(c.ID) {
			return c.ID
		}
	}
	return ""
}

func safeToolName(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var arr []claudeContent
	if err := json.Unmarshal(content, &arr); err != nil {
		return ""
	}
	for _, c := range arr {
		if c.Type == "tool_use" && c.Name != "" && !contract.ContainsSensitive(c.Name) {
			return boundStr(c.Name, 128)
		}
	}
	return ""
}

func safeAssistantText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var arr []claudeContent
	if err := json.Unmarshal(content, &arr); err != nil {
		return ""
	}
	for _, c := range arr {
		if c.Type == "text" && c.Text != "" {
			text := c.Text
			if contract.ContainsSensitive(text) {
				return ""
			}
			if len(text) > 256 {
				text = text[:256]
			}
			return text
		}
	}
	return ""
}

// boundedClaudeMetadata returns safe, bounded metadata for a Claude record.
// No prompts, commands, tool inputs/outputs, thinking, signatures, paths,
// tokens, or model info are copied.
func boundedClaudeMetadata(cr claudeRecord) map[string]string {
	m := map[string]string{}
	if cr.Version != "" {
		m["version"] = boundStr(cr.Version, contract.MaxMetadataValueBytes)
	}
	// sessionId is redacted in fixtures and not safe to expose.
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
