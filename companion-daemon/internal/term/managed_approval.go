// Package term — SP1-P1: strict structured observation of the pinned Codex
// app-server approval request (`item/commandExecution/requestApproval`),
// NON-ACTIONABLE ONLY.
//
// The single event pump is the only caller. A request that passes EVERY
// certification check (strict field-allowlist decode with duplicate-key
// rejection, individual byte bounds, lossless bounded top-level JSON-RPC id,
// exact session/epoch/thread/turn/item binding, pinned authority version,
// exact environment and decision-tag fingerprint) arms one bounded PRIVATE
// runtime-owned pending record AND ingests one non-actionable ApprovalStore
// record with zero options as ONE outcome: if the store does not admit the
// record, nothing is armed. Anything else is rejected with ZERO state: no
// pending entry, no store record, no provider write, no log of provider
// payload. `command`, `cwd` and `proposedExecpolicyAmendment` are accepted
// ONLY as allowlisted, byte-bounded, uninterpreted raw fields — their content
// is never decoded, stored, logged, or projected into any DTO.
//
// P1 explicitly does NOT implement delivery material, provider response
// writes, resolved-consumption commit, actionable options, gate capacity, or
// RuntimeOf wiring (P2A/P2B/P3 of docs/SP1_NATIVE_APPROVAL_CONTRACT_NOTE.md).
package term

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"time"

	"encoding/json"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/devicetrust"
)

const (
	// codexApprovalMethod is the exact pinned 0.144.1 approval server request.
	codexApprovalMethod = "item/commandExecution/requestApproval"
	// codexResolvedMethod is the provider-side resolution notification.
	codexResolvedMethod = "serverRequest/resolved"

	// certifiedCodexAuthorityVersion is the ONE grammar-valid canonical
	// authority version this projection is certified against (CP0 evidence,
	// pinned @openai/codex@0.144.1). The display version string
	// ("codex-cli 0.144.1") is never compared as authority.
	certifiedCodexAuthorityVersion = "0.144.1"
	// certifiedEnvironmentID is the exact pinned environment binding.
	certifiedEnvironmentID = "local"

	maxNativeReqIDTokenLen = 20  // "-9223372036854775808" (int64 min) fits
	maxApprovalParamIDLen  = 128 // threadId / turnId / itemId byte bound
	maxDecisionTagLen      = 64
	maxDecisionCount       = 8
	// maxPendingApprovals bounds the private per-runtime pending-request
	// state. Exhaustion fails closed: the request creates NO state.
	maxPendingApprovals = 4

	// Individual byte bounds (each rejects the whole request with ZERO state).
	// The message bound is the outer rampart checked first on the raw line;
	// with the strict top-level allowlist, the largest reachable certified
	// message is envelope + params bound, well under it.
	maxApprovalMessageBytes = 24 << 10 // whole approval JSONL message
	maxApprovalParamsBytes  = 16 << 10 // raw params object
	maxDecisionElementBytes = 4 << 10  // one availableDecisions element
	maxAmendmentMarkerBytes = 4 << 10  // raw proposedExecpolicyAmendment field
)

// certifiedDecisionFingerprint is the exact ordered availableDecisions tag
// sequence proven by the accepted CP0 evidence (wire_accept_pinned.jsonl):
// tags are certification fingerprint material, NOT action authority. Any
// deviation (different tags, order, count, or element shape) rejects the
// request.
var certifiedDecisionFingerprint = [3]string{"accept", "acceptWithExecpolicyAmendment", "cancel"}

// approvalTopLevelFields is the CLOSED top-level field allowlist of the
// certified approval request envelope.
var approvalTopLevelFields = map[string]bool{
	"jsonrpc": true, "id": true, "method": true, "params": true,
}

// approvalParamsFields is the CLOSED params allowlist of the certified
// request. `command`, `cwd` and `proposedExecpolicyAmendment` are accepted as
// raw, byte-bounded, UNINTERPRETED fields only.
var approvalParamsFields = map[string]bool{
	"threadId": true, "turnId": true, "itemId": true,
	"command": true, "cwd": true, "environmentId": true,
	"availableDecisions": true, "proposedExecpolicyAmendment": true,
}

// resolvedTopLevelFields / resolvedParamsFields are the closed shape of the
// serverRequest/resolved notification (CP0 evidence: threadId + requestId).
var resolvedTopLevelFields = map[string]bool{"jsonrpc": true, "method": true, "params": true}
var resolvedParamsFields = map[string]bool{"threadId": true, "requestId": true}

