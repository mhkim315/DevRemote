package term

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
)

// ipcRoundTripWith drives the real IPC connection handler with an explicit
// mux registry and managed service (either may be nil).
func ipcRoundTripWith(t *testing.T, reg *mux.Registry, managed *ManagedCodexService, body map[string]any) map[string]string {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	go handleIPCConnection(serverConn, reg, nil, nil, managed, nil)
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := clientConn.Write(append(payload, '\n')); err != nil {
		t.Fatalf("write request: %v", err)
	}
	line, err := bufio.NewReader(clientConn).ReadString('\n')
	if err != nil && err != io.EOF {
		t.Fatalf("read response: %v", err)
	}
	clientConn.Close()
	var resp map[string]string
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("decode response %q: %v", line, err)
	}
	return resp
}

// ── Remediation blocker 1: managed-disabled must fail closed, never fall back ──

// TestManagedIPCCreate_DisabledFailsClosed_NoLegacyFallback: the structured
// detached codex request with the managed service DISABLED returns an explicit
// unavailable error — no legacy controlled_pty session, no child, no launcher.
func TestManagedIPCCreate_DisabledFailsClosed_NoLegacyFallback(t *testing.T) {
	reg := mux.MustNewRegistry() // any legacy fallback attempt would be visible here
	resp := ipcRoundTripWith(t, reg, nil /* managed disabled */, sp0DetachCodexRequest())
	if !strings.Contains(resp["error"], "managed codex runtime unavailable") {
		t.Fatalf("error = %q, want explicit managed-unavailable rejection", resp["error"])
	}
	if resp["id"] != "" {
		t.Fatalf("disabled managed create returned a session id: %v", resp)
	}
	if got := reg.Sessions(context.Background()); len(got) != 0 {
		t.Fatalf("legacy session created despite disabled managed runtime: %v", got)
	}
}

// TestManagedIPCCreate_EnabledUsesManagedRuntime: the SAME structured request
// with the managed service ENABLED reaches the native runtime (paired
// production test for the enabled/disabled contract).
func TestManagedIPCCreate_EnabledUsesManagedRuntime(t *testing.T) {
	fl := &fakeLauncher{handler: happyAppServer("thread-EN")}
	managed := newTestManagedService(fl)

	resp := ipcCreateRoundTrip(t, managed, sp0DetachCodexRequest())
	if resp["error"] != "" || !strings.HasPrefix(resp["id"], "codex_app_server:") {
		t.Fatalf("enabled managed create = %v", resp)
	}
	if fl.callCount() != 1 {
		t.Fatalf("launcher calls = %d, want 1", fl.callCount())
	}
}

// ── Remediation blocker 2: CreateDetached vs Shutdown linearization ──

func runtimesLen(s *ManagedCodexService) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.runtimes)
}

func leasesLen(s *ManagedCodexService) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.leases)
}

// assertDrainedShutdownState asserts the Shutdown success postconditions:
// no in-flight create, empty runtime map, closed registry, no records.
func assertDrainedShutdownState(t *testing.T, managed *ManagedCodexService) {
	t.Helper()
	if n := leasesLen(managed); n != 0 {
		t.Fatalf("%d in-flight creates after shutdown returned", n)
	}
	if n := runtimesLen(managed); n != 0 {
		t.Fatalf("runtime map has %d entries after shutdown returned", n)
	}
	if n := len(managed.Registry().List()); n != 0 {
		t.Fatalf("registry has %d records after shutdown returned", n)
	}
	if managed.Registry().Register(testRecord("codex_app_server:post", 99)) == nil {
		t.Fatal("registry not closed after shutdown returned")
	}
}

