// Package term — SP1-P2A: pump-owned resolved router + Codex certified
// approval delivery boundary.
//
// This is the ONLY component that may write a provider approval response,
// and only through the runtime's owned transport under writeMu. Success is
// NEVER queue admission: DeliveryAccepted is returned only after the exact
// store-granted bytes were written once AND the sole pump observed the
// matching `serverRequest/resolved` strictly after that write.
//
// P2A-R1 linearization: every waiter transition (armed → write_claimed →
// written, resolved routing, exit close) happens under ONE respMu
// linearization point, while no lock is ever held across the external write.
// A resolved that wins the claim race produces ZERO writes; a resolved
// observed after the write claim but before the written mark is the
// ambiguous during-write outcome — never success and never retryable, so a
// manual retry can never cause a duplicate write.
//
// P2A-R1 semantic coupling: the store binds the selected OptionID and the
// material's schema identity into the immutable binding; this boundary
// strict-decodes the response to EXACTLY {"jsonrpc","id","result":{"decision"}}
// and admits only the certified mapping allow_once→accept / deny→decline
// under the certified schema — a swapped, amended, unknown, or extra-field
// response is rejected BEFORE the wire.
//
// P2A scope: the boundary exists and is proven by deterministic tests, but
// it is NOT wired into production handlers — Handlers.ApprovalDelivery
// remains the capacity-0 gate and production ingestion stays non-actionable.
// P2B installs this boundary and actionable ingestion as one atomic
// production-owned transition.
package term

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

const (
	// defaultApprovalDeliveryTimeout bounds the write→resolved wait.
	defaultApprovalDeliveryTimeout = 60 * time.Second
	// maxPendingApprovalResponses bounds the per-runtime armed waiters. It
	// mirrors the pending provider-request bound: there can never be more
	// in-flight responses than open provider requests.
	maxPendingApprovalResponses = maxPendingApprovals

	// codexDecisionSchemaV1 is the certified Codex decision-response schema
	// identity. Delivery material ingested under any other schema identity is
	// rejected by this boundary before the wire.
	codexDecisionSchemaV1 = "codex.appserver.decision.v1"
)

// certifiedActionDecision is the frozen certified action→decision mapping
// (schema + live CP0 accept/decline consumption evidence). Everything else —
// including acceptWithExecpolicyAmendment and cancel — is non-deliverable.
var certifiedActionDecision = map[string]string{
	"allow_once": "accept",
	"deny":       "decline",
}

// waiterState is the closed waiter lifecycle. Transitions happen ONLY under
// respMu — the same linearization point the pump's resolved routing and the
// exit close use.
type waiterState int

const (
	// waiterStateArmed — registered; the write has not been claimed yet. A
	// resolved routed now proves provider-side resolution and the write is
	// never performed.
	waiterStateArmed waiterState = iota
	// waiterStateWriteClaimed — the deliverer owns the (single) write and is
	// performing it outside the lock. A resolved routed now is the ambiguous
	// during-write outcome: never success, never retryable.
	waiterStateWriteClaimed
	// waiterStateWritten — the exact write completed. A resolved routed now
	// is the ONLY success witness.
	waiterStateWritten
)

// approvalWaiterOutcome is the closed outcome vocabulary the pump routes to
// an armed waiter.
type approvalWaiterOutcome int

const (
	// waiterResolvedAfterWrite — the exact resolved was observed strictly
	// after the exact write completed: the ONLY success witness.
	waiterResolvedAfterWrite approvalWaiterOutcome = iota + 1
	// waiterResolvedPreWrite — the provider resolved the request before the
	// write was claimed: the write is skipped entirely (zero writes).
	waiterResolvedPreWrite
	// waiterResolvedDuringWrite — the provider's resolution was observed
	// after the write claim but before the written mark: ambiguous (the
	// resolution may or may not be ours), never success, never retryable.
	waiterResolvedDuringWrite
	// waiterRuntimeExited — the child exited while the waiter was armed.
	waiterRuntimeExited
)

// approvalResponseWaiter is one armed pending-response entry. It is owned by
// the runtime's respMu table; the pump is the only router.
type approvalResponseWaiter struct {
	idToken string
	state   waiterState
	ch      chan approvalWaiterOutcome // buffered 1: the router never blocks
}