// pendingProviderRequest is the bounded PRIVATE runtime-owned record of one
// exactly-certified observed provider approval request. It is owned by the
// pump under turnMu, keyed by the native int64 id, and never enters any DTO,
// log line, or public projection. The exact id token is preserved so a later
// packet (P2A) can echo the same JSON type and value; P1 itself never writes
// a provider response.
type pendingProviderRequest struct {
	idToken             string // exact top-level JSON-RPC id token (integer grammar)
	idInt               int64
	threadID            string
	turnID              string
	itemID              string
	approvalID          string
	hasAmendmentPayload bool // bounded internal marker only; payload never read
	observedAt          time.Time
}

// decodeStrictObject decodes raw as EXACTLY one JSON object admitting only
// allowlisted keys, rejecting unknown fields, duplicate keys, non-object
// input, and trailing content. Values are returned as raw messages — the
// caller decides which are interpreted; everything else stays opaque.
func decodeStrictObject(raw []byte, allowed map[string]bool) (map[string]json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, false
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, false
	}
	out := make(map[string]json.RawMessage, len(allowed))
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, ok := kt.(string)
		if !ok {
			return nil, false
		}
		if !allowed[key] {
			return nil, false // unknown field → reject
		}
		if _, dup := out[key]; dup {
			return nil, false // duplicate key → reject
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, false
		}
		out[key] = val
	}
	if _, err := dec.Token(); err != nil { // consume '}'
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF { // trailing content → reject
		return nil, false
	}
	return out, true
}

// strictBoundedString decodes raw as a JSON string that is non-empty and at
// most maxLen bytes.
func strictBoundedString(raw json.RawMessage, maxLen int) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	if s == "" || len(s) > maxLen {
		return "", false
	}
	return s, true
}

// parseNativeReqID validates and losslessly captures the top-level JSON-RPC
// id. ONLY the integer form observed in the accepted CP0 evidence is
// certified: a string, fractional, exponent, oversized, or absent id token is
// an uncertified shape and rejects. The returned token is a fresh copy (it
// must not alias the pump's scanner buffer).
func parseNativeReqID(raw json.RawMessage) (token string, id int64, ok bool) {
	if len(raw) == 0 || len(raw) > maxNativeReqIDTokenLen {
		return "", 0, false
	}
	for i, c := range raw {
		if c >= '0' && c <= '9' {
			continue
		}
		if c == '-' && i == 0 && len(raw) > 1 {
			continue
		}
		return "", 0, false // string/float/exponent/whitespace → uncertified
	}
	tok := string(raw) // copy out of the scanner buffer
	n, err := strconv.ParseInt(tok, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return tok, n, true
}

// singleObjectKey decodes raw as a JSON object with EXACTLY one key and
// returns that key. A second pair (including a duplicate of the same key),
// a non-object, or trailing content rejects. The value is skipped opaquely —
// never interpreted.
func singleObjectKey(raw json.RawMessage) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return "", false
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return "", false
	}
	if !dec.More() {
		return "", false // empty object: no tag
	}
	kt, err := dec.Token()
	if err != nil {
		return "", false
	}
	key, ok := kt.(string)
	if !ok {
		return "", false
	}
	var val json.RawMessage
	if err := dec.Decode(&val); err != nil {
		return "", false
	}
	if dec.More() {
		return "", false // second key (incl. duplicate) → reject
	}
	if _, err := dec.Token(); err != nil { // consume '}'
		return "", false
	}
	if _, err := dec.Token(); err != io.EOF {
		return "", false
	}
	return key, true
}

// decisionTags extracts the bounded certification tags from
// availableDecisions. A string element is its own tag; a single-key object
// element contributes its KEY only (the payload is never decoded beyond the
// key and never stored — only a boolean presence marker survives). Each raw
// element is individually byte-bounded. Any other element shape, an
// over-long element/tag, or an over-long list rejects.
func decisionTags(raw []json.RawMessage) (tags []string, hasAmendmentPayload bool, ok bool) {
	if len(raw) == 0 || len(raw) > maxDecisionCount {
		return nil, false, false
	}
	tags = make([]string, 0, len(raw))
	for _, el := range raw {
		if len(el) == 0 || len(el) > maxDecisionElementBytes {
			return nil, false, false
		}
		var s string
		if err := json.Unmarshal(el, &s); err == nil {
			if s == "" || len(s) > maxDecisionTagLen {
				return nil, false, false
			}
			tags = append(tags, s)
			continue
		}
		key, kok := singleObjectKey(el)
		if !kok || key == "" || len(key) > maxDecisionTagLen {
			return nil, false, false
		}
		tags = append(tags, key)
		if key == certifiedDecisionFingerprint[1] {
			hasAmendmentPayload = true
		}
	}
	return tags, hasAmendmentPayload, true
}

