// Package term — C2D-C: uninstalled Claude ApprovalDelivery boundary.
//
// C2D-B built the private resume coordinator. C2D-C wraps it in an
// ApprovalDelivery implementation that validates the binding, routes
// through the coordinator (ReserveEntry → ClaimWrite → ConfirmWrite →
// wait for witness → MarkWitnessed), and returns receipts only after
// provider-native consumption witnesses.
//
// This is controlled-composition work: the boundary is NOT wired into
// production app.go. C3D will activate it atomically.
package term

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"time"
)

const (
	defaultClaudeDeliveryTimeout = 120 * time.Second
)

// ClaudeWitness contains the fields extracted from a Claude consumption
// witness (PostToolUse or permission_denials stream-json event).
type ClaudeWitness struct {
	Kind        WitnessKind
	SessionID   string
	ToolUseID   string
	ToolName    string
	InputDigest string
	Runtime     RuntimeRef
}

// WitnessProvider is a function that waits for a Claude consumption
// witness. In production, it parses stream-json from the resumed
// Claude process. In tests, it returns a synthetic witness.
//
// Returns ErrWitnessTimeout if no witness arrives within the timeout.
type WitnessProvider func(claimToken string, kind WitnessKind, timeout time.Duration) (ClaudeWitness, error)

// ClaudeManagedApprovalDelivery implements ApprovalDelivery for the
// Claude managed runtime. It is NOT installed in production; C3D will
// wire it through the activation pattern.
type ClaudeManagedApprovalDelivery struct {
	svc             *ManagedClaudeService
	timeout         time.Duration
	barrier         func(stage string)
	witnessProvider WitnessProvider
}

// NewClaudeManagedApprovalDelivery creates an uninstalled delivery
// boundary. witnessProvider must not be nil — in production it parses
// the resumed Claude stream-json; in tests it returns synthetic witnesses.
func NewClaudeManagedApprovalDelivery(svc *ManagedClaudeService, wp WitnessProvider) *ClaudeManagedApprovalDelivery {
	return &ClaudeManagedApprovalDelivery{
		svc:             svc,
		timeout:         defaultClaudeDeliveryTimeout,
		witnessProvider: wp,
	}
}

// Deliver implements ApprovalDelivery. Every failure path returns a
// non-success receipt with ZERO provider writes.
func (d *ClaudeManagedApprovalDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	fail := func(o DeliveryOutcome) DeliveryReceipt {
		return DeliveryReceipt{Outcome: o, ClaimToken: req.ClaimToken, Binding: req.Binding}
	}

	// 1. Canonical metadata + payload digest BEFORE any state mutation.
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
	if b.Runtime.Adapter != claudeHeadlessAdapter || b.Runtime.StreamGen != 0 {
		return fail(DeliveryRuntimeMismatch)
	}
	if b.DeliverySchema != claudeDecisionSchemaV1 {
		return fail(DeliveryRejected)
	}
	if _, ok := certifiedClaudeDecision[b.OptionID]; !ok {
		return fail(DeliveryRejected)
	}

	// 2. Resolve the live coordinator.
	coord := d.svc.coordinator
	if coord == nil {
		return fail(DeliveryUnavailable)
	}

	// 3. Reserve an entry from the stored identity.
	handle, ok := coord.ReserveEntry(req.ClaimToken, b)
	if !ok {
		return fail(DeliveryUnavailable)
	}
	if d.barrier != nil {
		d.barrier("post-reserve")
	}

	// 4. Look up the identity for the hook fields.
	id, ok := coord.LookupIdentity(b.ApprovalID)
	if !ok {
		return fail(DeliveryUnavailable)
	}

	// 5. Claim the write — this transitions reserved→writeClaimed.
	// The decision string (WriteHandle.Decision()) is written to the
	// hook HTTP response OUTSIDE the coordinator lock. In production
	// this is the actual HTTP response; in tests the barrier allows
	// interleaving injection.
	_, outcome := coord.ClaimWrite(handle.ClaimToken, handle.ResumeNonce,
		id.sessionID, id.toolUseID, id.toolName, id.inputDigest)
	if outcome != outcomeWritten {
		return fail(DeliveryRejected)
	}
	if d.barrier != nil {
		d.barrier("post-claim")
	}

	// 7. Confirm the write.
	outcome = coord.ConfirmWrite(handle.ClaimToken, true)
	if outcome != outcomeWritten {
		// Write was ambiguous — the coordinator was invalidated or the
		// write-claim deadline expired.
		return fail(DeliveryConflict)
	}
	if d.barrier != nil {
		d.barrier("post-confirm")
	}

	// 8. Wait for the consumption witness.
	timeout := d.timeout
	if timeout <= 0 {
		timeout = defaultClaudeDeliveryTimeout
	}
	witness, err := d.witnessProvider(handle.ClaimToken, WitnessKind(0), timeout)
	if err != nil {
		return fail(DeliveryConflict)
	}

	// 9. Mark the witness — validates identity and returns stored binding.
	storedBinding, ok := coord.MarkWitnessed(handle.ClaimToken, witness.Kind,
		witness.SessionID, witness.ToolUseID, witness.ToolName,
		witness.InputDigest, witness.Runtime)
	if !ok {
		return fail(DeliveryRejected)
	}
	// Consume the identity so duplicate claims cannot reuse it.
	coord.RemoveIdentity(b.ApprovalID)

	// 10. Generate receipt and return success.
	rid, ok := newClaudeReceiptID()
	if !ok {
		return fail(DeliveryConflict)
	}
	return DeliveryReceipt{
		Outcome:                DeliveryAccepted,
		ClaimToken:             req.ClaimToken,
		Binding:                storedBinding,
		ReceiptID:              rid,
		DeliveredPayloadDigest: payloadDigest(req.Payload),
	}
}

// newClaudeReceiptID generates an opaque receipt identifier.
func newClaudeReceiptID() (string, bool) {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", false
	}
	return "cldr-" + hex.EncodeToString(b[:]), true
}

// ErrWitnessTimeout is returned by WitnessProvider when no witness
// arrives within the deadline.
var ErrWitnessTimeout = &witnessTimeoutError{}

type witnessTimeoutError struct{}

func (e *witnessTimeoutError) Error() string { return "claude witness timeout" }