// routeResolvedToWaiter offers an exact resolved (native id + exact token) to
// the armed waiter, if any. Called only by the pump. The waiter's state at
// THIS linearization point decides the outcome. Returns whether a waiter
// consumed it.
func (rt *codexManagedRuntime) routeResolvedToWaiter(idInt int64, token string) bool {
	rt.respMu.Lock()
	defer rt.respMu.Unlock()
	w := rt.respWaiters[idInt]
	if w == nil || w.idToken != token {
		return false
	}
	delete(rt.respWaiters, idInt)
	switch w.state {
	case waiterStateWritten:
		w.ch <- waiterResolvedAfterWrite
	case waiterStateWriteClaimed:
		w.ch <- waiterResolvedDuringWrite
	default:
		w.ch <- waiterResolvedPreWrite
	}
	return true
}

// closeResponseWaiters deterministically fails every armed waiter and rejects
// all future arming (pump exit). It uses the same respMu linearization point,
// so stop/kill/replacement race the armed→claimed→written transitions exactly
// like a resolved does.
func (rt *codexManagedRuntime) closeResponseWaiters() {
	rt.respMu.Lock()
	rt.respClosed = true
	for id, w := range rt.respWaiters {
		delete(rt.respWaiters, id)
		w.ch <- waiterRuntimeExited
	}
	rt.respMu.Unlock()
}

// writeRawResponse writes the EXACT daemon-generated response bytes as one
// JSONL line through the owned transport. The input is copied before the
// newline is appended so the caller's backing array is never mutated.
func (rt *codexManagedRuntime) writeRawResponse(b []byte) error {
	line := make([]byte, 0, len(b)+1)
	line = append(line, b...)
	line = append(line, '\n')
	rt.writeMu.Lock()
	defer rt.writeMu.Unlock()
	_, err := rt.proc.Stdin().Write(line)
	return err
}

// payloadDecisionEnvelope strictly decodes the daemon-generated response
// payload: EXACTLY {id:<integer>, result:{"decision":<string>}} with an
// optional jsonrpc member that, when present, must be "2.0" (the LIVE-proven
// consumed response shape carries no jsonrpc), no unknown or extra fields at
// either level and no duplicate keys. It returns the exact top-level id token
// and the decision string.
func payloadDecisionEnvelope(payload []byte) (idToken, decision string, ok bool) {
	top, tok0 := decodeStrictObject(payload, map[string]bool{"jsonrpc": true, "id": true, "result": true})
	if !tok0 {
		return "", "", false
	}
	if !optionalExactJSONRPC(top["jsonrpc"]) {
		return "", "", false
	}
	tok, _, iok := parseNativeReqID(top["id"])
	if !iok {
		return "", "", false
	}
	res, rok := decodeStrictObject(top["result"], map[string]bool{"decision": true})
	if !rok {
		return "", "", false
	}
	dec, dok := strictBoundedString(res["decision"], maxDecisionTagLen)
	if !dok {
		return "", "", false
	}
	return tok, dec, true
}

// newDeliveryReceiptID returns an opaque receipt identifier, or ("", false)
// on entropy failure. It never falls back to a fixed value.
func newDeliveryReceiptID() (string, bool) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", false
	}
	return "cdxr-" + hex.EncodeToString(b[:]), true
}

// CodexManagedApprovalDelivery is the SP1-P2A certified delivery boundary
// bound to one ManagedCodexService. NOT wired into production in P2A
// (Handlers.ApprovalDelivery stays the capacity-0 gate); P2B installs it
// atomically together with actionable ingestion.
type CodexManagedApprovalDelivery struct {
	svc     *ManagedCodexService
	timeout time.Duration
	// barrier is a NARROW test seam (nil in production) invoked at named
	// points inside Deliver so deterministic tests can interleave a provider
	// resolution against the arm→claim→write windows.
	barrier func(stage string)
}

// NewCodexManagedApprovalDelivery constructs the boundary with the production
// write→resolved timeout.
func NewCodexManagedApprovalDelivery(svc *ManagedCodexService) *CodexManagedApprovalDelivery {
	return &CodexManagedApprovalDelivery{svc: svc, timeout: defaultApprovalDeliveryTimeout}
}