// fingerprintCertified requires the EXACT ordered certified tag sequence.
func fingerprintCertified(tags []string) bool {
	if len(tags) != len(certifiedDecisionFingerprint) {
		return false
	}
	for i := range certifiedDecisionFingerprint {
		if tags[i] != certifiedDecisionFingerprint[i] {
			return false
		}
	}
	return true
}

// codexApprovalID derives the canonical bounded PUBLIC approval id from the
// runtime epoch and the exact native id token. It carries no provider
// payload material and is NOT a substitute for the preserved native token.
func codexApprovalID(epoch int64, idToken string) string {
	return fmt.Sprintf("codexas-%d-%s", epoch, idToken)
}

// observeApprovalRequest is the P1 projection. Called ONLY by the pump with
// the raw JSONL line (valid until the pump's next read). Every rejection is
// total: no pending entry, no store record, no log of provider content. A
// certified request produces the pending entry AND the non-actionable store
// record as ONE outcome — if the store does not admit the record (missing
// sink, stale generation, capacity, validation), nothing is armed.
func (rt *codexManagedRuntime) observeApprovalRequest(raw []byte) {
	// Whole-message bound: the first check, on the raw line itself.
	if len(raw) > maxApprovalMessageBytes {
		rt.rejectApproval()
		return
	}
	top, ok := decodeStrictObject(raw, approvalTopLevelFields)
	if !ok {
		rt.rejectApproval()
		return
	}
	if v, sok := strictBoundedString(top["jsonrpc"], 8); !sok || v != "2.0" {
		rt.rejectApproval()
		return
	}
	if m, sok := strictBoundedString(top["method"], 128); !sok || m != codexApprovalMethod {
		rt.rejectApproval()
		return
	}
	// Top-level JSON-RPC id is the provider request identity. params.id can
	// never substitute for it: only the envelope id is consulted.
	idToken, idInt, ok := parseNativeReqID(top["id"])
	if !ok {
		rt.rejectApproval()
		return
	}
	paramsRaw := top["params"]
	if len(paramsRaw) == 0 || len(paramsRaw) > maxApprovalParamsBytes {
		rt.rejectApproval()
		return
	}
	params, ok := decodeStrictObject(paramsRaw, approvalParamsFields)
	if !ok {
		rt.rejectApproval()
		return
	}
	threadID, ok1 := strictBoundedString(params["threadId"], maxApprovalParamIDLen)
	turnID, ok2 := strictBoundedString(params["turnId"], maxApprovalParamIDLen)
	itemID, ok3 := strictBoundedString(params["itemId"], maxApprovalParamIDLen)
	if !ok1 || !ok2 || !ok3 {
		rt.rejectApproval()
		return
	}
	if env, sok := strictBoundedString(params["environmentId"], 64); !sok || env != certifiedEnvironmentID {
		rt.rejectApproval()
		return
	}
	// command and cwd are REQUIRED by the certified shape but accepted only
	// as opaque raw fields — never decoded, stored, or logged.
	if len(params["command"]) == 0 || len(params["cwd"]) == 0 {
		rt.rejectApproval()
		return
	}
	// proposedExecpolicyAmendment is optional; if present it is byte-bounded
	// and kept ONLY as a boolean presence marker.
	amendmentField := params["proposedExecpolicyAmendment"]
	if len(amendmentField) > maxAmendmentMarkerBytes {
		rt.rejectApproval()
		return
	}
	var decisionsRaw []json.RawMessage
	if dr := params["availableDecisions"]; len(dr) == 0 || json.Unmarshal(dr, &decisionsRaw) != nil {
		rt.rejectApproval()
		return
	}
	tags, hasAmendment, ok := decisionTags(decisionsRaw)
	if !ok || !fingerprintCertified(tags) {
		rt.rejectApproval()
		return
	}
	hasAmendment = hasAmendment || len(amendmentField) > 0
	// Exact pinned authority version (grammar-valid, separate from display).
	if rt.authorityVersion != certifiedCodexAuthorityVersion || !validVersion(rt.authorityVersion) {
		rt.rejectApproval()
		return
	}
	// Exact thread binding (defense in depth; the pump gates threadId too).
	if threadID != rt.threadID {
		rt.rejectApproval()
		return
	}

	approvalID := codexApprovalID(rt.epoch, idToken)

	// Linearization point: turn binding + duplicate + capacity + store
	// admission + arming are ONE critical section under turnMu (the same lock
	// that owns the exact current-turn identity). Lock order is turnMu →
	// store.mu; the store never calls back into the runtime. The pending
	// entry is armed ONLY after the store admitted the record, so private
	// pending state and the store record cannot diverge.
	rt.turnMu.Lock()
	defer rt.turnMu.Unlock()
	if rt.turnClosed || !rt.turnActive || rt.currentTurn == "" || turnID != rt.currentTurn {
		rt.approvalRejects++
		return
	}
	if _, dup := rt.pendingApprovals[idInt]; dup {
		// Duplicate native request id: never a second authority record.
		rt.approvalRejects++
		return
	}
	if len(rt.pendingApprovals) >= maxPendingApprovals {
		// Bounded-state exhaustion fails closed.
		rt.approvalRejects++
		return
	}
	if rt.approvals == nil {
		// No admitted store record is possible → nothing may be armed.
		rt.approvalRejects++
		return
	}
	// Safe NON-ACTIONABLE store record: zero options, no prompt text, no
	// provider payload. Provenance is the versioned provider protocol; the
	// mechanical origin vocabulary is closed, so the structured JSONL stdio
	// protocol records as SourceJSONL. Actionability stays false — the frozen
	// store never upgrades it later.
	admitted := rt.approvals.IngestObserved(ApprovalIngest{
		SessionID: rt.sessionID,
		LaunchGen: rt.epoch,
		StreamGen: 0,
		Provider:  codexAppServerAdapter,
		Version:   rt.authorityVersion,
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID:         approvalID,
				SessionID:  rt.sessionID,
				AgentKind:  codexAppServerAdapter,
				Kind:       "approval",
				Source:     agent.SourceJSONL,
				Confidence: 1,
			},
			Provenance:   contract.ProvenanceProviderProtocol,
			Actionable:   false,
			RequiredPerm: devicetrust.PermTerminalInput,
		}},
	})
	if !admitted {
		// Store refusal (stale generation / capacity / validation): zero state.
		rt.approvalRejects++
		return
	}
	rt.pendingApprovals[idInt] = pendingProviderRequest{
		idToken:             idToken,
		idInt:               idInt,
		threadID:            threadID,
		turnID:              turnID,
		itemID:              itemID,
		approvalID:          approvalID,
		hasAmendmentPayload: hasAmendment,
		observedAt:          time.Now(),
	}
}

