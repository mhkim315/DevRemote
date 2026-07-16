package term

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ── Test helpers ──

func newTestClaudeDelivery() (*ClaudeManagedApprovalDelivery, *ManagedClaudeService, *AuthoritativeApprovalStore) {
	store := NewApprovalStore()
	svc := NewManagedClaudeService(PinnedClaudeConfig(), &fakeClaudeLauncher{}, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)

	var wp WitnessProvider // set per-test
	d := &ClaudeManagedApprovalDelivery{
		svc:             svc,
		timeout:         5 * time.Second,
		witnessProvider: wp,
	}
	return d, svc, store
}

// setupDeliveryApproval creates an identity, store record, and coordinator
// entry ready for delivery. Returns the approvalID, binding, and claim token.
func setupDeliveryApproval(t *testing.T, svc *ManagedClaudeService, store *AuthoritativeApprovalStore, optionID string) (approvalID string, binding ApprovalExecutionBinding, claimToken string) {
	t.Helper()

	// Create a runtime so coordinator identities can be stored.
	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}

	// Inject an identity directly into the coordinator.
	sessionID := "claude-sess-test"
	toolUseID := "call_delivery_test"
	toolName := "Bash"
	inputDigest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	pokitSID := id
	rt := RuntimeRef{Adapter: claudeHeadlessAdapter, Version: "2.1.209", LaunchGen: 1, StreamGen: 0}
	approvalID = "claude-delivery-test"

	if !svc.coordinator.ReserveIdentity(approvalID, sessionID, toolUseID, toolName, inputDigest, pokitSID, rt) {
		t.Fatal("ReserveIdentity failed")
	}

	binding = ApprovalExecutionBinding{
		ApprovalID:     approvalID,
		SessionID:      pokitSID,
		Runtime:        rt,
		ActionDigest:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PayloadDigest:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		IdempotencyKey: "test.delivery.key",
		OptionID:       optionID,
		DeliverySchema: claudeDecisionSchemaV1,
	}
	claimToken = "cccccccccccccccccccccccccccccccc"
	return
}

// makeWitness creates a WitnessProvider that returns the given witness after
// a short delay. Set delay=0 for immediate return.
func makeWitness(kind WitnessKind, sid, tuid, tn, dig string, rt RuntimeRef, delay time.Duration) WitnessProvider {
	return func(claimToken string, _ WitnessKind, timeout time.Duration) (ClaudeWitness, error) {
		if delay > 0 {
			time.Sleep(delay)
		}
		return ClaudeWitness{
			Kind: kind, SessionID: sid, ToolUseID: tuid,
			ToolName: tn, InputDigest: dig, Runtime: rt,
		}, nil
	}
}

// makeTimeoutWitness returns a WitnessProvider that never responds.
func makeTimeoutWitness() WitnessProvider {
	return func(claimToken string, _ WitnessKind, timeout time.Duration) (ClaudeWitness, error) {
		time.Sleep(timeout + 100*time.Millisecond)
		return ClaudeWitness{}, ErrWitnessTimeout
	}
}

// makeChannelWitness returns a WitnessProvider that waits on a channel.
func makeChannelWitness(ch <-chan ClaudeWitness) WitnessProvider {
	return func(claimToken string, _ WitnessKind, timeout time.Duration) (ClaudeWitness, error) {
		select {
		case w := <-ch:
			return w, nil
		case <-time.After(timeout):
			return ClaudeWitness{}, ErrWitnessTimeout
		}
	}
}

// ── Positive path tests ──

func TestClaudeDelivery_Allow(t *testing.T) {
	d, svc, _ := setupDeliveryTest(t)
	approvalID, binding, claimToken := setupDeliveryApproval(t, svc, nil, "allow_once")

	witness := makeWitness(WitnessPostToolUse, "claude-sess-test", "call_delivery_test",
		"Bash", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		binding.Runtime, 0)
	d.witnessProvider = witness

	payload := []byte(`{"decision":"allow"}`) // placeholder — real payload is hook response
	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	// Override payload digest to match.
	req.Binding.PayloadDigest = payloadDigest(payload)

	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryAccepted {
		t.Fatalf("expected DeliveryAccepted, got %s", receipt.Outcome)
	}
	if receipt.ReceiptID == "" {
		t.Fatal("expected non-empty ReceiptID")
	}
	if receipt.ClaimToken != claimToken {
		t.Fatal("receipt ClaimToken mismatch")
	}
	_ = approvalID
}

func TestClaudeDelivery_Deny(t *testing.T) {
	d, svc, _ := setupDeliveryTest(t)
	_, binding, claimToken := setupDeliveryApproval(t, svc, nil, "deny")

	witness := makeWitness(WitnessPermissionDenials, "claude-sess-test", "call_delivery_test",
		"Bash", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		binding.Runtime, 0)
	d.witnessProvider = witness

	payload := []byte(`{"decision":"deny"}`)
	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	req.Binding.PayloadDigest = payloadDigest(payload)

	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryAccepted {
		t.Fatalf("expected DeliveryAccepted for deny, got %s", receipt.Outcome)
	}
}

