// Package term — SP1-P1: strict structured observation of the pinned Codex
// app-server approval request (`item/commandExecution/requestApproval`),
// NON-ACTIONABLE ONLY.
//
// The single event pump is the only caller. A request that passes EVERY
// certification check (lossless bounded top-level JSON-RPC id, exact
// session/epoch/thread/turn/item binding, pinned authority version, exact
// environment and decision-tag fingerprint) arms one bounded PRIVATE
// runtime-owned pending record and ingests one non-actionable ApprovalStore
// record with zero options. Anything else is rejected with ZERO state: no
// pending entry, no store record, no provider write, no log of provider
// payload. Raw command, CWD, prompt and amendment bodies are never decoded,
// stored, logged, or projected into any DTO.
//
// P1 explicitly does NOT implement delivery material, provider response
// writes, resolved-consumption commit, actionable options, gate capacity, or
// RuntimeOf wiring (P2A/P2B/P3 of docs/SP1_NATIVE_APPROVAL_CONTRACT_NOTE.md).
package term

import (
	"fmt"
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
)

// certifiedDecisionFingerprint is the exact ordered availableDecisions tag
// sequence proven by the accepted CP0 evidence (wire_accept_pinned.jsonl):
// tags are certification fingerprint material, NOT action authority. Any
// deviation (different tags, order, count, or element shape) rejects the
// request.
var certifiedDecisionFingerprint = [3]string{"accept", "acceptWithExecpolicyAmendment", "cancel"}

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

// codexApprovalEnvelope decodes ONLY the JSON-RPC envelope fields the
// projection needs. ID is captured as the exact raw token (json.RawMessage)
// so the native identity is handled without floating-point conversion.
type codexApprovalEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// codexApprovalParams is the strict structural projection of the certified
// params subset. `command`, `cwd` and every other field are DELIBERATELY not
// decoded — they must never be read, stored, or logged. A known field with
// the wrong JSON type fails the decode and rejects the request.
type codexApprovalParams struct {
	ThreadID           string            `json:"threadId"`
	TurnID             string            `json:"turnId"`
	ItemID             string            `json:"itemId"`
	EnvironmentID      string            `json:"environmentId"`
	AvailableDecisions []json.RawMessage `json:"availableDecisions"`
}

