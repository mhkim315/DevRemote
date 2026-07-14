package term

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// A1 remediation 3 (R3-B/R3-C) — approval-specific delivery boundary + generation-
// owned delivery gate.
//
// A delivery request and its receipt carry the ONE immutable ApprovalExecutionBinding
// the claim token owns; the receipt also carries an opaque ReceiptID and the digest
// of the EXACT bytes delivered. RecordDelivery compares every binding field, the claim
// token, and the delivered-payload digest before a success may commit, so substituted
// bytes cannot commit.
//
// Delivery acceptance is linearized by the per-session RuntimeDeliveryGate: under one
// gate lock the current generation-owned endpoint is verified AND the exact bytes are
// enqueued into that endpoint (the acceptance point). Activation, launch/stream
// replacement, correlation loss, delete, unlink, and termination take the SAME lock,
// so a replacement cannot interleave between the check and the enqueue, and an old
// generation accepts no bytes. No external I/O is performed while any lock is held.
//
// No accepted provider action-delivery channel exists, so production registers NO
// sink; Accept fails and the production gated boundary returns `unavailable`, writing
// no bytes. The gate/receipt semantics are proven with a controlled fixture sink.

type DeliveryOutcome string

const (
	DeliveryAccepted        DeliveryOutcome = "accepted"
	DeliveryAlreadyAccepted DeliveryOutcome = "already_accepted"
	DeliveryStaleRuntime    DeliveryOutcome = "stale_runtime"
	DeliveryRuntimeMismatch DeliveryOutcome = "runtime_mismatch"
	DeliveryUnavailable     DeliveryOutcome = "unavailable"
	DeliveryConflict        DeliveryOutcome = "conflict"
	DeliveryRejected        DeliveryOutcome = "rejected"
)

var deliveryOutcomeValid = map[DeliveryOutcome]bool{
	DeliveryAccepted: true, DeliveryAlreadyAccepted: true, DeliveryStaleRuntime: true,
	DeliveryRuntimeMismatch: true, DeliveryUnavailable: true, DeliveryConflict: true,
	DeliveryRejected: true,
}

func IsValidDeliveryOutcome(o DeliveryOutcome) bool { return deliveryOutcomeValid[o] }

func deliverySucceeded(o DeliveryOutcome) bool {
	return o == DeliveryAccepted || o == DeliveryAlreadyAccepted
}

// deliveryProvesNonAcceptance reports whether an outcome proves the daemon boundary
// accepted NOTHING (so a bounded manual retry is safe). An accepted/already_accepted
// outcome is not a failure; every other closed outcome is an unambiguous non-acceptance.
func deliveryProvesNonAcceptance(o DeliveryOutcome) bool {
	switch o {
	case DeliveryUnavailable, DeliveryStaleRuntime, DeliveryRuntimeMismatch, DeliveryRejected:
		return true
	default:
		return false
	}
}

// ApprovalDeliveryRequest carries the claim token, immutable binding, and the exact
// server-computed payload bytes.
type ApprovalDeliveryRequest struct {
	ClaimToken string
	Binding    ApprovalExecutionBinding
	Payload    []byte
}

// DeliveryReceipt is the immutable, fully-bound result of a delivery attempt.
type DeliveryReceipt struct {
	Outcome                DeliveryOutcome
	ClaimToken             string
	Binding                ApprovalExecutionBinding
	ReceiptID              string
	DeliveredPayloadDigest string // digest of the EXACT bytes delivered
}

type ApprovalDelivery interface {
	Deliver(req ApprovalDeliveryRequest) DeliveryReceipt
}

func newReceiptID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// unavailableApprovalDelivery is a no-channel boundary that accepts nothing.
type unavailableApprovalDelivery struct{}

func NewUnavailableApprovalDelivery() ApprovalDelivery { return unavailableApprovalDelivery{} }

func (unavailableApprovalDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	return DeliveryReceipt{Outcome: DeliveryUnavailable, ClaimToken: req.ClaimToken, Binding: req.Binding}
}

// DeliverySink is a generation-owned delivery endpoint. Enqueue is called UNDER the
// gate lock — it must be fast (a bounded in-memory append) and perform NO external
// I/O; the external drain targets this captured endpoint separately.
type DeliverySink interface {
	Enqueue(payload []byte) (receiptID string)
}

// RuntimeDeliveryGate linearizes approval delivery acceptance against runtime
// activation/replacement/deactivation per session.
type RuntimeDeliveryGate struct {
	mu       sync.Mutex
	sessions map[string]*genEndpoint
}

type genEndpoint struct {
	runtime RuntimeRef
	active  bool
	sink    DeliverySink
}

func NewRuntimeDeliveryGate() *RuntimeDeliveryGate {
	return &RuntimeDeliveryGate{sessions: make(map[string]*genEndpoint)}
}

// Activate installs rt as the current generation-owned endpoint for a session,
// superseding any prior generation. sink is nil in production (no channel).
func (g *RuntimeDeliveryGate) Activate(sessionID string, rt RuntimeRef, sink DeliverySink) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sessions[sessionID] = &genEndpoint{runtime: rt, active: true, sink: sink}
}

// Deactivate marks a session's endpoint inactive (delete/unlink/termination).
func (g *RuntimeDeliveryGate) Deactivate(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, ok := g.sessions[sessionID]; ok {
		e.active = false
	}
}

// Accept is the sole acceptance point. Under one lock it verifies the current
// endpoint equals the expected runtime, is active, and has a sink, then enqueues the
// exact bytes into that endpoint and returns a ReceiptID — atomically. A concurrent
// Activate/Deactivate cannot interleave. Returns ok=false (no bytes accepted) when
// the runtime was replaced, removed, or has no sink.
func (g *RuntimeDeliveryGate) Accept(sessionID string, expected RuntimeRef, payload []byte) (receiptID string, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.sessions[sessionID]
	if e == nil || !e.active || !e.runtime.equal(expected) || e.sink == nil {
		return "", false
	}
	return e.sink.Enqueue(payload), true
}

// gatedApprovalDelivery is the production boundary: it accepts ONLY through the
// generation-owned gate. With no sink registered (no provider channel) it returns
// `unavailable` and writes no bytes; when a sink exists, an accepted receipt is
// created only after the daemon-owned acceptance, carrying the delivered-payload digest.
type gatedApprovalDelivery struct {
	gate *RuntimeDeliveryGate
}

// NewGatedApprovalDelivery wires the production delivery boundary to the gate.
func NewGatedApprovalDelivery(gate *RuntimeDeliveryGate) ApprovalDelivery {
	return gatedApprovalDelivery{gate: gate}
}

func (d gatedApprovalDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	rid, ok := d.gate.Accept(req.Binding.SessionID, req.Binding.Runtime, req.Payload)
	if !ok {
		return DeliveryReceipt{Outcome: DeliveryUnavailable, ClaimToken: req.ClaimToken, Binding: req.Binding}
	}
	return DeliveryReceipt{
		Outcome: DeliveryAccepted, ClaimToken: req.ClaimToken, Binding: req.Binding,
		ReceiptID: rid, DeliveredPayloadDigest: payloadDigest(req.Payload),
	}
}
