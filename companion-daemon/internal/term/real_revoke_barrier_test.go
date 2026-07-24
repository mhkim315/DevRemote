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
	"devremote/companion-daemon/internal/notification"
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

// postAuthRevokeBarrierAuthorizer is the complementary barrier: it delegates
// to the real registry first, then pauses only after authorization succeeded.
// Revoke therefore wins the gap between authorization return and sink commit.
type postAuthRevokeBarrierAuthorizer struct {
	reg      *devicetrust.DeviceRegistry
	deviceID string

	mu      sync.Mutex
	armed   bool
	reached chan struct{}
	release chan struct{}
}

func (a *postAuthRevokeBarrierAuthorizer) AuthorizeCommit(deviceID string, epoch uint64, intent devicetrust.MutationIntent) error {
	if err := a.reg.AuthorizeCommit(deviceID, epoch, intent); err != nil {
		return err
	}
	a.mu.Lock()
	if a.armed && deviceID == a.deviceID {
		a.armed = false
		reached, release := a.reached, a.release
		close(reached)
		a.mu.Unlock()
		<-release
		return nil
	}
	a.mu.Unlock()
	return nil
}

func (a *postAuthRevokeBarrierAuthorizer) arm() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reached = make(chan struct{})
	a.release = make(chan struct{})
	a.armed = true
}

func (a *postAuthRevokeBarrierAuthorizer) revokeAndRelease(t *testing.T, operation func() error) error {
	t.Helper()
	a.arm()
	done := make(chan error, 1)
	go func() { done <- operation() }()
	select {
	case <-a.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("mutation did not reach post-authorization barrier")
	}
	if err := a.reg.Revoke(a.deviceID); err != nil {
		t.Fatalf("real DeviceRegistry.Revoke: %v", err)
	}
	a.mu.Lock()
	close(a.release)
	a.mu.Unlock()
	return <-done
}

func (a *postAuthRevokeBarrierAuthorizer) releaseAfterAuthorization(t *testing.T, operation func() error) error {
	t.Helper()
	a.arm()
	done := make(chan error, 1)
	go func() { done <- operation() }()
	select {
	case <-a.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("mutation did not reach post-authorization barrier")
	}
	a.mu.Lock()
	close(a.release)
	a.mu.Unlock()
	return <-done
}

func newPostAuthBarrier(t *testing.T) (*postAuthRevokeBarrierAuthorizer, string, uint64) {
	t.Helper()
	pre, deviceID, epoch := newRealRevokeBarrier(t)
	return &postAuthRevokeBarrierAuthorizer{reg: pre.reg, deviceID: deviceID}, deviceID, epoch
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

func realApprovalClaim(t *testing.T, auth devicetrust.MutationAuthorizer, deviceID string, epoch uint64) (*AuthoritativeApprovalStore, ClaimRequest) {
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

type barrierResizer struct{ calls int }

func (r *barrierResizer) Resize(int, int) error {
	r.calls++
	return nil
}

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

func TestPostAuthorizationRevokeBarrier_LifecycleAndMutationWins(t *testing.T) {
	for _, tc := range []struct {
		name string
		win  bool
	}{
		{name: "revoke-wins"},
		{name: "mutation-wins", win: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth, deviceID, epoch := newPostAuthBarrier(t)
			launcher := newFakeAdapter()
			owned, err := NewOwnedPTYRuntime(auth, launcher, nil)
			if err != nil {
				t.Fatal(err)
			}
			id, err := owned.Create(context.Background(), SpawnConfig{Name: "post-life-" + tc.name}, "", "post", deviceID, epoch)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				owned.mu.Lock()
				entry := owned.entries[id]
				owned.mu.Unlock()
				if entry != nil && entry.handle != nil {
					_ = entry.handle.Kill()
				}
			})
			barrierRun := auth.revokeAndRelease
			if tc.win {
				barrierRun = auth.releaseAfterAuthorization
			}
			err = barrierRun(t, func() error {
				_, err := owned.Kill(context.Background(), id, deviceID, epoch)
				return err
			})
			if tc.win {
				if err != nil {
					t.Fatalf("mutation-wins kill: %v", err)
				}
				if err := auth.reg.Revoke(deviceID); err != nil {
					t.Fatalf("revoke after committed kill: %v", err)
				}
				return
			}
			assertRevokeWins(t, err)
			if _, _, terminated := launcher.snapshot(); len(terminated) != 0 {
				t.Fatalf("lifecycle side effect after revoke: terminated=%v", terminated)
			}
		})
	}
}

