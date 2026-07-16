// Package term — C2D-C: uninstalled Claude ApprovalDelivery boundary.
//
// C-R1: real decision delivery. The boundary writes the exact decision
// bytes through a concrete ClaudeResponseWriter, binds the expected
// witness kind before I/O, and preallocates the receipt ID before the
// first provider write.
//
// C-R2: terminal cleanup. Every failure path cleans up the reservation
// and identity. Nil dependencies fail closed. No lock is held across
// spawn, hook response I/O, or stream reading.
//
// This is controlled-composition work: NOT wired into production app.go.
// C3D will activate it atomically.
package term

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"time"
)

const defaultClaudeDeliveryTimeout = 120 * time.Second

// ── Concrete I/O boundaries ──

// ClaudeResponseWriter writes the exact decision bytes to the Claude
// hook HTTP response. In production, this is the hook bridge handler.
// In tests, a recording stub captures the written bytes.
type ClaudeResponseWriter interface {
	WriteResponse(decision string) error
}

// ClaudeWitnessRoute is a production-shaped witness source. It blocks
// until a consumption witness arrives or the deadline expires.
// In production, PostToolUseRoute parses the resumed Claude stream-json
// for a matching PostToolUse event. PermissionDenialRoute parses the
// same stream for a matching permission_denials event.
type ClaudeWitnessRoute func(sessionID, toolUseID string, timeout time.Duration) (ClaudeWitness, error)

// ClaudeWitness contains the fields extracted from a Claude consumption
// witness.
type ClaudeWitness struct {
	SessionID   string
	ToolUseID   string
	ToolName    string
	InputDigest string
	Runtime     RuntimeRef
}

// ErrWitnessTimeout is returned when no witness arrives within the deadline.
var ErrWitnessTimeout = &witnessTimeoutError{}

type witnessTimeoutError struct{}

func (e *witnessTimeoutError) Error() string { return "claude witness timeout" }

// ── Delivery boundary ──

// ClaudeManagedApprovalDelivery implements ApprovalDelivery for Claude.
// It is NOT installed in production; C3D will activate it.
type ClaudeManagedApprovalDelivery struct {
	svc              *ManagedClaudeService
	timeout          time.Duration
	barrier          func(stage string)
	responseWriter   ClaudeResponseWriter
	postToolUseRoute ClaudeWitnessRoute
	denialRoute      ClaudeWitnessRoute
}

// NewClaudeManagedApprovalDelivery creates an uninstalled delivery
// boundary. All I/O dependencies must be non-nil; nil routes fail
// closed with DeliveryUnavailable.
func NewClaudeManagedApprovalDelivery(svc *ManagedClaudeService, rw ClaudeResponseWriter, allowRoute, denyRoute ClaudeWitnessRoute) *ClaudeManagedApprovalDelivery {
	return &ClaudeManagedApprovalDelivery{
		svc:              svc,
		timeout:          defaultClaudeDeliveryTimeout,
		responseWriter:   rw,
		postToolUseRoute: allowRoute,
		denialRoute:      denyRoute,
	}
}

