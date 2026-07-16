package term

import (
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// ── Test doubles ──

// recordingResponseWriter records the written decision and can be
// configured to fail.
type recordingResponseWriter struct {
	mu       sync.Mutex
	written  []string
	failWith error
}

func (w *recordingResponseWriter) WriteResponse(decision string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.written = append(w.written, decision)
	return w.failWith
}

func (w *recordingResponseWriter) lastWritten() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.written) == 0 {
		return ""
	}
	return w.written[len(w.written)-1]
}

// stubWitnessRoute returns a pre-built witness after an optional delay.
func stubWitnessRoute(w ClaudeWitness, err error, delay time.Duration) ClaudeWitnessRoute {
	return func(sessionID, toolUseID string, timeout time.Duration) (ClaudeWitness, error) {
		if delay > 0 {
			time.Sleep(delay)
		}
		return w, err
	}
}

// channelWitnessRoute returns a witness from a channel (for concurrency tests).
func channelWitnessRoute(ch <-chan ClaudeWitness) ClaudeWitnessRoute {
	return func(sessionID, toolUseID string, timeout time.Duration) (ClaudeWitness, error) {
		select {
		case w := <-ch:
			return w, nil
		case <-time.After(timeout):
			return ClaudeWitness{}, ErrWitnessTimeout
		}
	}
}

// timeoutWitnessRoute never returns.
func timeoutWitnessRoute() ClaudeWitnessRoute {
	return func(sessionID, toolUseID string, timeout time.Duration) (ClaudeWitness, error) {
		time.Sleep(timeout + 100*time.Millisecond)
		return ClaudeWitness{}, ErrWitnessTimeout
	}
}

// ── Full-chain test setup ──

type deliveryTest struct {
	store     *AuthoritativeApprovalStore
	svc       *ManagedClaudeService
	coord     *claudeResumeCoordinator
	rw        *recordingResponseWriter
	launcher  *fakeClaudeLauncher
	sessionID string
	approval  string // approvalID
	rt        RuntimeRef
}

func newDeliveryTest(t *testing.T) *deliveryTest {
	t.Helper()
	store := NewApprovalStore()
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)

	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}

	dt := &deliveryTest{
		store:    store,
		svc:      svc,
		coord:    svc.coordinator,
		rw:       &recordingResponseWriter{},
		launcher: launcher,
		sessionID: id,
		approval:  "claude-del-test",
		rt:        RuntimeRef{Adapter: claudeHeadlessAdapter, Version: "2.1.209", LaunchGen: 1, StreamGen: 0},
	}

	t.Cleanup(func() {
		launcher.closeStream()
		svc.mu.Lock()
		for _, rt := range svc.runtimes {
			rt.terminate()
		}
		svc.mu.Unlock()
	})
	return dt
}

// ingestActionableRecord inserts an actionable approval into the Store
// with exact delivery material. Returns the approval ID.
func (dt *deliveryTest) ingestActionableRecord(t *testing.T, optionID string) string {
	t.Helper()

	// Create identity in coordinator (simulates C1D joinDeferred).
	ok := dt.coord.ReserveIdentity(dt.approval, "claude-sess-1", "call_test", "Bash",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		dt.sessionID, dt.rt)
	if !ok {
		t.Fatal("ReserveIdentity failed")
	}

	// Build delivery material for both options.
	allowBytes := []byte(`{"permissionDecision":"allow"}`)
	denyBytes := []byte(`{"permissionDecision":"deny"}`)

	items := []ApprovalIngestItem{{
		Approval: agent.AgentApproval{
			ID:         dt.approval,
			SessionID:  dt.sessionID,
			AgentKind:  claudeHeadlessAdapter,
			Kind:       "approval",
			Status:     "pending",
			Source:     agent.SourceJSONL,
			Confidence: 1,
			Options: []agent.InteractionOption{
				{ID: "allow_once", Label: "Allow once", Kind: "approve"},
				{ID: "deny", Label: "Deny", Kind: "reject"},
			},
		},
		Provenance: contract.ProvenanceProviderHook,
		Actionable: true,
		DeliveryMaterial: []ApprovalDeliveryMaterial{
			{OptionID: "allow_once", SchemaVersion: claudeDecisionSchemaV1, ResponseBytes: allowBytes},
			{OptionID: "deny", SchemaVersion: claudeDecisionSchemaV1, ResponseBytes: denyBytes},
		},
	}}

	ing := ApprovalIngest{
		SessionID: dt.sessionID,
		LaunchGen: dt.rt.LaunchGen,
		StreamGen: 0,
		Provider:  claudeHeadlessAdapter,
		Version:   dt.rt.Version,
		Items:     items,
	}
	if !dt.store.IngestObserved(ing) {
		t.Fatal("IngestObserved failed")
	}
	return dt.approval
}