// observeApprovalResolved handles a provider-side resolution: the matching
// pending entry is removed and the SAME record is marked invalidated so an
// already-resolved intervention is never displayed as a current request. P1
// has no consumption authority: this never commits a success, never writes
// anything, and never touches any OTHER record. A resolved for an unknown or
// foreign request, a foreign thread, a token mismatch, or an uncertified
// message shape is inert.
func (rt *codexManagedRuntime) observeApprovalResolved(raw []byte) {
	top, ok := decodeStrictObject(raw, resolvedTopLevelFields)
	if !ok {
		return
	}
	if m, sok := strictBoundedString(top["method"], 128); !sok || m != codexResolvedMethod {
		return
	}
	params, ok := decodeStrictObject(top["params"], resolvedParamsFields)
	if !ok {
		return
	}
	threadID, sok := strictBoundedString(params["threadId"], maxApprovalParamIDLen)
	if !sok || threadID != rt.threadID {
		return
	}
	token, idInt, ok := parseNativeReqID(params["requestId"])
	if !ok {
		return
	}
	rt.turnMu.Lock()
	approvalID := ""
	if pend, exists := rt.pendingApprovals[idInt]; exists && pend.idToken == token {
		delete(rt.pendingApprovals, idInt)
		approvalID = pend.approvalID
	}
	sink := rt.approvals
	rt.turnMu.Unlock()
	if approvalID != "" && sink != nil {
		sink.InvalidateRecord(rt.sessionID, approvalID)
	}
}

// clearPendingForTurnLocked drops pending entries bound to the completed turn
// and returns their approval IDs so the caller can invalidate the matching
// display records. Caller holds turnMu.
func (rt *codexManagedRuntime) clearPendingForTurnLocked(turnID string) []string {
	var ids []string
	for id, pend := range rt.pendingApprovals {
		if pend.turnID == turnID {
			ids = append(ids, pend.approvalID)
			delete(rt.pendingApprovals, id)
		}
	}
	return ids
}

// rejectApproval counts one totally-rejected observation (no state, no log of
// provider content). The counter is internal diagnostics for tests only.
func (rt *codexManagedRuntime) rejectApproval() {
	rt.turnMu.Lock()
	rt.approvalRejects++
	rt.turnMu.Unlock()
}

// approvalObservationSnapshot is a NARROW read-only accessor for
// deterministic tests: a copy of the pending records and the reject count.
func (rt *codexManagedRuntime) approvalObservationSnapshot() (pending []pendingProviderRequest, rejects int) {
	rt.turnMu.Lock()
	defer rt.turnMu.Unlock()
	for _, p := range rt.pendingApprovals {
		pending = append(pending, p)
	}
	return pending, rt.approvalRejects
}
