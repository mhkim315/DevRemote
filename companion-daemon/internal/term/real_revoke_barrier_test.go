package term

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

// realRevokeBarrierAuthorizer pauses the decisive authorization call. The
// test then revokes the real registry device before allowing AuthorizeCommit to
// continue, proving that the sink observes the registry's revoked state rather
// than a test-only epoch variable.
type realRevokeBarrierAuthorizer struct {
	reg      *devicetrust.DeviceRegistry
	deviceID string

	mu      sync.Mutex
	armed   bool
	reached chan struct{}
	release chan struct{}
}

func (a *realRevokeBarrierAuthorizer) AuthorizeCommit(deviceID string, epoch uint64, intent devicetrust.MutationIntent) error {
	a.mu.Lock()
	if a.armed && deviceID == a.deviceID {
		a.armed = false
		reached, release := a.reached, a.release
		close(reached)
		a.mu.Unlock()
		<-release
	} else {
		a.mu.Unlock()
	}
	return a.reg.AuthorizeCommit(deviceID, epoch, intent)
}

func (a *realRevokeBarrierAuthorizer) arm() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reached = make(chan struct{})
	a.release = make(chan struct{})
	a.armed = true
}

func (a *realRevokeBarrierAuthorizer) revokeAndRelease(t *testing.T, operation func() error) error {
	t.Helper()
	a.arm()
	done := make(chan error, 1)
	go func() { done <- operation() }()
	select {
	case <-a.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("mutation did not reach the revoke barrier")
	}
	if err := a.reg.Revoke(a.deviceID); err != nil {
		t.Fatalf("real DeviceRegistry.Revoke: %v", err)
	}
	a.mu.Lock()
	close(a.release)
	a.mu.Unlock()
	return <-done
}

func newRealRevokeBarrier(t *testing.T) (*realRevokeBarrierAuthorizer, string, uint64) {
	t.Helper()
	reg, err := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: t.TempDir() + "/devices.json"})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	device, err := reg.Add(der, "barrier-test")
	if err != nil {
		t.Fatal(err)
	}
	return &realRevokeBarrierAuthorizer{reg: reg, deviceID: device.DeviceID}, device.DeviceID, uint64(device.Epoch)
}

func assertRevokeWins(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("mutation succeeded after real device revoke")
	}
}

func TestRealRevokeBarrier_Lifecycle(t *testing.T) {
	auth, deviceID, epoch := newRealRevokeBarrier(t)
	launcher := newFakeAdapter()
	owned, err := NewOwnedPTYRuntime(auth, launcher, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := owned.Create(context.Background(), SpawnConfig{Name: "real-lifecycle"}, "", "real", deviceID, epoch)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		owned.mu.Lock()
		entry := owned.entries[id]
		owned.mu.Unlock()
		if entry != nil && entry.handle != nil {
			_ = entry.handle.Kill()
		}
	})
	if err := auth.AuthorizeCommit(deviceID, epoch, devicetrust.IntentSessionStop); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	err = auth.revokeAndRelease(t, func() error {
		_, err := owned.Stop(context.Background(), id, deviceID, epoch)
		return err
	})
	assertRevokeWins(t, err)
	if _, _, terminated := launcher.snapshot(); len(terminated) != 0 {
		t.Fatalf("lifecycle side effect after revoke: terminated=%v", terminated)
	}
}

func TestRealRevokeBarrier_Prompt(t *testing.T) {
	auth, deviceID, epoch := newRealRevokeBarrier(t)
	provider := &interactiveAppServer{threadID: "real-revoke-prompt"}
	managed, launcher := newInteractiveServiceWithAuthorizer(t, provider, auth)
	id, err := managed.CreateAttached("", deviceID, epoch)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		for _, proc := range launcher.procs {
			_ = proc.Kill()
		}
	})
	if err := auth.AuthorizeCommit(deviceID, epoch, devicetrust.IntentPrompt); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	err = auth.revokeAndRelease(t, func() error {
		return managed.SubmitPrompt(id, 1, "blocked", deviceID, epoch)
	})
	assertRevokeWins(t, err)
	if got := provider.turnCount(); got != 0 {
		t.Fatalf("provider turn writes after revoke: %d", got)
	}
}