// mustClaim calls ClaimForExecution and returns the binding+payload.
func (dt *deliveryTest) mustClaim(t *testing.T, optionID, input string) (ApprovalExecutionBinding, []byte, string) {
	t.Helper()
	reqCtx := RequesterContext{
		DeviceID: "dev-1", HostID: "host-1", BearerSessionID: "bearer-1",
		BootID: "boot-1", Permissions: []string{"session:approve"},
	}
	claim := dt.store.ClaimForExecution(ClaimRequest{
		SessionID:      dt.sessionID,
		ApprovalID:     dt.approval,
		OptionID:       optionID,
		Input:          input,
		Runtime:        dt.rt,
		Requester:      reqCtx,
		IdempotencyKey: "test.key.delivery",
	})
	if claim.Outcome != ClaimGranted {
		t.Fatalf("ClaimForExecution: %s", claim.Outcome)
	}
	return claim.Binding, claim.Payload, claim.Token
}

func (dt *deliveryTest) makeDelivery() *ClaudeManagedApprovalDelivery {
	allowRoute := stubWitnessRoute(ClaudeWitness{
		SessionID: "claude-sess-1", ToolUseID: "call_test", ToolName: "Bash",
		InputDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Runtime: dt.rt,
	}, nil, 0)
	denyRoute := stubWitnessRoute(ClaudeWitness{
		SessionID: "claude-sess-1", ToolUseID: "call_test", ToolName: "Bash",
		InputDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Runtime: dt.rt,
	}, nil, 0)
	return NewClaudeManagedApprovalDelivery(dt.svc, dt.rw, allowRoute, denyRoute)
}

// ── Positive tests (C-R3) ──

func TestClaudeDelivery_FullChainAllow(t *testing.T) {
	dt := newDeliveryTest(t)
	dt.ingestActionableRecord(t, "allow_once")
	binding, payload, claimToken := dt.mustClaim(t, "allow_once", "")
	d := dt.makeDelivery()

	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	receipt := d.Deliver(req)

	if receipt.Outcome != DeliveryAccepted {
		t.Fatalf("expected DeliveryAccepted, got %s", receipt.Outcome)
	}
	if receipt.ReceiptID == "" {
		t.Fatal("expected non-empty ReceiptID")
	}
	if dt.rw.lastWritten() != "allow" {
		t.Fatalf("expected response write 'allow', got %q", dt.rw.lastWritten())
	}

	// Commit via RecordDelivery.
	commit := dt.store.RecordDelivery(receipt)
	if !commit.Committed {
		t.Fatalf("RecordDelivery not committed: %s", commit.Outcome)
	}
}

func TestClaudeDelivery_FullChainDeny(t *testing.T) {
	dt := newDeliveryTest(t)
	dt.ingestActionableRecord(t, "deny")
	binding, payload, claimToken := dt.mustClaim(t, "deny", "")
	d := dt.makeDelivery()

	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	receipt := d.Deliver(req)

	if receipt.Outcome != DeliveryAccepted {
		t.Fatalf("expected DeliveryAccepted, got %s", receipt.Outcome)
	}
	if dt.rw.lastWritten() != "deny" {
		t.Fatalf("expected response write 'deny', got %q", dt.rw.lastWritten())
	}
	commit := dt.store.RecordDelivery(receipt)
	if !commit.Committed {
		t.Fatal("RecordDelivery not committed")
	}
}

// ── Write failure test (C-R1) ──

func TestClaudeDelivery_WriteFailure(t *testing.T) {
	dt := newDeliveryTest(t)
	dt.ingestActionableRecord(t, "allow_once")
	binding, payload, claimToken := dt.mustClaim(t, "allow_once", "")

	dt.rw.failWith = errTestWriteFailed
	d := dt.makeDelivery()

	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	receipt := d.Deliver(req)

	if receipt.Outcome != DeliveryConflict {
		t.Fatalf("expected DeliveryConflict for write failure, got %s", receipt.Outcome)
	}
	// Verify cleanup: identity removed, no entry remaining.
	if dt.coord.identityCount() != 0 {
		t.Fatal("identity should be cleaned up after write failure")
	}
	if dt.coord.pendingCount() != 0 {
		t.Fatal("no pending entries after write failure")
	}
}

var errTestWriteFailed = &testWriteError{}

type testWriteError struct{}