// TestManagedCreate_ShutdownRace_PostSpawn: a create parked between spawn and
// publication holds an open in-flight lease. Shutdown must (a) NOT return
// while that lease is open, (b) cancel-kill the spawned child even before the
// create resumes, and (c) return only after the create rolled back — so a
// successful Shutdown never leaves a POKIT-spawned child alive.
func TestManagedCreate_ShutdownRace_PostSpawn(t *testing.T) {
	fl := &fakeLauncher{handler: happyAppServer("thread-R1")}
	managed := newTestManagedService(fl)

	reached := make(chan struct{})
	release := make(chan struct{})
	managed.createBarrier = func(stage string) {
		if stage == "post-spawn" {
			close(reached)
			<-release
		}
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := managed.CreateDetached("")
		errCh <- err
	}()
	<-reached // the child exists, but is not yet published

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- managed.Shutdown(ctx) }()

	// The lease cancel must kill the unpublished child WITHOUT waiting for
	// the create goroutine to resume.
	select {
	case <-fl.procs[0].killed:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not cancel the spawned-but-unpublished child")
	}
	// Shutdown must NOT have returned: the in-flight create is still open.
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned (%v) while a create was still in flight", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(release) // resume the create — it must roll back and fail closed
	err := <-errCh
	if err == nil || !strings.Contains(err.Error(), "shutting down") {
		t.Fatalf("create err = %v, want fail-closed shutting-down", err)
	}
	// Only now may Shutdown return, and it must return clean.
	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not return after the in-flight create drained")
	}
	assertDrainedShutdownState(t, managed)
}

// TestManagedCreate_ShutdownRace_PostRegister: shutdown races a create whose
// child is registered and published — the snapshot owns the child (killed
// immediately), Shutdown still waits for the create's lease, and the resumed
// create rolls the record back before Shutdown returns.
func TestManagedCreate_ShutdownRace_PostRegister(t *testing.T) {
	fl := &fakeLauncher{handler: happyAppServer("thread-R2")}
	managed := newTestManagedService(fl)

	reached := make(chan struct{})
	release := make(chan struct{})
	managed.createBarrier = func(stage string) {
		if stage == "post-register" {
			close(reached)
			<-release
		}
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := managed.CreateDetached("")
		errCh <- err
	}()
	<-reached // registered + published, certification turn not yet started

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- managed.Shutdown(ctx) }()

	// Shutdown's snapshot owns the published child: killed without waiting
	// for the create to resume.
	select {
	case <-fl.procs[0].killed:
	case <-time.After(2 * time.Second):
		t.Fatal("published child not killed by shutdown snapshot")
	}
	// But Shutdown must still be waiting on the open lease.
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned (%v) while a create was still in flight", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(release) // resume: turn/start write must fail on the dead child
	err := <-errCh
	if err == nil {
		t.Fatal("create succeeded across shutdown")
	}
	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not return after the in-flight create drained")
	}
	assertDrainedShutdownState(t, managed)
}

// TestManagedShutdown_BoundedWhenCreateNeverResumes: a pathologically stuck
// create cannot block shutdown forever — the ctx bound expires with an error
// (honest partial: the child is killed but the caller learns the drain did
// not complete).
func TestManagedShutdown_BoundedWhenCreateNeverResumes(t *testing.T) {
	fl := &fakeLauncher{handler: happyAppServer("thread-R4")}
	managed := newTestManagedService(fl)

	reached := make(chan struct{})
	release := make(chan struct{})
	managed.createBarrier = func(stage string) {
		if stage == "post-spawn" {
			close(reached)
			<-release
		}
	}
	go func() { _, _ = managed.CreateDetached("") }()
	<-reached

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := managed.Shutdown(ctx); err == nil {
		t.Fatal("shutdown reported clean drain while a create was stuck")
	}
	select {
	case <-fl.procs[0].killed:
	case <-time.After(2 * time.Second):
		t.Fatal("stuck create's child not killed")
	}
	close(release)
}