func TestPostAuthorizationRevokeBarrier_PromptAndMutationWins(t *testing.T) {
	for _, tc := range []struct {
		name string
		win  bool
	}{
		{name: "revoke-wins"},
		{name: "mutation-wins", win: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth, deviceID, epoch := newPostAuthBarrier(t)
			provider := &interactiveAppServer{threadID: "post-prompt-" + tc.name}
			managed, launcher := newInteractiveServiceWithAuthorizer(t, provider, auth)
			id, err := managed.CreateAttached("", deviceID, epoch)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				for _, proc := range launcher.procs {
					_ = proc.Kill()
				}
			})
			barrierRun := auth.revokeAndRelease
			if tc.win {
				barrierRun = auth.releaseAfterAuthorization
			}
			err = barrierRun(t, func() error {
				return managed.SubmitPrompt(id, 1, "post-barrier", deviceID, epoch)
			})
			if tc.win {
				if err != nil {
					t.Fatalf("mutation-wins prompt: %v", err)
				}
				deadline := time.Now().Add(time.Second)
				for provider.turnCount() == 0 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if got := provider.turnCount(); got == 0 {
					t.Fatal("committed prompt produced no provider turn")
				}
				if err := auth.reg.Revoke(deviceID); err != nil {
					t.Fatalf("revoke after committed prompt: %v", err)
				}
				return
			}
			assertRevokeWins(t, err)
			if got := provider.turnCount(); got != 0 {
				t.Fatalf("provider turn after revoke: %d", got)
			}
		})
	}
}

func TestPostAuthorizationRevokeBarrier_ApprovalAndMutationWins(t *testing.T) {
	for _, tc := range []struct {
		name string
		win  bool
	}{
		{name: "revoke-wins"},
		{name: "mutation-wins", win: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth, deviceID, epoch := newPostAuthBarrier(t)
			store, req := realApprovalClaim(t, auth, deviceID, epoch)
			result := make(chan ClaimResult, 1)
			barrierRun := auth.revokeAndRelease
			if tc.win {
				barrierRun = auth.releaseAfterAuthorization
			}
			err := barrierRun(t, func() error {
				result <- store.ClaimForExecution(req)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			claim := <-result
			if tc.win {
				if claim.Outcome != ClaimGranted {
					t.Fatalf("mutation-wins claim=%s", claim.Outcome)
				}
				if err := auth.reg.Revoke(deviceID); err != nil {
					t.Fatalf("revoke after committed claim: %v", err)
				}
				return
			}
			if claim.Outcome != ClaimStaleEpoch {
				t.Fatalf("revoke-wins claim=%s, want stale_epoch", claim.Outcome)
			}
			if snap, ok := store.LookupRecord(req.SessionID, req.ApprovalID); !ok || snap.State != ApprovalPending {
				t.Fatalf("approval side effect after revoke: ok=%v state=%s", ok, snap.State)
			}
		})
	}
}

func TestPostAuthorizationRevokeBarrier_CommandAndMutationWins(t *testing.T) {
	for _, tc := range []struct {
		name string
		win  bool
	}{
		{name: "revoke-wins"},
		{name: "mutation-wins", win: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth, deviceID, epoch := newPostAuthBarrier(t)
			broker := NewCommandBroker(auth)
			var err error
			barrierRun := auth.revokeAndRelease
			if tc.win {
				barrierRun = auth.releaseAfterAuthorization
			}
			err = barrierRun(t, func() error {
				return broker.PutAuthorized("post-command", []byte("run"), deviceID, epoch)
			})
			if tc.win {
				if err != nil {
					t.Fatal(err)
				}
				if got := broker.Take("post-command"); string(got) != "run" {
					t.Fatalf("committed command=%q", got)
				}
				if err := auth.reg.Revoke(deviceID); err != nil {
					t.Fatalf("revoke after committed command: %v", err)
				}
				return
			}
			assertRevokeWins(t, err)
			if got := broker.Take("post-command"); got != nil {
				t.Fatalf("command side effect after revoke: %q", got)
			}
		})
	}
}

func TestPostAuthorizationRevokeBarrier_InputAndMutationWins(t *testing.T) {
	for _, tc := range []struct {
		name string
		win  bool
	}{
		{name: "revoke-wins"},
		{name: "mutation-wins", win: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth, deviceID, epoch := newPostAuthBarrier(t)
			var out bytes.Buffer
			transport := newTerminalTransport("controlled_pty:post-input", 1, &out, nil, nil, auth)
			var written int
			var err error
			barrierRun := auth.revokeAndRelease
			if tc.win {
				barrierRun = auth.releaseAfterAuthorization
			}
			err = barrierRun(t, func() error {
				written, err = transport.WriteInput([]byte("post-input"), deviceID, epoch)
				return err
			})
			if tc.win {
				if err != nil || written != len("post-input") || out.String() != "post-input" {
					t.Fatalf("mutation-wins input=(%d,%v,%q)", written, err, out.String())
				}
				if err := auth.reg.Revoke(deviceID); err != nil {
					t.Fatalf("revoke after committed input: %v", err)
				}
				return
			}
			assertRevokeWins(t, err)
			if written != 0 || out.Len() != 0 {
				t.Fatalf("input side effect after revoke=(%d,%q)", written, out.String())
			}
		})
	}
}