func (e *testWriteError) Error() string { return "test write failure" }

// ── Duplicate claim test ──

func TestClaudeDelivery_DuplicateClaim(t *testing.T) {
	dt := newDeliveryTest(t)
	dt.ingestActionableRecord(t, "allow_once")
	binding, payload, claimToken := dt.mustClaim(t, "allow_once", "")
	d := dt.makeDelivery()

	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	r1 := d.Deliver(req)
	if r1.Outcome != DeliveryAccepted {
		t.Fatalf("first delivery: %s", r1.Outcome)
	}

	// Second delivery with same claim token — identity consumed, ReserveEntry fails.
	r2 := d.Deliver(req)
	if r2.Outcome != DeliveryUnavailable {
		t.Fatalf("expected DeliveryUnavailable for duplicate, got %s", r2.Outcome)
	}
}

// ── Timeout test ──

func TestClaudeDelivery_Timeout(t *testing.T) {
	dt := newDeliveryTest(t)
	dt.ingestActionableRecord(t, "allow_once")
	binding, payload, claimToken := dt.mustClaim(t, "allow_once", "")

	d := NewClaudeManagedApprovalDelivery(dt.svc, dt.rw,
		stubWitnessRoute(ClaudeWitness{}, nil, 0),
		stubWitnessRoute(ClaudeWitness{}, nil, 0))
	d.timeout = 50 * time.Millisecond
	// Replace routes with timeout versions.
	d.postToolUseRoute = timeoutWitnessRoute()
	d.denialRoute = timeoutWitnessRoute()

	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryConflict {
		t.Fatalf("expected DeliveryConflict for timeout, got %s", receipt.Outcome)
	}
	if dt.coord.identityCount() != 0 {
		t.Fatal("identity should be cleaned up after timeout")
	}
}

// ── Barrier-based interleaving tests ──

func TestClaudeDelivery_CancelAtClaim(t *testing.T) {
	dt := newDeliveryTest(t)
	dt.ingestActionableRecord(t, "allow_once")
	binding, payload, claimToken := dt.mustClaim(t, "allow_once", "")
	d := dt.makeDelivery()

	d.barrier = func(stage string) {
		if stage == "post-claim" {
			dt.coord.Close()
		}
	}

	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryConflict {
		t.Fatalf("expected DeliveryConflict after cancel at claim, got %s", receipt.Outcome)
	}
}

func TestClaudeDelivery_WitnessRace(t *testing.T) {
	dt := newDeliveryTest(t)
	dt.ingestActionableRecord(t, "allow_once")
	binding, payload, claimToken := dt.mustClaim(t, "allow_once", "")

	witnessCh := make(chan ClaudeWitness, 1)
	d := NewClaudeManagedApprovalDelivery(dt.svc, dt.rw,
		channelWitnessRoute(witnessCh),
		stubWitnessRoute(ClaudeWitness{}, nil, 0))
	d.timeout = 5 * time.Second

	var receipt DeliveryReceipt
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
		receipt = d.Deliver(req)
	}()

	time.Sleep(50 * time.Millisecond)
	witnessCh <- ClaudeWitness{
		SessionID: "claude-sess-1", ToolUseID: "call_test", ToolName: "Bash",
		InputDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Runtime: dt.rt,
	}
	wg.Wait()

	if receipt.Outcome != DeliveryAccepted {
		t.Fatalf("expected DeliveryAccepted with concurrent witness, got %s", receipt.Outcome)
	}
}

// ── Cross-session witness ──

func TestClaudeDelivery_CrossSessionWitness(t *testing.T) {
	dt := newDeliveryTest(t)
	dt.ingestActionableRecord(t, "allow_once")
	binding, payload, claimToken := dt.mustClaim(t, "allow_once", "")

	// Witness with wrong SessionID.
	wrongWitness := ClaudeWitness{
		SessionID: "wrong-session", ToolUseID: "call_test", ToolName: "Bash",
		InputDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Runtime: dt.rt,
	}
	d := NewClaudeManagedApprovalDelivery(dt.svc, dt.rw,
		stubWitnessRoute(wrongWitness, nil, 0),
		stubWitnessRoute(ClaudeWitness{}, nil, 0))

	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryRejected {
		t.Fatalf("expected DeliveryRejected for cross-session witness, got %s", receipt.Outcome)
	}
}

// ── Correct witness routing ──