func realRequester(deviceID string, epoch uint64) RequesterContext {
	return RequesterContext{DeviceID: deviceID, DeviceEpoch: epoch, HostID: "host-real", BearerSessionID: "bearer-real", BootID: "boot-real", Permissions: []string{devicetrust.PermTerminalInput}}
}

func realApprovalClaim(t *testing.T, auth *realRevokeBarrierAuthorizer, deviceID string, epoch uint64) (*AuthoritativeApprovalStore, ClaimRequest) {
	t.Helper()
	store, err := NewApprovalStore(auth)
	if err != nil {
		t.Fatal(err)
	}
	sid := codexAppServerAdapter + ":real-revoke-approval"
	ingestActionable(t, store, sid, "approval-real", "real-token")
	return store, ClaimRequest{SessionID: sid, ApprovalID: "approval-real", OptionID: "allow_once", Runtime: boundManagedRT(), Requester: realRequester(deviceID, epoch), IdempotencyKey: "real-claim"}
}

func TestRealRevokeBarrier_ApprovalClaim(t *testing.T) {
	auth, deviceID, epoch := newRealRevokeBarrier(t)
	store, req := realApprovalClaim(t, auth, deviceID, epoch)
	if err := auth.AuthorizeCommit(deviceID, epoch, devicetrust.IntentApprovalClaim); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	result := make(chan ClaimResult, 1)
	auth.arm()
	go func() { result <- store.ClaimForExecution(req) }()
	select {
	case <-auth.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("approval claim did not reach revoke barrier")
	}
	if err := auth.reg.Revoke(deviceID); err != nil {
		t.Fatal(err)
	}
	auth.mu.Lock()
	close(auth.release)
	auth.mu.Unlock()
	if got := <-result; got.Outcome != ClaimStaleEpoch {
		t.Fatalf("claim outcome=%s, want stale_epoch", got.Outcome)
	}
	if snap, ok := store.LookupRecord(req.SessionID, req.ApprovalID); !ok || snap.State != ApprovalPending {
		t.Fatalf("claim side effect after revoke: ok=%v state=%s", ok, snap.State)
	}
}

func TestRealRevokeBarrier_ApprovalDelivery(t *testing.T) {
	auth, deviceID, epoch := newRealRevokeBarrier(t)
	store, req := realApprovalClaim(t, auth, deviceID, epoch)
	claim := store.ClaimForExecution(req)
	if claim.Outcome != ClaimGranted {
		t.Fatalf("claim setup: %s", claim.Outcome)
	}
	gate, err := NewRuntimeDeliveryGate(auth)
	if err != nil {
		t.Fatal(err)
	}
	handle, ok := gate.Activate(req.SessionID, boundManagedRT(), 1)
	if !ok {
		t.Fatal("delivery gate activation failed")
	}
	if err := auth.AuthorizeCommit(deviceID, epoch, devicetrust.IntentApprovalDeliver); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	result := make(chan DeliveryReceipt, 1)
	auth.arm()
	go func() {
		result <- NewGatedApprovalDelivery(gate).Deliver(ApprovalDeliveryRequest{
			ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload,
			DeviceID: deviceID, DeviceEpoch: epoch,
		})
	}()
	select {
	case <-auth.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("approval delivery did not reach revoke barrier")
	}
	if err := auth.reg.Revoke(deviceID); err != nil {
		t.Fatal(err)
	}
	auth.mu.Lock()
	close(auth.release)
	auth.mu.Unlock()
	if got := <-result; got.Outcome != DeliveryUnavailable {
		t.Fatalf("delivery outcome=%s, want unavailable", got.Outcome)
	}
	if queued := gate.Drain(handle); len(queued) != 0 {
		t.Fatalf("delivery side effect after revoke: queued=%d", len(queued))
	}
}