// ── Binding validation tests ──

func TestClaudeDelivery_WrongAdapter(t *testing.T) {
	d, svc, _ := setupDeliveryTest(t)
	_, binding, claimToken := setupDeliveryApproval(t, svc, nil, "allow_once")

	binding.Runtime.Adapter = "codex_app_server"
	payload := []byte(`{"decision":"allow"}`)
	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	req.Binding.PayloadDigest = payloadDigest(payload)

	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryRuntimeMismatch {
		t.Fatalf("expected DeliveryRuntimeMismatch, got %s", receipt.Outcome)
	}
}

func TestClaudeDelivery_WrongSchema(t *testing.T) {
	d, svc, _ := setupDeliveryTest(t)
	_, binding, claimToken := setupDeliveryApproval(t, svc, nil, "allow_once")

	binding.DeliverySchema = "wrong.schema"
	payload := []byte(`{"decision":"allow"}`)
	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	req.Binding.PayloadDigest = payloadDigest(payload)

	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryRejected {
		t.Fatalf("expected DeliveryRejected, got %s", receipt.Outcome)
	}
}

func TestClaudeDelivery_WrongOptionID(t *testing.T) {
	d, svc, _ := setupDeliveryTest(t)
	_, binding, claimToken := setupDeliveryApproval(t, svc, nil, "allow_once")

	binding.OptionID = "cancel"
	payload := []byte(`{"decision":"allow"}`)
	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	req.Binding.PayloadDigest = payloadDigest(payload)

	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryRejected {
		t.Fatalf("expected DeliveryRejected, got %s", receipt.Outcome)
	}
}

func TestClaudeDelivery_SubstitutedPayload(t *testing.T) {
	d, svc, _ := setupDeliveryTest(t)
	_, binding, claimToken := setupDeliveryApproval(t, svc, nil, "allow_once")

	payload := []byte(`{"decision":"allow"}`)
	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	// Deliberately leave PayloadDigest mismatched.

	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryRejected {
		t.Fatalf("expected DeliveryRejected for mismatched digest, got %s", receipt.Outcome)
	}
}

// ── Duplicate / lifecycle tests ──

func TestClaudeDelivery_DuplicateClaim(t *testing.T) {
	d, svc, _ := setupDeliveryTest(t)
	_, binding, claimToken := setupDeliveryApproval(t, svc, nil, "allow_once")

	witness := makeWitness(WitnessPostToolUse, "claude-sess-test", "call_delivery_test",
		"Bash", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		binding.Runtime, 0)
	d.witnessProvider = witness

	payload := []byte(`{"decision":"allow"}`)
	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	req.Binding.PayloadDigest = payloadDigest(payload)

	// First delivery succeeds.
	r1 := d.Deliver(req)
	if r1.Outcome != DeliveryAccepted {
		t.Fatalf("first delivery should succeed, got %s", r1.Outcome)
	}

	// Second delivery with same claim token — identity consumed, ReserveEntry fails.
	r2 := d.Deliver(req)
	if r2.Outcome != DeliveryUnavailable {
		t.Fatalf("second delivery should fail, got %s", r2.Outcome)
	}
}

func TestClaudeDelivery_Timeout(t *testing.T) {
	d, svc, _ := setupDeliveryTest(t)
	d.timeout = 100 * time.Millisecond
	_, binding, claimToken := setupDeliveryApproval(t, svc, nil, "allow_once")

	d.witnessProvider = makeTimeoutWitness()

	payload := []byte(`{"decision":"allow"}`)
	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	req.Binding.PayloadDigest = payloadDigest(payload)

	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryConflict {
		t.Fatalf("expected DeliveryConflict for timeout, got %s", receipt.Outcome)
	}
}

// ── Barrier-based interleaving tests ──

func TestClaudeDelivery_ClaimThenCancel(t *testing.T) {
	d, svc, _ := setupDeliveryTest(t)
	_, binding, claimToken := setupDeliveryApproval(t, svc, nil, "allow_once")

	// Inject cancel at post-claim barrier.
	d.barrier = func(stage string) {
		if stage == "post-claim" {
			svc.coordinator.Close()
		}
	}

	payload := []byte(`{"decision":"allow"}`)
	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	req.Binding.PayloadDigest = payloadDigest(payload)

	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryConflict {
		t.Fatalf("expected DeliveryConflict after cancel at claim, got %s", receipt.Outcome)
	}
}

func TestClaudeDelivery_WitnessRace(t *testing.T) {
	d, svc, _ := setupDeliveryTest(t)
	d.timeout = 5 * time.Second
	_, binding, claimToken := setupDeliveryApproval(t, svc, nil, "allow_once")

	// Use a channel-based witness so we can deliver it concurrently.
	witnessCh := make(chan ClaudeWitness, 1)
	d.witnessProvider = makeChannelWitness(witnessCh)

	payload := []byte(`{"decision":"allow"}`)
	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	req.Binding.PayloadDigest = payloadDigest(payload)

	var receipt DeliveryReceipt
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		receipt = d.Deliver(req)
	}()

	// Simulate a brief delay then deliver the witness.
	time.Sleep(50 * time.Millisecond)
	witnessCh <- ClaudeWitness{
		Kind: WitnessPostToolUse, SessionID: "claude-sess-test",
		ToolUseID: "call_delivery_test", ToolName: "Bash",
		InputDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Runtime: binding.Runtime,
	}
	wg.Wait()

	if receipt.Outcome != DeliveryAccepted {
		t.Fatalf("expected DeliveryAccepted with concurrent witness, got %s", receipt.Outcome)
	}
}