// Deliver implements the §5 sequence. Every failure path returns a
// non-success receipt with ZERO provider writes unless the write was already
// claimed (then only the ambiguous/exit outcomes are possible).
func (d *CodexManagedApprovalDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	fail := func(o DeliveryOutcome) DeliveryReceipt {
		return DeliveryReceipt{Outcome: o, ClaimToken: req.ClaimToken, Binding: req.Binding}
	}
	// 1. Canonical metadata grammar + EXACT payload digest before anything
	// else: substituted bytes are rejected before the wire, not at commit.
	if !validGateBindingMeta(req) {
		return fail(DeliveryRejected)
	}
	if len(req.Payload) == 0 || len(req.Payload) > maxDeliveryMaterialBytes {
		return fail(DeliveryRejected)
	}
	if payloadDigest(req.Payload) != req.Binding.PayloadDigest {
		return fail(DeliveryRejected)
	}
	b := req.Binding
	if b.Runtime.Adapter != codexAppServerAdapter || b.Runtime.StreamGen != 0 {
		return fail(DeliveryRuntimeMismatch)
	}
	// 1b. P2A-R1 exact action↔response semantic coupling, BEFORE the wire:
	// the response must strict-decode to exactly one certified decision, the
	// binding's material schema must be the certified schema, and the
	// store-selected option must map to exactly that decision. A swapped,
	// amended, unknown, or extra-field response never reaches the provider.
	idToken, decision, ok := payloadDecisionEnvelope(req.Payload)
	if !ok {
		return fail(DeliveryRejected)
	}
	if b.DeliverySchema != codexDecisionSchemaV1 {
		return fail(DeliveryRejected)
	}
	want, certified := certifiedActionDecision[b.OptionID]
	if !certified || decision != want {
		return fail(DeliveryRejected)
	}
	// 2. Current-runtime revalidation: live runtime, exact epoch, pinned
	// certified authority version, registry record present and not exited.
	d.svc.mu.Lock()
	rt := d.svc.runtimes[b.SessionID]
	d.svc.mu.Unlock()
	if rt == nil {
		return fail(DeliveryUnavailable)
	}
	if rt.epoch != b.Runtime.LaunchGen || rt.authorityVersion != b.Runtime.Version ||
		rt.authorityVersion != certifiedCodexAuthorityVersion {
		return fail(DeliveryStaleRuntime)
	}
	rec, ok2 := d.svc.reg.Get(b.SessionID)
	if !ok2 || rec.Exited || rec.Epoch != b.Runtime.LaunchGen {
		return fail(DeliveryStaleRuntime)
	}
	timeout := d.timeout
	if timeout <= 0 {
		timeout = defaultApprovalDeliveryTimeout
	}
	return rt.deliverResponse(req, idToken, timeout, d.barrier)
}

