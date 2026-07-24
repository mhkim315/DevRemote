// Package term — C2D-C: uninstalled Claude ApprovalDelivery boundary.
//
// R3-A: claim-owned terminal result. Deliver waits on the coordinator's
// completion channel, not global identity counts.
//
// R3-B: canonical response bytes. Receipt digest matches the exact
// C0D-certified hook response encoding.
package term

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"time"
)

const defaultClaudeDeliveryTimeout = 120 * time.Second
const defaultClaudeDrainTimeout = 5 * time.Second

// drainWaiter waits for the resume process to exit naturally. Injected
// for deterministic tests; production uses a real timer-based waiter.
type drainWaiter func(exited <-chan struct{}, timeout time.Duration) bool

// ClaudeManagedApprovalDelivery implements ApprovalDelivery for Claude.
type ClaudeManagedApprovalDelivery struct {
	svc          *ManagedClaudeService
	timeout      time.Duration
	drainTimeout time.Duration
	barrier      func(stage string)
	drainWait    drainWaiter // nil means use production timer
}

func NewClaudeManagedApprovalDelivery(svc *ManagedClaudeService) *ClaudeManagedApprovalDelivery {
	return &ClaudeManagedApprovalDelivery{
		svc: svc, timeout: defaultClaudeDeliveryTimeout,
		drainTimeout: defaultClaudeDrainTimeout,
	}
}

func (d *ClaudeManagedApprovalDelivery) SetPollTimeout(dur time.Duration) { d.timeout = dur }

// SetDrainTimeout sets the post-witness natural-exit grace period.
// Use 0 in deterministic tests to skip the drain entirely.
func (d *ClaudeManagedApprovalDelivery) SetDrainTimeout(dur time.Duration) { d.drainTimeout = dur }

// SetDrainWaiter injects a custom drain wait function for deterministic
// tests. Pass nil to restore production timer-based waiter.
func (d *ClaudeManagedApprovalDelivery) SetDrainWaiter(w drainWaiter) { d.drainWait = w }

// productionDrainWait is the default timer-based drain.
func productionDrainWait(exited <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-exited:
		return true
	case <-timer.C:
		return false
	}
}

func (d *ClaudeManagedApprovalDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	fail := func(o DeliveryOutcome) DeliveryReceipt {
		return DeliveryReceipt{Outcome: o, ClaimToken: req.ClaimToken, Binding: req.Binding}
	}

	if d.svc == nil || d.svc.coordinator == nil {
		return fail(DeliveryUnavailable)
	}
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

	// Verify payload matches the canonical response bytes.
	expectedPayload := claudeHookResponseBytes(decision)
	if len(req.Payload) == 0 || !bytesEqual(req.Payload, expectedPayload) {
		return fail(DeliveryRejected)
	}
	if payloadDigest(req.Payload) != b.PayloadDigest {
		return fail(DeliveryRejected)
	}

	// Preallocate receipt ID.
	receiptID, ok := newClaudeReceiptID()
	if !ok {
		return fail(DeliveryConflict)
	}

	coord := d.svc.coordinator
	handle, ok := coord.ReserveEntry(req.ClaimToken, b, req.EpochRecheck)
	if !ok {
		return fail(DeliveryUnavailable)
	}
	if d.barrier != nil {
		d.barrier("post-reserve")
	}

	id, ok := coord.LookupIdentity(b.ApprovalID)
	if !ok {
		coord.CancelEntry(req.ClaimToken)
		coord.RemoveIdentity(b.ApprovalID)
		return fail(DeliveryUnavailable)
	}

	// Build the immutable resume context with original cwd.
	resumeCWD := "/tmp"
	d.svc.mu.Lock()
	if origRT, ok2 := d.svc.runtimes[b.SessionID]; ok2 && origRT.cwd != "" {
		resumeCWD = origRT.cwd
	}
	d.svc.mu.Unlock()

	ctx := &resumeContext{
		coordinator:      coord,
		claimToken:       handle.ClaimToken,
		resumeNonce:      handle.ResumeNonce,
		originalRuntime:  b.Runtime,
		pokitSessionID:   b.SessionID,
		claudeSessionID:  id.sessionID,
		toolUseID:        id.toolUseID,
		toolName:         id.toolName,
		inputDigest:      id.inputDigest,
		expectedDecision: decision,
		originalCWD:      resumeCWD,
	}

	// Spawn the resumed Claude process. The bridge runs ClaimWrite →
	// write response → ConfirmWrite, and PostToolUse → MarkWitnessed.
	rt, err := d.svc.ResumeForApproval(handle, ctx)
	if err != nil {
		coord.CancelEntry(req.ClaimToken)
		coord.RemoveIdentity(b.ApprovalID)
		return fail(DeliveryUnavailable)
	}
	defer d.svc.finishApprovalResume(rt)
	if d.barrier != nil {
		d.barrier("post-resume-spawn")
	}

	// Wait for the claim-owned terminal result.
	timeout := d.timeout
	if timeout <= 0 {
		timeout = defaultClaudeDeliveryTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var result TerminalResult
	select {
	case result = <-handle.Completion:
	case <-timer.C:
		coord.CancelEntry(req.ClaimToken)
		coord.RemoveIdentity(b.ApprovalID)
		return fail(DeliveryConflict)
	}

	if result.Outcome != TerminalWitnessed {
		return fail(DeliveryConflict)
	}
	rt.emitOperational(OperationalApprovalResolved, b.ApprovalID, "approval/witnessed", b.ApprovalID)
	if decision == "allow" {
		rt.emitOperational(OperationalToolCallFinished, ctx.toolUseID, "PostToolUse", ctx.toolUseID)
	}

	// Allow the resume process to exit naturally within a bounded
	// window. The witness is committed, but tool_result stdout may
	// still be in-flight. Killing immediately (defer terminate) would
	// truncate the pipe. Tests inject a channel-barrier drainWaiter
	// or set drainTimeout=0 to skip.
	if d.drainTimeout > 0 {
		w := d.drainWait
		if w == nil {
			w = productionDrainWait
		}
		w(rt.exited, d.drainTimeout)
	}

	// Construct receipt with the exact response digest.
	return DeliveryReceipt{
		Outcome:                DeliveryAccepted,
		ClaimToken:             req.ClaimToken,
		Binding:                result.Binding,
		ReceiptID:              receiptID,
		DeliveredPayloadDigest: result.ExactResponseDigest,
	}
}

func newClaudeReceiptID() (string, bool) {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", false
	}
	return "cldr-" + hex.EncodeToString(b[:]), true
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
