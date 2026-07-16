// Package term — C2D-C: uninstalled Claude ApprovalDelivery boundary.
//
// R2-A: real resume flow. Deliver calls ManagedClaudeService.ResumeForApproval
// to spawn Claude with --resume, isolated hook settings, and resume+posttool
// endpoints. The hook bridge (not Deliver) performs ClaimWrite/ConfirmWrite.
// Deliver waits for the coordinator to reach terminal state.
//
// R2-B: canonical response bytes. The exact hook response encoding matches
// the C0D-certified format; receipt digest hashes those bytes.
package term

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"time"
)

const defaultClaudeDeliveryTimeout = 120 * time.Second

// claudeHookResponse encodes the C0D-certified hook response for a decision.
// Must match the encoding in claudeHookBridge.handleResume exactly.
func claudeHookResponse(decision string) []byte {
	return []byte(fmt.Sprintf(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"%s"}}`, decision))
}

// ClaudeManagedApprovalDelivery implements ApprovalDelivery for Claude.
// It uses the real ManagedClaudeService resume path and the C2D-B
// coordinator. NOT installed in production; C3D will activate it.
type ClaudeManagedApprovalDelivery struct {
	svc     *ManagedClaudeService
	timeout time.Duration
	barrier func(stage string)
}

// NewClaudeManagedApprovalDelivery creates an uninstalled delivery boundary.
func NewClaudeManagedApprovalDelivery(svc *ManagedClaudeService) *ClaudeManagedApprovalDelivery {
	return &ClaudeManagedApprovalDelivery{
		svc:     svc,
		timeout: defaultClaudeDeliveryTimeout,
	}
}

// SetPollTimeout sets the deadline for waiting on the coordinator terminal
// state. Exported for tests that use fake processes (no real hook exchange).
func (d *ClaudeManagedApprovalDelivery) SetPollTimeout(dur time.Duration) {
	d.timeout = dur
}

// Deliver implements ApprovalDelivery. It reserves a coordinator entry,
// spawns a resumed Claude process via the managed service, waits for the
// terminal coordinator result, and constructs a receipt.
func (d *ClaudeManagedApprovalDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	fail := func(o DeliveryOutcome) DeliveryReceipt {
		return DeliveryReceipt{Outcome: o, ClaimToken: req.ClaimToken, Binding: req.Binding}
	}

	// 0. Nil-dependency check.
	if d.svc == nil || d.svc.coordinator == nil {
		return fail(DeliveryUnavailable)
	}

	// 1. Canonical metadata + payload digest.
	if !validGateBindingMeta(req) {
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

	// 2. Preallocate receipt ID.
	receiptID, ok := newClaudeReceiptID()
	if !ok {
		return fail(DeliveryConflict)
	}

	// 3. Reserve the coordinator entry.
	coord := d.svc.coordinator
	handle, ok := coord.ReserveEntry(req.ClaimToken, b)
	if !ok {
		return fail(DeliveryUnavailable)
	}
	if d.barrier != nil {
		d.barrier("post-reserve")
	}

	// 4. Look up identity for Claude session ID.
	id, ok := coord.LookupIdentity(b.ApprovalID)
	if !ok {
		coord.CancelEntry(req.ClaimToken)
		coord.RemoveIdentity(b.ApprovalID)
		return fail(DeliveryUnavailable)
	}

	// 5. Spawn the resumed Claude process. The hook bridge will call
	//    ClaimWrite → write response → ConfirmWrite, and PostToolUse
	//    will call MarkWitnessed. We wait for the terminal result.
	rt, err := d.svc.ResumeForApproval(handle.ClaimToken, handle.ResumeNonce, id.sessionID, "/tmp")
	if err != nil {
		coord.CancelEntry(req.ClaimToken)
		coord.RemoveIdentity(b.ApprovalID)
		return fail(DeliveryUnavailable)
	}
	if d.barrier != nil {
		d.barrier("post-resume-spawn")
	}

	// 6. Wait for the coordinator to reach terminal state.
	//    Success: MarkWitnessed removes both entry and identity.
	//    Failure: CancelEntry + RemoveIdentity cleans up.
	timeout := d.timeout
	if timeout <= 0 {
		timeout = defaultClaudeDeliveryTimeout
	}
	deadline := time.Now().Add(timeout)

	terminal := false
	for time.Now().Before(deadline) {
		if coord.identityCount() == 0 {
			terminal = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !terminal {
		coord.CancelEntry(req.ClaimToken)
		coord.RemoveIdentity(b.ApprovalID)
		rt.terminate()
		return fail(DeliveryConflict)
	}

	// 7. Construct receipt with the exact hook response digest.
	responseBytes := claudeHookResponse(decision)
	return DeliveryReceipt{
		Outcome:                DeliveryAccepted,
		ClaimToken:             req.ClaimToken,
		Binding:                b,
		ReceiptID:              receiptID,
		DeliveredPayloadDigest: payloadDigest(responseBytes),
	}
}

func newClaudeReceiptID() (string, bool) {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", false
	}
	return "cldr-" + hex.EncodeToString(b[:]), true
}
