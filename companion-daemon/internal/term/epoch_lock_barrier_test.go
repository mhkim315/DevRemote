package term

import (
	"bytes"
	"testing"

	"devremote/companion-daemon/internal/devicetrust"
)

// TestEpochLock_ApprovalClaimBarrier proves that a successful handler-style
// preflight does not authorize a claim after revoke: the second check runs
// inside AuthoritativeApprovalStore.mu and rejects before the state transition.
func TestEpochLock_ApprovalClaimBarrier(t *testing.T) {
	store := NewApprovalStore()
	sid := codexAppServerAdapter + ":epoch-claim"
	ingestActionable(t, store, sid, "codexas-1-7", "7")

	currentEpoch := int64(0)
	principal := &devicetrust.Principal{DeviceID: "epoch-device", DeviceEpoch: 0}
	getAuth := func(string) devicetrust.AuthorizationState {
		return devicetrust.AuthorizationState{Active: true, Epoch: uint64(currentEpoch)}
	}
	check := func() error {
		return devicetrust.RecheckEpoch(getAuth, principal)
	}
	if err := check(); err != nil { // handler preflight
		t.Fatalf("preflight: %v", err)
	}
	currentEpoch = 1 // revoke between preflight and the store lock
	claim := store.ClaimForExecution(ClaimRequest{
		SessionID: sid, ApprovalID: "codexas-1-7", OptionID: "allow_once",
		Runtime: boundManagedRT(), Requester: completeRequester(), IdempotencyKey: "k1",
		EpochRecheck: check,
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
	store := NewApprovalStore()
	sid := codexAppServerAdapter + ":epoch-commit"
	ingestActionable(t, store, sid, "codexas-1-7", "7")
	claim := mustClaim(t, store, sid, "codexas-1-7", "allow_once", "k1")

	currentEpoch := int64(0)
	principal := &devicetrust.Principal{DeviceID: "epoch-device", DeviceEpoch: 0}
	getAuth := func(string) devicetrust.AuthorizationState {
		return devicetrust.AuthorizationState{Active: true, Epoch: uint64(currentEpoch)}
	}
	check := func() error {
		return devicetrust.RecheckEpoch(getAuth, principal)
	}
	if err := check(); err != nil { // handler preflight
		t.Fatalf("preflight: %v", err)
	}
	currentEpoch = 1 // revoke between preflight and RecordDelivery lock
	commit := store.RecordDelivery(DeliveryReceipt{
		Outcome: DeliveryAccepted, ClaimToken: claim.Token, Binding: claim.Binding,
		ReceiptID: "receipt-1", DeliveredPayloadDigest: claim.Binding.PayloadDigest,
	}, check)
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
	store := NewApprovalStore()
	sid := codexAppServerAdapter + ":epoch-delivery"
	ingestActionable(t, store, sid, "codexas-1-7", "7")
	claim := mustClaim(t, store, sid, "codexas-1-7", "allow_once", "k1")

	gate := NewRuntimeDeliveryGate()
	handle, ok := gate.Activate(sid, boundManagedRT(), 1)
	if !ok || handle == "" {
		t.Fatal("delivery gate activation failed")
	}
	currentEpoch := int64(0)
	principal := &devicetrust.Principal{DeviceID: "epoch-device", DeviceEpoch: 0}
	getAuth := func(string) devicetrust.AuthorizationState {
		return devicetrust.AuthorizationState{Active: true, Epoch: uint64(currentEpoch)}
	}
	check := func() error {
		return devicetrust.RecheckEpoch(getAuth, principal)
	}
	if err := check(); err != nil { // handler preflight
		t.Fatalf("preflight: %v", err)
	}
	currentEpoch = 1
	receipt := NewGatedApprovalDelivery(gate).Deliver(ApprovalDeliveryRequest{
		ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload,
		EpochRecheck: check,
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
	managed, _ := newInteractiveService(t, provider)
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	id := resp["id"]
	t.Cleanup(func() { _ = managed.Kill(id, 1, "", 0) })

	currentEpoch := int64(0)
	principal := &devicetrust.Principal{DeviceID: "epoch-device", DeviceEpoch: 0}
	getAuth := func(string) devicetrust.AuthorizationState {
		return devicetrust.AuthorizationState{Active: true, Epoch: uint64(currentEpoch)}
	}
	checks := 0
	check := func() error {
		checks++
		if checks == 2 { // revoke after the handler preflight, before lock check
			currentEpoch = 1
		}
		return devicetrust.RecheckEpoch(getAuth, principal)
	}
	if err := check(); err != nil { // handler preflight
		t.Fatalf("preflight: %v", err)
	}
	if err := managed.SubmitPrompt(id, 1, "blocked", check); err == nil {
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
	transport := newTerminalTransport("controlled_pty:epoch-input", 1, &out, nil, nil)
	currentEpoch := int64(0)
	principal := &devicetrust.Principal{DeviceID: "epoch-device", DeviceEpoch: 0}
	getAuth := func(string) devicetrust.AuthorizationState {
		return devicetrust.AuthorizationState{Active: true, Epoch: uint64(currentEpoch)}
	}
	check := func() error {
		return devicetrust.RecheckEpoch(getAuth, principal)
	}
	if err := check(); err != nil { // handler preflight
		t.Fatalf("preflight: %v", err)
	}
	currentEpoch = 1
	written, err := transport.WriteInput([]byte("blocked\n"), check)
	if written != 0 || err == nil {
		t.Fatalf("stale input write = (%d, %v), want rejection", written, err)
	}
	if out.Len() != 0 {
		t.Fatalf("stale input reached writer: %q", out.String())
	}
}
