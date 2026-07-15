// Package term — SP1-P2A: pump-owned resolved router + Codex certified
// approval delivery boundary.
//
// This is the ONLY component that may write a provider approval response,
// and only through the runtime's owned transport under writeMu. Success is
// NEVER queue admission: DeliveryAccepted is returned only after the exact
// store-granted bytes were written once AND the sole pump observed the
// matching `serverRequest/resolved` strictly after that write. A resolved
// observed before the write, a missing pending provider request, a payload
// digest mismatch, a duplicate arm, a timeout, provider cancellation, child
// exit, stop/kill/delete, or an epoch replacement can never produce success.
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
)

const (
	// defaultApprovalDeliveryTimeout bounds the write→resolved wait.
	defaultApprovalDeliveryTimeout = 60 * time.Second
	// maxPendingApprovalResponses bounds the per-runtime armed waiters. It
	// mirrors the pending provider-request bound: there can never be more
	// in-flight responses than open provider requests.
	maxPendingApprovalResponses = maxPendingApprovals
)

// approvalWaiterOutcome is the closed outcome vocabulary the pump routes to
// an armed waiter.
type approvalWaiterOutcome int

const (
	// waiterResolvedAfterWrite — the exact resolved was observed strictly
	// after the exact write completed: the ONLY success witness.
	waiterResolvedAfterWrite approvalWaiterOutcome = iota + 1
	// waiterResolvedPreWrite — the provider resolved the request before our
	// write completed (cancellation/independent resolution): never success.
	waiterResolvedPreWrite
	// waiterRuntimeExited — the child exited while the waiter was armed.
	waiterRuntimeExited
)

// approvalResponseWaiter is one armed pending-response entry. It is owned by
// the runtime's respMu table; the pump is the only router. `written` is the
// write/observe order witness: it is set (under respMu) only AFTER the exact
// write returned, so a resolved routed with written=false proves provider-
// side resolution. A resolved observed in the narrow window after the write
// syscall returns but before written is set routes as pre-write — failing
// CLOSED (a possible success is reported as failure, never the reverse).
type approvalResponseWaiter struct {
	idToken string
	written bool
	ch      chan approvalWaiterOutcome // buffered 1: the router never blocks
}

// routeResolvedToWaiter offers an exact resolved (native id + exact token) to
// the armed waiter, if any. Called only by the pump. Returns whether a waiter
// consumed it.
func (rt *codexManagedRuntime) routeResolvedToWaiter(idInt int64, token string) bool {
	rt.respMu.Lock()
	defer rt.respMu.Unlock()
	w := rt.respWaiters[idInt]
	if w == nil || w.idToken != token {
		return false
	}
	delete(rt.respWaiters, idInt)
	if w.written {
		w.ch <- waiterResolvedAfterWrite
	} else {
		w.ch <- waiterResolvedPreWrite
	}
	return true
}

// closeResponseWaiters deterministically fails every armed waiter and rejects
// all future arming (pump exit).
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

// payloadIDToken strictly decodes the daemon-generated response payload
// (closed shape: jsonrpc/id/result, jsonrpc=="2.0") and returns the exact
// top-level id token. Any other shape fails.
func payloadIDToken(payload []byte) (string, bool) {
	top, ok := decodeStrictObject(payload, map[string]bool{"jsonrpc": true, "id": true, "result": true})
	if !ok {
		return "", false
	}
	if v, sok := strictBoundedString(top["jsonrpc"], 8); !sok || v != "2.0" {
		return "", false
	}
	if len(top["result"]) == 0 {
		return "", false
	}
	tok, _, ok := parseNativeReqID(top["id"])
	if !ok {
		return "", false
	}
	return tok, true
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
	// resolution against the arm→write window.
	barrier func(stage string)
}

// NewCodexManagedApprovalDelivery constructs the boundary with the production
// write→resolved timeout.
func NewCodexManagedApprovalDelivery(svc *ManagedCodexService) *CodexManagedApprovalDelivery {
	return &CodexManagedApprovalDelivery{svc: svc, timeout: defaultApprovalDeliveryTimeout}
}

// Deliver implements the §5 sequence. Every failure path returns a
// non-success receipt with ZERO provider writes unless the exact write
// already happened (then only the ambiguous/exit outcomes are possible).
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
	rec, ok := d.svc.reg.Get(b.SessionID)
	if !ok || rec.Exited || rec.Epoch != b.Runtime.LaunchGen {
		return fail(DeliveryStaleRuntime)
	}
	timeout := d.timeout
	if timeout <= 0 {
		timeout = defaultApprovalDeliveryTimeout
	}
	return rt.deliverResponse(req, timeout, d.barrier)
}

// deliverResponse arms the waiter, writes the exact bytes once, and waits for
// the pump-observed resolved. See the package comment for the linearization.
func (rt *codexManagedRuntime) deliverResponse(req ApprovalDeliveryRequest, timeout time.Duration, barrier func(string)) DeliveryReceipt {
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
	tok, ok := payloadIDToken(req.Payload)
	if !ok || tok != pend.idToken {
		return fail(DeliveryRejected)
	}

	// 4. Arm the bounded waiter: one per native id (duplicate → conflict).
	w := &approvalResponseWaiter{idToken: pend.idToken, ch: make(chan approvalWaiterOutcome, 1)}
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

	// 5. Write the exact bytes ONCE — unless the provider already resolved
	// the request while we were arming: then the write is skipped entirely
	// and the outcome is the queued pre-write/exit failure.
	rt.respMu.Lock()
	stillArmed := rt.respWaiters[pend.idInt] == w
	rt.respMu.Unlock()
	if !stillArmed {
		switch <-w.ch {
		case waiterRuntimeExited:
			return fail(DeliveryStaleRuntime)
		default:
			return fail(DeliveryRejected) // provider resolved before our write
		}
	}
	if err := rt.writeRawResponse(req.Payload); err != nil {
		rt.respMu.Lock()
		if rt.respWaiters[pend.idInt] == w {
			delete(rt.respWaiters, pend.idInt)
			rt.respMu.Unlock()
			return fail(DeliveryRejected) // nothing reached the provider
		}
		rt.respMu.Unlock()
		// The router consumed the waiter concurrently; honor its outcome.
		switch <-w.ch {
		case waiterRuntimeExited:
			return fail(DeliveryStaleRuntime)
		default:
			return fail(DeliveryRejected)
		}
	}
	rt.respMu.Lock()
	if rt.respWaiters[pend.idInt] == w {
		w.written = true
	}
	rt.respMu.Unlock()

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