// codexResolvedParams is the strict projection of serverRequest/resolved.
// RequestID is kept as the exact raw token for lossless comparison with the
// stored pending id token.
type codexResolvedParams struct {
	ThreadID  string          `json:"threadId"`
	RequestID json.RawMessage `json:"requestId"`
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

// decisionTags extracts the bounded certification tags from
// availableDecisions. A string element is its own tag; a single-key object
// element contributes its KEY only (the payload is never decoded beyond the
// key and never stored — only a boolean presence marker survives). Any other
// element shape, an over-long tag, or an over-long list rejects.
func decisionTags(raw []json.RawMessage) (tags []string, hasAmendmentPayload bool, ok bool) {
	if len(raw) == 0 || len(raw) > maxDecisionCount {
		return nil, false, false
	}
	tags = make([]string, 0, len(raw))
	for _, el := range raw {
		var s string
		if err := json.Unmarshal(el, &s); err == nil {
			if s == "" || len(s) > maxDecisionTagLen {
				return nil, false, false
			}
			tags = append(tags, s)
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(el, &obj); err != nil || len(obj) != 1 {
			return nil, false, false
		}
		for k := range obj {
			if k == "" || len(k) > maxDecisionTagLen {
				return nil, false, false
			}
			tags = append(tags, k)
			if k == certifiedDecisionFingerprint[1] {
				hasAmendmentPayload = true
			}
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
// total: no pending entry, no store record, no log of provider content.
func (rt *codexManagedRuntime) observeApprovalRequest(raw []byte) {
	var env codexApprovalEnvelope
	if err := json.Unmarshal(raw, &env); err != nil || env.Method != codexApprovalMethod {
		rt.rejectApproval()
		return
	}
	// Top-level JSON-RPC id is the provider request identity. params.id can
	// never substitute for it: only env.ID is consulted.
	idToken, idInt, ok := parseNativeReqID(env.ID)
	if !ok {
		rt.rejectApproval()
		return
	}
	if len(env.Params) == 0 {
		rt.rejectApproval()
		return
	}
	var p codexApprovalParams
	if err := json.Unmarshal(env.Params, &p); err != nil {
		rt.rejectApproval()
		return
	}
	if p.ThreadID == "" || len(p.ThreadID) > maxApprovalParamIDLen ||
		p.TurnID == "" || len(p.TurnID) > maxApprovalParamIDLen ||
		p.ItemID == "" || len(p.ItemID) > maxApprovalParamIDLen {
		rt.rejectApproval()
		return
	}
	if p.EnvironmentID != certifiedEnvironmentID {
		rt.rejectApproval()
		return
	}
	tags, hasAmendment, ok := decisionTags(p.AvailableDecisions)
	if !ok || !fingerprintCertified(tags) {
		rt.rejectApproval()
		return
	}
	// Exact pinned authority version (grammar-valid, separate from display).
	if rt.authorityVersion != certifiedCodexAuthorityVersion || !validVersion(rt.authorityVersion) {
		rt.rejectApproval()
		return
	}
	// Exact thread binding (defense in depth; the pump gates threadId too).
	if p.ThreadID != rt.threadID {
		rt.rejectApproval()
		return
	}

	approvalID := codexApprovalID(rt.epoch, idToken)

	// Linearization point: turn binding + duplicate + capacity + arming are
	// ONE critical section under turnMu (the same lock that owns the exact
	// current-turn identity), so a closing runtime or a completed turn can
	// never race an arm.
	rt.turnMu.Lock()
	if rt.turnClosed || !rt.turnActive || rt.currentTurn == "" || p.TurnID != rt.currentTurn {
		rt.approvalRejects++
		rt.turnMu.Unlock()
		return
	}
	if _, dup := rt.pendingApprovals[idInt]; dup {
		// Duplicate native request id: never a second authority record.
		rt.approvalRejects++
		rt.turnMu.Unlock()
		return
	}
	if len(rt.pendingApprovals) >= maxPendingApprovals {
		// Bounded-state exhaustion fails closed.
		rt.approvalRejects++
		rt.turnMu.Unlock()
		return
	}
	rt.pendingApprovals[idInt] = pendingProviderRequest{
		idToken:             idToken,
		idInt:               idInt,
		threadID:            p.ThreadID,
		turnID:              p.TurnID,
		itemID:              p.ItemID,
		approvalID:          approvalID,
		hasAmendmentPayload: hasAmendment,
		observedAt:          time.Now(),
	}
	sink := rt.approvals
	rt.turnMu.Unlock()

	if sink == nil {
		return
	}
	// Safe NON-ACTIONABLE store record: zero options, no prompt text, no
	// provider payload. Provenance is the versioned provider protocol; the
	// mechanical origin vocabulary is closed, so the structured JSONL stdio
	// protocol records as SourceJSONL. Actionability stays false — the frozen
	// store never upgrades it later.
	sink.Ingest(ApprovalIngest{
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
}

// observeApprovalResolved removes the matching pending entry when the
// provider reports its own resolution. P1 has no consumption authority: this
// never commits, resolves, or writes anything — the store record ages out
// under the frozen TTL / session invalidation. A resolved for an unknown or
// foreign request, a foreign thread, or a token mismatch is inert.
func (rt *codexManagedRuntime) observeApprovalResolved(raw []byte) {
	var env struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.Method != codexResolvedMethod || len(env.Params) == 0 {
		return
	}
	var p codexResolvedParams
	if err := json.Unmarshal(env.Params, &p); err != nil {
		return
	}
	if p.ThreadID != rt.threadID {
		return
	}
	token, idInt, ok := parseNativeReqID(p.RequestID)
	if !ok {
		return
	}
	rt.turnMu.Lock()
	if pend, exists := rt.pendingApprovals[idInt]; exists && pend.idToken == token {
		delete(rt.pendingApprovals, idInt)
	}
	rt.turnMu.Unlock()
}

// clearPendingForTurnLocked drops pending entries bound to the completed
// turn. Caller holds turnMu.
func (rt *codexManagedRuntime) clearPendingForTurnLocked(turnID string) {
	for id, pend := range rt.pendingApprovals {
		if pend.turnID == turnID {
			delete(rt.pendingApprovals, id)
		}
	}
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