func TestRealRevokeBarrier_ApprovalCommit(t *testing.T) {
	auth, deviceID, epoch := newRealRevokeBarrier(t)
	store, req := realApprovalClaim(t, auth, deviceID, epoch)
	claim := store.ClaimForExecution(req)
	if claim.Outcome != ClaimGranted {
		t.Fatalf("claim setup: %s", claim.Outcome)
	}
	if err := auth.AuthorizeCommit(deviceID, epoch, devicetrust.IntentApprovalCommit); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	result := make(chan DeliveryCommit, 1)
	auth.arm()
	go func() {
		result <- store.RecordDelivery(DeliveryReceipt{Outcome: DeliveryAccepted, ClaimToken: claim.Token, Binding: claim.Binding, ReceiptID: "real-receipt", DeliveredPayloadDigest: claim.Binding.PayloadDigest})
	}()
	select {
	case <-auth.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("approval commit did not reach revoke barrier")
	}
	if err := auth.reg.Revoke(deviceID); err != nil {
		t.Fatal(err)
	}
	auth.mu.Lock()
	close(auth.release)
	auth.mu.Unlock()
	got := <-result
	if got.Committed || got.Outcome != DeliveryStaleRuntime {
		t.Fatalf("commit=%+v, want stale non-commit", got)
	}
	if snap, ok := store.LookupRecord(req.SessionID, req.ApprovalID); !ok || snap.State != ApprovalExecuting {
		t.Fatalf("commit side effect after revoke: ok=%v state=%s", ok, snap.State)
	}
}

func TestRealRevokeBarrier_Command(t *testing.T) {
	auth, deviceID, epoch := newRealRevokeBarrier(t)
	broker := NewCommandBroker(auth)
	if err := auth.AuthorizeCommit(deviceID, epoch, devicetrust.IntentCmd); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	err := auth.revokeAndRelease(t, func() error { return broker.PutAuthorized("real-command", []byte("run"), deviceID, epoch) })
	assertRevokeWins(t, err)
	if got := broker.Take("real-command"); got != nil {
		t.Fatalf("command side effect after revoke: %q", got)
	}
}

func TestRealRevokeBarrier_Input(t *testing.T) {
	auth, deviceID, epoch := newRealRevokeBarrier(t)
	var out bytes.Buffer
	transport := newTerminalTransport("controlled_pty:real-input", 1, &out, nil, nil, auth)
	if err := auth.AuthorizeCommit(deviceID, epoch, devicetrust.IntentWSInput); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	var written int
	var err error
	auth.arm()
	done := make(chan struct{})
	go func() {
		written, err = transport.WriteInput([]byte("blocked"), deviceID, epoch)
		close(done)
	}()
	select {
	case <-auth.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("input did not reach revoke barrier")
	}
	if revokeErr := auth.reg.Revoke(deviceID); revokeErr != nil {
		t.Fatal(revokeErr)
	}
	auth.mu.Lock()
	close(auth.release)
	auth.mu.Unlock()
	<-done
	if written != 0 || err == nil || out.Len() != 0 {
		t.Fatalf("input after revoke=(%d,%v) bytes=%d", written, err, out.Len())
	}
}

func TestRealRevokeBarrier_Ticket(t *testing.T) {
	auth, deviceID, epoch := newRealRevokeBarrier(t)
	store := devicetrust.NewWSTicketStore(auth)
	principal := &devicetrust.Principal{DeviceID: deviceID, DeviceEpoch: int64(epoch), HostID: "host-real", BearerSessionID: "bearer-real", BearerExpires: time.Now().Add(time.Minute)}
	if err := auth.AuthorizeCommit(deviceID, epoch, devicetrust.IntentReconnect); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	var issueErr error
	auth.arm()
	done := make(chan struct{})
	go func() {
		_, _, issueErr = store.Issue(principal, "host-real", "controlled_pty:ticket")
		close(done)
	}()
	select {
	case <-auth.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("ticket issue did not reach revoke barrier")
	}
	if err := auth.reg.Revoke(deviceID); err != nil {
		t.Fatal(err)
	}
	auth.mu.Lock()
	close(auth.release)
	auth.mu.Unlock()
	<-done
	assertRevokeWins(t, issueErr)
}

type barrierCloser struct{}

func (barrierCloser) Close() error { return nil }

func TestRealRevokeBarrier_Connection(t *testing.T) {
	auth, deviceID, epoch := newRealRevokeBarrier(t)
	registry := devicetrust.NewAuthenticatedConnRegistry(auth)
	if err := auth.AuthorizeCommit(deviceID, epoch, devicetrust.IntentReconnect); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	err := auth.revokeAndRelease(t, func() error { return registry.Register(deviceID, epoch, barrierCloser{}) })
	assertRevokeWins(t, err)
	if got := registry.Count(deviceID); got != 0 {
		t.Fatalf("connection side effect after revoke: %d connection(s)", got)
	}
}
