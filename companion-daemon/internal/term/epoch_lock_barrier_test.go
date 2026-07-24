package term

import (
	"bytes"
	"errors"
	"testing"

	"devremote/companion-daemon/internal/devicetrust"
)

type barrierMutationAuthorizer struct {
	current  *int64
	calls    *int
	revokeOn int
}

func (a barrierMutationAuthorizer) AuthorizeCommit(_ string, expected uint64, _ devicetrust.MutationIntent) error {
	(*a.calls)++
	if a.revokeOn > 0 && *a.calls == a.revokeOn {
		*a.current = 1
	}
	if uint64(*a.current) != expected {
		return errors.New("stale epoch")
	}
	return nil
}

// TestEpochLock_ApprovalClaimBarrier proves that a successful handler-style
// preflight does not authorize a claim after revoke: the second check runs
// inside AuthoritativeApprovalStore.mu and rejects before the state transition.
func TestEpochLock_ApprovalClaimBarrier(t *testing.T) {
	sid := codexAppServerAdapter + ":epoch-claim"
	currentEpoch := int64(0)
	principal := &devicetrust.Principal{DeviceID: "epoch-device", DeviceEpoch: 0}
	calls := 0
	authorizer := barrierMutationAuthorizer{current: &currentEpoch, calls: &calls}
	store, err := NewApprovalStore(authorizer)
	if err != nil {
		t.Fatal(err)
	}
	ingestActionable(t, store, sid, "codexas-1-7", "7")
	if err := authorizer.AuthorizeCommit(principal.DeviceID, 0, devicetrust.IntentApprovalClaim); err != nil { // handler preflight
		t.Fatalf("preflight: %v", err)
	}
	currentEpoch = 1 // revoke between preflight and the store lock
	claim := store.ClaimForExecution(ClaimRequest{
		SessionID: sid, ApprovalID: "codexas-1-7", OptionID: "allow_once",
		Runtime: boundManagedRT(), Requester: completeRequester(), IdempotencyKey: "k1",
	})
	if claim.Outcome != ClaimStaleEpoch {
		t.Fatalf("claim outcome = %s, want stale_epoch", claim.Outcome)
	}
	if snap, ok := store.LookupRecord(sid, "codexas-1-7"); !ok || snap.State != ApprovalPending {
		t.Fatalf("stale claim mutated approval: ok=%v state=%s", ok, snap.State)
	}
}

// TestEpochLock_ApprovalCommitBarrier proves that RecordDelivery performs its
// epoch check while holding the store lock and refuses the accepted commit.
func TestEpochLock_ApprovalCommitBarrier(t *testing.T) {
	sid := codexAppServerAdapter + ":epoch-commit"
	currentEpoch := int64(0)
	principal := &devicetrust.Principal{DeviceID: "epoch-device", DeviceEpoch: 0}
	calls := 0
	authorizer := barrierMutationAuthorizer{current: &currentEpoch, calls: &calls}
	store, err := NewApprovalStore(authorizer)
	if err != nil {
		t.Fatal(err)
	}
	ingestActionable(t, store, sid, "codexas-1-7", "7")
	claim := mustClaim(t, store, sid, "codexas-1-7", "allow_once", "k1")
	if err := authorizer.AuthorizeCommit(principal.DeviceID, 0, devicetrust.IntentApprovalCommit); err != nil { // handler preflight
		t.Fatalf("preflight: %v", err)
	}
	currentEpoch = 1 // revoke between preflight and RecordDelivery lock
	commit := store.RecordDelivery(DeliveryReceipt{
		Outcome: DeliveryAccepted, ClaimToken: claim.Token, Binding: claim.Binding,
		ReceiptID: "receipt-1", DeliveredPayloadDigest: claim.Binding.PayloadDigest,
	})
	if commit.Committed || commit.Outcome != DeliveryStaleRuntime {
		t.Fatalf("stale commit = %+v, want non-committed stale runtime", commit)
	}
	if snap, ok := store.LookupRecord(sid, "codexas-1-7"); !ok || snap.State != ApprovalExecuting {
		t.Fatalf("stale commit changed approval: ok=%v state=%s", ok, snap.State)
	}
}