// deliverResponse arms the waiter, claims and performs the single write, and
// waits for the pump-observed resolved. See the package comment for the
// linearization.
func (rt *codexManagedRuntime) deliverResponse(req ApprovalDeliveryRequest, payloadIDToken string, timeout time.Duration, barrier func(string)) DeliveryReceipt {
	fail := func(o DeliveryOutcome) DeliveryReceipt {
		return DeliveryReceipt{Outcome: o, ClaimToken: req.ClaimToken, Binding: req.Binding}
	}
	b := req.Binding

	// 3. The P1 pending provider request for this approval must still be
	// open (a resolved/expired/completed request can never be written to),
	// and the payload's top-level id token must equal the preserved native
	// token (the public ApprovalID is never parsed back).
	rt.turnMu.Lock()
	if rt.turnClosed {
		rt.turnMu.Unlock()
		return fail(DeliveryStaleRuntime)
	}
	var pend pendingProviderRequest
	found := false
	for _, p := range rt.pendingApprovals {
		if p.approvalID == b.ApprovalID {
			pend, found = p, true
			break
		}
	}
	rt.turnMu.Unlock()
	if !found {
		return fail(DeliveryUnavailable)
	}
	if payloadIDToken != pend.idToken {
		return fail(DeliveryRejected)
	}

	// 4. Arm the bounded waiter: one per native id (duplicate → conflict).
	w := &approvalResponseWaiter{idToken: pend.idToken, state: waiterStateArmed, ch: make(chan approvalWaiterOutcome, 1)}
	rt.respMu.Lock()
	if rt.respClosed {
		rt.respMu.Unlock()
		return fail(DeliveryStaleRuntime)
	}
	if _, dup := rt.respWaiters[pend.idInt]; dup {
		rt.respMu.Unlock()
		return fail(DeliveryConflict)
	}
	if len(rt.respWaiters) >= maxPendingApprovalResponses {
		rt.respMu.Unlock()
		return fail(DeliveryUnavailable)
	}
	rt.respWaiters[pend.idInt] = w
	rt.respMu.Unlock()

	if barrier != nil {
		barrier("post-arm")
	}

	// 5. Claim the single write under the SAME linearization point the
	// router and the exit close use. If a resolved (or exit) already
	// consumed the waiter, the write is NEVER performed — zero writes.
	rt.respMu.Lock()
	if rt.respWaiters[pend.idInt] != w {
		rt.respMu.Unlock()
		switch <-w.ch {
		case waiterRuntimeExited:
			return fail(DeliveryStaleRuntime)
		default:
			return fail(DeliveryRejected) // provider resolved: zero writes
		}
	}
	if err := rt.authorizer.AuthorizeCommit(req.DeviceID, req.DeviceEpoch, devicetrust.IntentApprovalDeliver); err != nil {
		delete(rt.respWaiters, pend.idInt)
		rt.respMu.Unlock()
		return fail(DeliveryStaleRuntime)
	}
	w.state = waiterStateWriteClaimed
	rt.respMu.Unlock()

	if barrier != nil {
		barrier("post-write-claim")
	}

	// The claimed write happens OUTSIDE any lock. A resolved observed while
	// the claim is held routes as during-write (ambiguous, non-retryable).
	writeErr := rt.writeRawResponse(req.Payload)

	rt.respMu.Lock()
	if rt.respWaiters[pend.idInt] == w {
		if writeErr != nil {
			// Nothing reached the provider and nobody consumed the waiter:
			// retract it. Rejected proves non-acceptance (retry-safe: zero
			// bytes were written).
			delete(rt.respWaiters, pend.idInt)
			rt.respMu.Unlock()
			return fail(DeliveryRejected)
		}
		w.state = waiterStateWritten
		rt.respMu.Unlock()
	} else {
		// The router (or exit) consumed the waiter during the claimed write.
		rt.respMu.Unlock()
		switch <-w.ch {
		case waiterRuntimeExited:
			return fail(DeliveryStaleRuntime)
		default:
			// during-write resolution: ambiguous, non-retryable — a manual
			// retry can never cause a duplicate write.
			return fail(DeliveryConflict)
		}
	}

	if barrier != nil {
		barrier("post-written-mark")
	}

	// 6. Wait for the pump-observed resolved, bounded.
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	consume := func(out approvalWaiterOutcome) DeliveryReceipt {
		switch out {
		case waiterResolvedAfterWrite:
			rid, rok := newDeliveryReceiptID()
			if !rok {
				// Entropy failure after consumption: ambiguous, non-retryable.
				return fail(DeliveryConflict)
			}
			return DeliveryReceipt{
				Outcome: DeliveryAccepted, ClaimToken: req.ClaimToken, Binding: req.Binding,
				ReceiptID: rid, DeliveredPayloadDigest: payloadDigest(req.Payload),
			}
		case waiterResolvedDuringWrite:
			return fail(DeliveryConflict)
		case waiterResolvedPreWrite:
			return fail(DeliveryRejected)
		default:
			return fail(DeliveryStaleRuntime)
		}
	}
	select {
	case out := <-w.ch:
		return consume(out)
	case <-timer.C:
		rt.respMu.Lock()
		if rt.respWaiters[pend.idInt] == w {
			delete(rt.respWaiters, pend.idInt)
			rt.respMu.Unlock()
			// Written but never observed resolved: ambiguous (a late resolved
			// may still exist) → non-retryable conflict; the late resolved
			// then finds no waiter and cannot commit.
			return fail(DeliveryConflict)
		}
		rt.respMu.Unlock()
		// The router consumed the waiter concurrently with the timeout.
		return consume(<-w.ch)
	}
}
