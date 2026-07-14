package term

// A1 remediation (R-B / B3) — approval-specific delivery boundary.
//
// The generic CommandBroker (a session-level overwrite mailbox with no ApprovalID,
// runtime identity, ActionDigest, idempotency key, or receipt) is NOT approval
// delivery authority. Approval actions are delivered ONLY through this dedicated
// boundary, whose request is bound to the exact claim token, approval, runtime,
// action digest, idempotency key, and server-side payload, and which returns a
// typed receipt. A generic command overwrite can neither satisfy nor be mistaken
// for an approval delivery.
//
// No accepted provider action-delivery channel exists today (Codex's log only
// OBSERVES its own resolution; there is no verified resolution protocol, and blind
// terminal Y/N synthesis is prohibited). The production boundary is therefore
// `unavailableApprovalDelivery`, which accepts nothing. The receipt semantics
// (accepted / already_accepted / conflict / stale_runtime / runtime_mismatch /
// rejected) are proven at the boundary contract level with a controlled fixture in
// tests; that fixture is never used to claim a production provider path.

// DeliveryOutcome is the frozen closed receipt vocabulary. Only `accepted` and
// `already_accepted` may transition an approval to a successful committed state.
type DeliveryOutcome string

const (
	DeliveryAccepted        DeliveryOutcome = "accepted"         // exact action accepted once by the owned boundary
	DeliveryAlreadyAccepted DeliveryOutcome = "already_accepted" // same key+digest already accepted
	DeliveryStaleRuntime    DeliveryOutcome = "stale_runtime"    // runtime generation moved since claim
	DeliveryRuntimeMismatch DeliveryOutcome = "runtime_mismatch" // adapter/provider/version mismatch
	DeliveryUnavailable     DeliveryOutcome = "unavailable"      // no accepted delivery channel for this runtime
	DeliveryConflict        DeliveryOutcome = "conflict"         // same key previously used with a different digest
	DeliveryRejected        DeliveryOutcome = "rejected"         // boundary refused the action
)

var deliveryOutcomeValid = map[DeliveryOutcome]bool{
	DeliveryAccepted: true, DeliveryAlreadyAccepted: true, DeliveryStaleRuntime: true,
	DeliveryRuntimeMismatch: true, DeliveryUnavailable: true, DeliveryConflict: true,
	DeliveryRejected: true,
}

// IsValidDeliveryOutcome reports membership in the closed receipt set.
func IsValidDeliveryOutcome(o DeliveryOutcome) bool { return deliveryOutcomeValid[o] }

// deliverySucceeded reports whether a receipt permits a successful committed state.
func deliverySucceeded(o DeliveryOutcome) bool {
	return o == DeliveryAccepted || o == DeliveryAlreadyAccepted
}

// ApprovalDeliveryRequest is the fully-bound request handed to the delivery
// boundary. Every field is server-derived. Payload is the exact server-side bytes
// to deliver; it is never sourced from or echoed to the public DTO.
type ApprovalDeliveryRequest struct {
	ClaimToken     string
	ApprovalID     string
	SessionID      string
	Runtime        RuntimeRef
	ActionDigest   string
	IdempotencyKey string
	Payload        []byte
}

// DeliveryReceipt is the immutable, bound result of a delivery attempt.
type DeliveryReceipt struct {
	Outcome        DeliveryOutcome
	ApprovalID     string
	SessionID      string
	ActionDigest   string
	IdempotencyKey string
}

// ApprovalDelivery is the daemon-owned approval delivery boundary.
type ApprovalDelivery interface {
	// Deliver attempts the exact bound action exactly once and returns a bound
	// receipt. It never panics and never performs generic terminal writes.
	Deliver(req ApprovalDeliveryRequest) DeliveryReceipt
}

// unavailableApprovalDelivery is the production boundary: no accepted provider
// action-delivery channel exists, so every delivery is `unavailable`. It performs
// NO terminal write. This is the honest state until a provider resolution channel
// is separately verified with controlled evidence.
type unavailableApprovalDelivery struct{}

// NewUnavailableApprovalDelivery returns the production (no-channel) boundary.
func NewUnavailableApprovalDelivery() ApprovalDelivery { return unavailableApprovalDelivery{} }

func (unavailableApprovalDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	return DeliveryReceipt{
		Outcome:        DeliveryUnavailable,
		ApprovalID:     req.ApprovalID,
		SessionID:      req.SessionID,
		ActionDigest:   req.ActionDigest,
		IdempotencyKey: req.IdempotencyKey,
	}
}
