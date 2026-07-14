package term

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"sync"
)

// A1 remediation 3/4 (R3-B/R3-C/R4-C) — approval-specific delivery boundary + a
// generation-owned delivery gate with a bounded, non-blocking, production-owned
// in-memory acceptance queue.
//
// A delivery request and its receipt carry the ONE immutable ApprovalExecutionBinding
// the claim token owns; the receipt also carries an opaque ReceiptID and the digest
// of the EXACT bytes accepted. RecordDelivery compares every binding field, the claim
// token, and the delivered-payload digest before a success may commit.
//
// Linearization (R4-C): the under-lock acceptance is a bounded append into a
// generation-owned in-memory queue owned by the gate — never an arbitrary interface
// callback. It cannot block or perform I/O; queue-full returns non-acceptance and
// writes nothing. Activation/replacement/deactivation take the SAME lock, so a
// replaced generation accepts no bytes. Any external drain reads the CAPTURED
// generation queue via Drain (a brief locked snapshot) and performs provider I/O
// AFTER releasing the lock, so a slow drain cannot hold the transition gate.
//
// No accepted provider action-delivery channel exists, so production activates every
// endpoint with capacity 0 (no channel) → Accept returns unavailable and no bytes are
// ever accepted. The gate/receipt semantics are proven with controlled tests.

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
// accepted NOTHING (so a bounded manual retry is safe).
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
	DeliveredPayloadDigest string
}

type ApprovalDelivery interface {
	Deliver(req ApprovalDeliveryRequest) DeliveryReceipt
}

func newGateNonce() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "n"
	}
	return hex.EncodeToString(b[:])
}

// unavailableApprovalDelivery is a no-channel boundary that accepts nothing.
type unavailableApprovalDelivery struct{}

func NewUnavailableApprovalDelivery() ApprovalDelivery { return unavailableApprovalDelivery{} }

func (unavailableApprovalDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	return DeliveryReceipt{Outcome: DeliveryUnavailable, ClaimToken: req.ClaimToken, Binding: req.Binding}
}

// genEndpoint is a per-session, generation-owned bounded delivery queue. capacity 0
// means no delivery channel (production) — nothing is ever accepted.
type genEndpoint struct {
	runtime  RuntimeRef
	active   bool
	capacity int
	queue    [][]byte
	seq      int
	nonce    string
}

// RuntimeDeliveryGate linearizes approval delivery acceptance against runtime
// activation/replacement/deactivation per session. It directly owns the concrete
// bounded queue; there is NO callback under the lock.
type RuntimeDeliveryGate struct {
	mu       sync.Mutex
	sessions map[string]*genEndpoint
}

func NewRuntimeDeliveryGate() *RuntimeDeliveryGate {
	return &RuntimeDeliveryGate{sessions: make(map[string]*genEndpoint)}
}

// Activate installs rt as the current generation-owned endpoint with a bounded
// acceptance capacity, superseding any prior generation. capacity 0 = no channel
// (production): the endpoint tracks generation currency but accepts nothing.
func (g *RuntimeDeliveryGate) Activate(sessionID string, rt RuntimeRef, capacity int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sessions[sessionID] = &genEndpoint{runtime: rt, active: true, capacity: capacity, nonce: newGateNonce()}
}

// Deactivate marks a session's endpoint inactive (replacement/delete/unlink/
// termination/registry disappearance). After this it accepts nothing.
func (g *RuntimeDeliveryGate) Deactivate(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, ok := g.sessions[sessionID]; ok {
		e.active = false
	}
}

// Accept is the sole acceptance point. Under one lock it verifies the current
// endpoint equals the expected runtime, is active, and has capacity, then appends the
// exact bytes into the generation-owned queue and returns a ReceiptID — a bounded,
// non-blocking, in-memory operation. Queue-full or a replaced/removed/no-channel
// generation returns ok=false and writes nothing. No external I/O occurs here.
func (g *RuntimeDeliveryGate) Accept(sessionID string, expected RuntimeRef, payload []byte) (receiptID string, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.sessions[sessionID]
	if e == nil || !e.active || !e.runtime.equal(expected) || e.capacity == 0 {
		return "", false
	}
	if len(e.queue) >= e.capacity {
		return "", false // fail closed
	}
	cp := append([]byte(nil), payload...)
	e.queue = append(e.queue, cp)
	e.seq++
	return e.nonce + "-" + strconv.Itoa(e.seq), true
}

// Drain returns and clears the session endpoint's accepted payloads under a brief
// lock. The caller performs any external/provider I/O AFTER this returns (outside the
// lock), so a slow drain never holds the transition gate.
func (g *RuntimeDeliveryGate) Drain(sessionID string) [][]byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.sessions[sessionID]
	if e == nil || len(e.queue) == 0 {
		return nil
	}
	out := e.queue
	e.queue = nil
	return out
}

// gatedApprovalDelivery is the production boundary: it accepts ONLY through the
// generation-owned gate. With capacity 0 (no provider channel) it returns
// `unavailable` and writes no bytes; when a bounded endpoint exists, an accepted
// receipt is created only after the daemon-owned acceptance, carrying the
// delivered-payload digest.
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
