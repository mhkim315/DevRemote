package term

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// A1 remediation 2 (R2-C, supplement #3/#4) — approval-specific delivery boundary
// and generation-bound delivery gate.
//
// A delivery request and its receipt carry the ONE immutable
// ApprovalExecutionBinding the claim token owns; the receipt also has an opaque
// ReceiptID. RecordDelivery compares every binding field and the claim token before
// a success may commit. The generic CommandBroker remains removed from this path.
//
// Linearization: replacement/delete/unlink/termination and delivery share the
// per-session RuntimeDeliveryGate. Delivery is accepted ONLY inside the gate, for
// the exact current+active runtime generation, so an old generation accepts no
// bytes after replacement begins, and no external I/O is performed while the store
// mutex is held.
//
// No accepted provider action-delivery channel exists today, so the production
// boundary is `unavailableApprovalDelivery`, which accepts nothing and writes no
// bytes. The gate + receipt semantics are proven with a controlled fixture; that
// fixture is never used to claim a production positive path.

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

// ApprovalDeliveryRequest carries the claim token, the immutable binding, and the
// exact server-side payload. Every identity field lives in Binding.
type ApprovalDeliveryRequest struct {
	ClaimToken string
	Binding    ApprovalExecutionBinding
	Payload    []byte
}

// DeliveryReceipt is the immutable, fully-bound result of a delivery attempt.
type DeliveryReceipt struct {
	Outcome    DeliveryOutcome
	ClaimToken string
	Binding    ApprovalExecutionBinding
	ReceiptID  string // opaque; present on an accepted receipt
}

// ApprovalDelivery is the daemon-owned approval delivery boundary.
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

// unavailableApprovalDelivery is the production boundary: no accepted channel, no
// bytes written, always `unavailable`.
type unavailableApprovalDelivery struct{}

func NewUnavailableApprovalDelivery() ApprovalDelivery { return unavailableApprovalDelivery{} }

func (unavailableApprovalDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	return DeliveryReceipt{Outcome: DeliveryUnavailable, ClaimToken: req.ClaimToken, Binding: req.Binding}
}

// RuntimeDeliveryGate serializes approval delivery acceptance against runtime
// replacement/removal per session. Replacement (SetActive) and removal (Deactivate)
// take the SAME lock as AcceptDelivery, so a delivery cannot be accepted for a
// generation that is being replaced, and an old generation accepts nothing once a
// newer one is set active. It performs NO external I/O and holds NO store lock.
type RuntimeDeliveryGate struct {
	mu  sync.Mutex
	cur map[string]gateEntry
}

type gateEntry struct {
	runtime RuntimeRef
	active  bool
}

func NewRuntimeDeliveryGate() *RuntimeDeliveryGate {
	return &RuntimeDeliveryGate{cur: make(map[string]gateEntry)}
}

// SetActive marks rt the current active runtime generation for a session (called on
// launch/replacement). It supersedes any prior generation.
func (g *RuntimeDeliveryGate) SetActive(sessionID string, rt RuntimeRef) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cur[sessionID] = gateEntry{runtime: rt, active: true}
}

// Deactivate marks a session's runtime inactive (delete/unlink/termination). After
// this, no delivery is accepted until a new generation is set active.
func (g *RuntimeDeliveryGate) Deactivate(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if e, ok := g.cur[sessionID]; ok {
		e.active = false
		g.cur[sessionID] = e
	}
}

// AcceptDelivery atomically checks that the session's current runtime is exactly the
// expected generation AND active, and (in the same critical section, without any
// external I/O) accepts. Returns false if the runtime was replaced, removed, or is
// not current — the linearization point.
func (g *RuntimeDeliveryGate) AcceptDelivery(sessionID string, expected RuntimeRef) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.cur[sessionID]
	if !ok || !e.active || !e.runtime.equal(expected) {
		return false
	}
	return true
}