// Deliver implements ApprovalDelivery. Every failure path cleans up the
// reservation and returns a non-success receipt.
func (d *ClaudeManagedApprovalDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	fail := func(o DeliveryOutcome) DeliveryReceipt {
		return DeliveryReceipt{Outcome: o, ClaimToken: req.ClaimToken, Binding: req.Binding}
	}

	// 0. Nil-dependency check first — before any validation.
	if d.svc == nil || d.svc.coordinator == nil || d.responseWriter == nil {
		return fail(DeliveryUnavailable)
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
	decision, ok := certifiedClaudeDecision[b.OptionID]
	if !ok {
		return fail(DeliveryRejected)
	}

	// 2. Preallocate receipt ID BEFORE any provider I/O.
	receiptID, ok := newClaudeReceiptID()
	if !ok {
		return fail(DeliveryConflict)
	}

	// 3. Resolve coordinator.
	coord := d.svc.coordinator
	if coord == nil {
		return fail(DeliveryUnavailable)
	}

	// 4. Reserve an entry from the stored identity.
	handle, ok := coord.ReserveEntry(req.ClaimToken, b)
	if !ok {
		return fail(DeliveryUnavailable)
	}
	if d.barrier != nil {
		d.barrier("post-reserve")
	}

	// 5. Look up the identity for hook fields.
	id, ok := coord.LookupIdentity(b.ApprovalID)
	if !ok {
		d.cleanupPreWrite(coord, req.ClaimToken, b.ApprovalID)
		return fail(DeliveryUnavailable)
	}

	// 6. Claim the write — transitions reserved→writeClaimed.
	_, outcome := coord.ClaimWrite(handle.ClaimToken, handle.ResumeNonce,
		id.sessionID, id.toolUseID, id.toolName, id.inputDigest)
	if outcome != outcomeWritten {
		d.cleanupPreWrite(coord, req.ClaimToken, b.ApprovalID)
		return fail(DeliveryRejected)
	}
	if d.barrier != nil {
		d.barrier("post-claim")
	}

	// 7. Write the exact decision bytes through the concrete I/O boundary.
	// This is the provider write — OUTSIDE the coordinator lock.
	writeErr := d.responseWriter.WriteResponse(decision)
	if writeErr != nil {
		// C-R2: write failure — confirm failure, clean up, return ambiguous.
		coord.ConfirmWrite(handle.ClaimToken, false)
		d.cleanupPreWrite(coord, req.ClaimToken, b.ApprovalID)
		return fail(DeliveryConflict)
	}

	// 8. Confirm the write succeeded.
	outcome = coord.ConfirmWrite(handle.ClaimToken, true)
	if outcome != outcomeWritten {
		d.cleanupPreWrite(coord, req.ClaimToken, b.ApprovalID)
		return fail(DeliveryConflict)
	}
	if d.barrier != nil {
		d.barrier("post-confirm")
	}

	// 9. Bind the expected witness kind BEFORE I/O.
	var expectedKind WitnessKind
	switch decision {
	case "allow":
		expectedKind = WitnessPostToolUse
	case "deny":
		expectedKind = WitnessPermissionDenials
	default:
		d.cleanupPreWrite(coord, req.ClaimToken, b.ApprovalID)
		return fail(DeliveryRejected)
	}

	// 10. Wait for the consumption witness through the production-shaped route.
	timeout := d.timeout
	if timeout <= 0 {
		timeout = defaultClaudeDeliveryTimeout
	}
	witness, err := d.waitForWitness(expectedKind, id.sessionID, id.toolUseID, timeout)
	if err != nil {
		d.cleanupPreWrite(coord, req.ClaimToken, b.ApprovalID)
		return fail(DeliveryConflict)
	}

	// 11. Mark the witness — validates identity and returns stored binding.
	storedBinding, ok := coord.MarkWitnessed(handle.ClaimToken, expectedKind,
		witness.SessionID, witness.ToolUseID, witness.ToolName,
		witness.InputDigest, witness.Runtime)
	if !ok {
		d.cleanupPreWrite(coord, req.ClaimToken, b.ApprovalID)
		return fail(DeliveryRejected)
	}
	// Consume the identity so duplicate claims cannot reuse it.
	coord.RemoveIdentity(b.ApprovalID)

	// 12. Return success with the preallocated receipt ID.
	return DeliveryReceipt{
		Outcome:                DeliveryAccepted,
		ClaimToken:             req.ClaimToken,
		Binding:                storedBinding,
		ReceiptID:              receiptID,
		DeliveredPayloadDigest: payloadDigest(req.Payload),
	}
}

// waitForWitness selects the correct production-shaped route based on the
// expected witness kind.
func (d *ClaudeManagedApprovalDelivery) waitForWitness(kind WitnessKind, sessionID, toolUseID string, timeout time.Duration) (ClaudeWitness, error) {
	switch kind {
	case WitnessPostToolUse:
		if d.postToolUseRoute == nil {
			return ClaudeWitness{}, fmt.Errorf("post-tool-use route unavailable")
		}
		return d.postToolUseRoute(sessionID, toolUseID, timeout)
	case WitnessPermissionDenials:
		if d.denialRoute == nil {
			return ClaudeWitness{}, fmt.Errorf("permission-denial route unavailable")
		}
		return d.denialRoute(sessionID, toolUseID, timeout)
	default:
		return ClaudeWitness{}, fmt.Errorf("unknown witness kind: %d", kind)
	}
}

// cleanupPreWrite cleans up the coordinator reservation and identity.
// Called on any failure before (or at) ConfirmWrite. After ConfirmWrite,
// the coordinator entry is in decisionWritten or terminal state and will
// be cleaned up by timeout/clear mechanisms.
func (d *ClaudeManagedApprovalDelivery) cleanupPreWrite(coord *claudeResumeCoordinator, claimToken, approvalID string) {
	if coord == nil {
		return
	}
	coord.CancelEntry(claimToken)
	coord.RemoveIdentity(approvalID)
}

// ── Receipt ID ──

func newClaudeReceiptID() (string, bool) {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", false
	}
	return "cldr-" + hex.EncodeToString(b[:]), true
}