func TestPostAuthorizationRevokeBarrier_TicketAndMutationWins(t *testing.T) {
	for _, tc := range []struct {
		name string
		win  bool
	}{
		{name: "revoke-wins"},
		{name: "mutation-wins", win: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth, deviceID, epoch := newPostAuthBarrier(t)
			store := devicetrust.NewWSTicketStore(auth)
			principal := &devicetrust.Principal{DeviceID: deviceID, DeviceEpoch: int64(epoch), HostID: "post-host", BearerSessionID: "post-bearer", BearerExpires: time.Now().Add(time.Minute)}
			var raw string
			var issueErr error
			barrierRun := auth.revokeAndRelease
			if tc.win {
				barrierRun = auth.releaseAfterAuthorization
			}
			issueErr = barrierRun(t, func() error {
				raw, _, issueErr = store.Issue(principal, "post-host", "controlled_pty:post-ticket")
				return issueErr
			})
			if tc.win {
				if issueErr != nil || raw == "" || store.Count() != 1 {
					t.Fatalf("mutation-wins ticket=(%q,%v,count=%d)", raw, issueErr, store.Count())
				}
				if err := auth.reg.Revoke(deviceID); err != nil {
					t.Fatalf("revoke after committed ticket: %v", err)
				}
				return
			}
			assertRevokeWins(t, issueErr)
			if store.Count() != 0 {
				t.Fatalf("ticket side effect after revoke: %d", store.Count())
			}
		})
	}
}

func TestPostAuthorizationRevokeBarrier_ConnectionAndNotification(t *testing.T) {
	for _, tc := range []struct {
		name string
		win  bool
	}{
		{name: "revoke-wins"},
		{name: "mutation-wins", win: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth, deviceID, epoch := newPostAuthBarrier(t)
			connections := devicetrust.NewAuthenticatedConnRegistry(auth)
			barrierRun := auth.revokeAndRelease
			if tc.win {
				barrierRun = auth.releaseAfterAuthorization
			}
			err := barrierRun(t, func() error {
				return connections.Register(deviceID, epoch, barrierCloser{})
			})
			if tc.win {
				if err != nil || connections.Count(deviceID) != 1 {
					t.Fatalf("mutation-wins connection=(%v,count=%d)", err, connections.Count(deviceID))
				}
				if err := auth.reg.Revoke(deviceID); err != nil {
					t.Fatalf("revoke after committed connection: %v", err)
				}
				return
			}
			assertRevokeWins(t, err)
			if connections.Count(deviceID) != 0 {
				t.Fatalf("connection side effect after revoke: %d", connections.Count(deviceID))
			}
		})
	}

	// Notification registration is a separate sink using the same real
	// registry barrier, so push/token binding cannot bypass the post-auth check.
	auth, deviceID, epoch := newPostAuthBarrier(t)
	devices := notification.NewDeviceStore(auth)
	err := auth.revokeAndRelease(t, func() error { return devices.Bind(deviceID, "push", int64(epoch)) })
	assertRevokeWins(t, err)
	if got := devices.Token(deviceID); got != "" {
		t.Fatalf("notification side effect after revoke: %q", got)
	}

	auth, deviceID, epoch = newPostAuthBarrier(t)
	devices = notification.NewDeviceStore(auth)
	if err := auth.releaseAfterAuthorization(t, func() error { return devices.Bind(deviceID, "push-wins", int64(epoch)) }); err != nil {
		t.Fatalf("mutation-wins notification bind: %v", err)
	}
	if got := devices.Token(deviceID); got != "push-wins" {
		t.Fatalf("committed notification token=%q", got)
	}
	if err := auth.reg.Revoke(deviceID); err != nil {
		t.Fatalf("revoke after committed notification bind: %v", err)
	}
}

func TestPostAuthorizationRevokeBarrier_Resize(t *testing.T) {
	for _, tc := range []struct {
		name string
		win  bool
	}{
		{name: "revoke-wins"},
		{name: "mutation-wins", win: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth, deviceID, epoch := newPostAuthBarrier(t)
			resizer := &barrierResizer{}
			transport := newTerminalTransport("controlled_pty:post-resize", 1, nil, resizer, nil, auth)
			barrierRun := auth.revokeAndRelease
			if tc.win {
				barrierRun = auth.releaseAfterAuthorization
			}
			err := barrierRun(t, func() error { return transport.Resize(24, 80, deviceID, epoch) })
			if tc.win {
				if err != nil || resizer.calls != 1 {
					t.Fatalf("mutation-wins resize=(%v,calls=%d)", err, resizer.calls)
				}
				if err := auth.reg.Revoke(deviceID); err != nil {
					t.Fatalf("revoke after committed resize: %v", err)
				}
				return
			}
			assertRevokeWins(t, err)
			if resizer.calls != 0 {
				t.Fatalf("resize side effect after revoke: %d", resizer.calls)
			}
		})
	}
}