// TestManagedCreate_AfterShutdown_FailsBeforeSpawn: once shutdown began, a
// new create fails closed BEFORE verify/spawn — the launcher is never invoked.
func TestManagedCreate_AfterShutdown_FailsBeforeSpawn(t *testing.T) {
	fl := &fakeLauncher{handler: happyAppServer("thread-R3")}
	managed := newTestManagedService(fl)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := managed.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if _, err := managed.CreateDetached(""); err == nil || !strings.Contains(err.Error(), "shutting down") {
		t.Fatalf("post-shutdown create err = %v", err)
	}
	if fl.callCount() != 0 {
		t.Fatalf("launcher invoked %d times after shutdown", fl.callCount())
	}
}

// ── Remediation blocker 3: managed surface bounded while discovery hangs ──

// hangingAdapter blocks discovery until unblocked — a hanging backend that
// neither errors nor returns.
type hangingAdapter struct {
	name    string
	unblock chan struct{}
}

func (a hangingAdapter) Name() string { return a.name }
func (a hangingAdapter) ListSessions(context.Context) ([]mux.Session, error) {
	<-a.unblock
	return nil, nil
}

// TestManagedREST_BoundedWhileDiscoveryHangs: with external adapter-style discovery
// BLOCKED (not merely erroring), the dedicated managed list, the native-status
// get, and a managed create all complete within a bounded time — and a later
// discovery success does not change the managed status.
func TestManagedREST_BoundedWhileDiscoveryHangs(t *testing.T) {
	unblock := make(chan struct{})
	reg := mux.MustNewRegistry(
		hangingAdapter{name: "ext", unblock: unblock},
		hangingAdapter{name: "ext2", unblock: unblock},
	)
	// Occupy discovery exactly like an observer caller would.
	discoveryDone := make(chan struct{})
	go func() {
		reg.Sessions(context.Background())
		close(discoveryDone)
	}()

	managed, id := createManagedForAPI(t)
	h := &Handlers{Registry: reg, Managed: managed, Catalog: catalogForAPI(managed, nil)}

	type outcome struct {
		listCode, statusCode int
		listBody, statusBody string
		createErr            error
	}
	got := make(chan outcome, 1)
	go func() {
		var o outcome
		rec := httptest.NewRecorder()
		h.HandleManagedSessions(rec, httptest.NewRequest("GET", "/api/managed-sessions", nil))
		o.listCode, o.listBody = rec.Code, rec.Body.String()

		req := httptest.NewRequest("GET", "/api/sessions/x/native-status", nil)
		req.SetPathValue("id", id)
		rec2 := httptest.NewRecorder()
		h.HandleManagedNativeStatus(rec2, req)
		o.statusCode, o.statusBody = rec2.Code, rec2.Body.String()

		_, o.createErr = managed.CreateDetached("")
		got <- o
	}()

	var o outcome
	select {
	case o = <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("managed list/get/create blocked by hanging discovery")
	}
	if o.listCode != 200 || !strings.Contains(o.listBody, id) {
		t.Fatalf("managed list under hang: code=%d body=%s", o.listCode, o.listBody)
	}
	var dtos []ManagedNativeStatusDTO
	if err := json.Unmarshal([]byte(o.listBody), &dtos); err != nil {
		t.Fatalf("decode managed list: %v", err)
	}
	if o.statusCode != 200 || !strings.Contains(o.statusBody, `"nativeStatus":"idle"`) {
		t.Fatalf("native-status under hang: code=%d body=%s", o.statusCode, o.statusBody)
	}
	if o.createErr != nil {
		t.Fatalf("managed create under hang: %v", o.createErr)
	}

	// Discovery later succeeds — the managed status must be unchanged.
	close(unblock)
	select {
	case <-discoveryDone:
	case <-time.After(5 * time.Second):
		t.Fatal("discovery never completed after unblock")
	}
	if rec, _ := managed.Registry().Get(id); rec.NativeStatus != ManagedStatusIdle {
		t.Fatalf("observer success changed managed status: %+v", rec)
	}
}