func TestClaudeDelivery_AllowVsDeny(t *testing.T) {
	d, svc, _ := setupDeliveryTest(t)

	// Allow path.
	approvalID1, binding1, claimToken1 := setupDeliveryApproval(t, svc, nil, "allow_once")
	witness1 := makeWitness(WitnessPostToolUse, "claude-sess-test", "call_delivery_test",
		"Bash", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		binding1.Runtime, 0)
	d.witnessProvider = witness1
	payload1 := []byte(`{"decision":"allow"}`)
	req1 := ApprovalDeliveryRequest{ClaimToken: claimToken1, Binding: binding1, Payload: payload1}
	req1.Binding.PayloadDigest = payloadDigest(payload1)
	r1 := d.Deliver(req1)
	if r1.Outcome != DeliveryAccepted {
		t.Fatalf("allow delivery failed: %s", r1.Outcome)
	}

	// Deny path — need a new identity and entry.
	approvalID2 := approvalID1 + "-deny"
	rt := binding1.Runtime
	svc.coordinator.ReserveIdentity(approvalID2, "claude-sess-test", "call_delivery_test",
		"Bash", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		binding1.SessionID, rt)
	binding2 := ApprovalExecutionBinding{
		ApprovalID:     approvalID2,
		SessionID:      binding1.SessionID,
		Runtime:        rt,
		ActionDigest:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PayloadDigest:  payloadDigest([]byte(`{"decision":"deny"}`)),
		IdempotencyKey: "test.delivery.deny",
		OptionID:       "deny",
		DeliverySchema: claudeDecisionSchemaV1,
	}
	claimToken2 := "dddddddddddddddddddddddddddddddd"

	witness2 := makeWitness(WitnessPermissionDenials, "claude-sess-test", "call_delivery_test",
		"Bash", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		rt, 0)
	d.witnessProvider = witness2
	payload2 := []byte(`{"decision":"deny"}`)
	req2 := ApprovalDeliveryRequest{ClaimToken: claimToken2, Binding: binding2, Payload: payload2}
	req2.Binding.PayloadDigest = payloadDigest(payload2)
	r2 := d.Deliver(req2)
	if r2.Outcome != DeliveryAccepted {
		t.Fatalf("deny delivery failed: %s", r2.Outcome)
	}
}

func TestClaudeDelivery_WitnessBeforeConfirm(t *testing.T) {
	d, svc, _ := setupDeliveryTest(t)
	_, binding, claimToken := setupDeliveryApproval(t, svc, nil, "allow_once")

	// Witness arrives instantly (before ConfirmWrite in real time, but
	// since there's no actual hook write, it's fine — the coordinator
	// accepts the witness after decisionWritten state).
	witness := makeWitness(WitnessPostToolUse, "claude-sess-test", "call_delivery_test",
		"Bash", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		binding.Runtime, 0)
	d.witnessProvider = witness

	payload := []byte(`{"decision":"allow"}`)
	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	req.Binding.PayloadDigest = payloadDigest(payload)

	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryAccepted {
		t.Fatalf("expected DeliveryAccepted, got %s", receipt.Outcome)
	}
}

// ── cross-session witness test ──

func TestClaudeDelivery_CrossSessionWitness(t *testing.T) {
	d, svc, _ := setupDeliveryTest(t)
	_, binding, claimToken := setupDeliveryApproval(t, svc, nil, "allow_once")

	// Witness with wrong SessionID — MarkWitnessed will reject it.
	witness := makeWitness(WitnessPostToolUse, "wrong-session-id", "call_delivery_test",
		"Bash", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		binding.Runtime, 0)
	d.witnessProvider = witness

	payload := []byte(`{"decision":"allow"}`)
	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	req.Binding.PayloadDigest = payloadDigest(payload)

	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryRejected {
		t.Fatalf("expected DeliveryRejected for cross-session witness, got %s", receipt.Outcome)
	}
}

// ── helpers ──

func setupDeliveryTest(t *testing.T) (*ClaudeManagedApprovalDelivery, *ManagedClaudeService, *AuthoritativeApprovalStore) {
	t.Helper()
	store := NewApprovalStore()
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)
	t.Cleanup(func() {
		launcher.closeStream()
		svc.mu.Lock()
		for _, rt := range svc.runtimes {
			rt.terminate()
		}
		svc.mu.Unlock()
	})

	d := &ClaudeManagedApprovalDelivery{
		svc:     svc,
		timeout: 5 * time.Second,
	}
	return d, svc, store
}

// ensure atomic import
var _ = atomic.Int32{}