// TestEpochLock_ApprovalDeliveryBarrier proves that the delivery acceptance
// gate rechecks authorization while holding its acceptance lock, before queue
// admission/provider delivery.
func TestEpochLock_ApprovalDeliveryBarrier(t *testing.T) {
	store := testApprovalStore()
	sid := codexAppServerAdapter + ":epoch-delivery"
	ingestActionable(t, store, sid, "codexas-1-7", "7")
	claim := mustClaim(t, store, sid, "codexas-1-7", "allow_once", "k1")

	currentEpoch := int64(0)
	principal := &devicetrust.Principal{DeviceID: "epoch-device", DeviceEpoch: 0}
	calls := 0
	authorizer := barrierMutationAuthorizer{current: &currentEpoch, calls: &calls}
	gate, err := NewRuntimeDeliveryGate(authorizer)
	if err != nil {
		t.Fatal(err)
	}
	handle, ok := gate.Activate(sid, boundManagedRT(), 1)
	if !ok || handle == "" {
		t.Fatal("delivery gate activation failed")
	}
	if err := authorizer.AuthorizeCommit(principal.DeviceID, 0, devicetrust.IntentApprovalDeliver); err != nil { // handler preflight
		t.Fatalf("preflight: %v", err)
	}
	currentEpoch = 1
	receipt := NewGatedApprovalDelivery(gate).Deliver(ApprovalDeliveryRequest{
		ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload,
		DeviceID: principal.DeviceID, DeviceEpoch: 0,
	})
	if receipt.Outcome != DeliveryUnavailable {
		t.Fatalf("stale delivery outcome = %s, want unavailable", receipt.Outcome)
	}
	if queued := gate.Drain(handle); len(queued) != 0 {
		t.Fatalf("stale delivery queued %d item(s)", len(queued))
	}
}

// TestEpochLock_PromptBarrier proves that the managed prompt path rechecks
// inside the runtime turn lock before provider delivery. The fake provider
// must observe zero turn/start writes after the barrier revoke.
func TestEpochLock_PromptBarrier(t *testing.T) {
	provider := &interactiveAppServer{threadID: "thread-epoch-prompt"}
	currentEpoch := int64(0)
	checks := 0
	authorizer := barrierMutationAuthorizer{current: &currentEpoch, calls: &checks, revokeOn: 2}
	managed, _ := newInteractiveServiceWithAuthorizer(t, provider, authorizer)
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	id := resp["id"]
	t.Cleanup(func() { _ = managed.Kill(id, 1, "", 0) })

	principal := &devicetrust.Principal{DeviceID: "epoch-device", DeviceEpoch: 0}
	// Session creation is itself authorized; reset the barrier seam so the
	// preflight and prompt-internal checks are the two calls under test.
	currentEpoch = 0
	checks = 0
	if err := authorizer.AuthorizeCommit(principal.DeviceID, 0, devicetrust.IntentPrompt); err != nil { // handler preflight
		t.Fatalf("preflight: %v", err)
	}
	if err := managed.SubmitPrompt(id, 1, "blocked", principal.DeviceID, 0); err == nil {
		t.Fatal("stale prompt was accepted")
	}
	if checks < 2 {
		t.Fatalf("lock-internal epoch check calls = %d, want at least 2", checks)
	}
	if got := provider.turnCount(); got != 0 {
		t.Fatalf("provider turn/start writes = %d, want 0", got)
	}
}

// TestEpochLock_InputBarrier proves that the WebSocket input transport invokes
// its epoch callback under the transport lock before writing any bytes.
func TestEpochLock_InputBarrier(t *testing.T) {
	var out bytes.Buffer
	currentEpoch := int64(0)
	transport := newTerminalTransport("controlled_pty:epoch-input", 1, &out, nil, nil, barrierMutationAuthorizer{current: &currentEpoch, calls: new(int)})
	principal := &devicetrust.Principal{DeviceID: "epoch-device", DeviceEpoch: 0}
	currentEpoch = 1
	written, err := transport.WriteInput([]byte("blocked\n"), principal.DeviceID, uint64(principal.DeviceEpoch))
	if written != 0 || err == nil {
		t.Fatalf("stale input write = (%d, %v), want rejection", written, err)
	}
	if out.Len() != 0 {
		t.Fatalf("stale input reached writer: %q", out.String())
	}
}