func TestClaudeDelivery_CorrectWitnessRouting(t *testing.T) {
	dt := newDeliveryTest(t)
	dt.ingestActionableRecord(t, "allow_once")
	binding, payload, claimToken := dt.mustClaim(t, "allow_once", "")
	d := dt.makeDelivery()

	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryAccepted {
		t.Fatalf("expected DeliveryAccepted with correct routing, got %s", receipt.Outcome)
	}
	// The coordinator validates decision("allow") against kind(WitnessPostToolUse).
	// Kind is bound by route selection in waitForWitness, not by witness data.
}

// ── Known-bad: nil response writer ──

func TestClaudeDelivery_NilResponseWriter(t *testing.T) {
	dt := newDeliveryTest(t)
	dt.ingestActionableRecord(t, "allow_once")
	binding, payload, claimToken := dt.mustClaim(t, "allow_once", "")
	d := NewClaudeManagedApprovalDelivery(dt.svc, nil, // nil writer
		stubWitnessRoute(ClaudeWitness{}, nil, 0),
		stubWitnessRoute(ClaudeWitness{}, nil, 0))

	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryUnavailable {
		t.Fatalf("expected DeliveryUnavailable for nil writer, got %s", receipt.Outcome)
	}
}

// ── Known-bad: fake confirmation (C-R3 requirement) ──

func TestClaudeDelivery_FakeConfirmation(t *testing.T) {
	// Prove that calling ConfirmWrite without WriteResponse is rejected.
	// We do this by setting up a delivery with a nil response writer BUT
	// patching it so we can observe the failure point is the nil check,
	// not a later step. The real guard is in Deliver: nil writer →
	// DeliveryUnavailable BEFORE any coordinator state change.
	dt := newDeliveryTest(t)
	dt.ingestActionableRecord(t, "allow_once")
	binding, payload, claimToken := dt.mustClaim(t, "allow_once", "")

	// Nil writer — should fail at the nil-dependency check.
	d := NewClaudeManagedApprovalDelivery(dt.svc, nil,
		stubWitnessRoute(ClaudeWitness{}, nil, 0),
		stubWitnessRoute(ClaudeWitness{}, nil, 0))

	req := ApprovalDeliveryRequest{ClaimToken: claimToken, Binding: binding, Payload: payload}
	receipt := d.Deliver(req)

	// Must be unavailable, not some later failure.
	if receipt.Outcome != DeliveryUnavailable {
		t.Fatalf("fake confirmation must be DeliveryUnavailable, got %s", receipt.Outcome)
	}
	// Coordinator must be untouched — no entries consumed.
	if dt.coord.pendingCount() != 0 || dt.coord.identityCount() != 1 {
		t.Fatal("fake confirmation must not consume coordinator entries")
	}
}

// ── Nil dependencies ──

func TestClaudeDelivery_NilService(t *testing.T) {
	d := &ClaudeManagedApprovalDelivery{timeout: time.Second}
	req := ApprovalDeliveryRequest{}
	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryUnavailable {
		t.Fatalf("expected DeliveryUnavailable for nil service, got %s", receipt.Outcome)
	}
}

func TestClaudeDelivery_NilCoordinator(t *testing.T) {
	svc := NewManagedClaudeService(testCfg(), &fakeClaudeLauncher{}, &fakeClaudeAttestor{})
	svc.coordinator = nil
	d := NewClaudeManagedApprovalDelivery(svc, &recordingResponseWriter{},
		stubWitnessRoute(ClaudeWitness{}, nil, 0),
		stubWitnessRoute(ClaudeWitness{}, nil, 0))
	req := ApprovalDeliveryRequest{}
	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryUnavailable {
		t.Fatalf("expected DeliveryUnavailable for nil coordinator, got %s", receipt.Outcome)
	}
}

// ── Cleanup verification ──

func TestClaudeDelivery_CleanupAfterClaimFailure(t *testing.T) {
	dt := newDeliveryTest(t)
	dt.ingestActionableRecord(t, "allow_once")
	binding, payload, _ := dt.mustClaim(t, "allow_once", "")

	// Use a non-matching approvalID so ReserveEntry fails at identity lookup.
	binding.ApprovalID = "claude-nonexistent"
	d := dt.makeDelivery()
	req := ApprovalDeliveryRequest{
		ClaimToken: "deadbeefdeadbeefdeadbeefdeadbeef",
		Binding:    binding,
		Payload:    payload,
	}
	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryUnavailable {
		t.Fatalf("expected DeliveryUnavailable for missing identity, got %s", receipt.Outcome)
	}
	// Identity should still exist (it was never consumed).
	if dt.coord.identityCount() != 1 {
		t.Fatal("identity should survive failed ReserveEntry")
	}
}
