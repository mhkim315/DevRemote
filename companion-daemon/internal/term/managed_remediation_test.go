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
	go handleIPCConnection(serverConn, reg, nil, nil, nil, nil, nil, managed)
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

// TestManagedCreate_ShutdownRace_PostSpawn: shutdown wins between spawn and
// publication — the create observes closing, kills + reaps its OWN child,
// returns fail-closed, and leaves no record and no runtime.
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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := managed.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	close(release) // resume the create

	err := <-errCh
	if err == nil || !strings.Contains(err.Error(), "shutting down") {
		t.Fatalf("create err = %v, want fail-closed shutting-down", err)
	}
	select {
	case <-fl.procs[0].killed:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight child survived daemon shutdown")
	}
	if n := len(managed.Registry().List()); n != 0 {
		t.Fatalf("registry has %d records after raced create", n)
	}
	if n := runtimesLen(managed); n != 0 {
		t.Fatalf("runtime map has %d entries after raced create", n)
	}
}

// TestManagedCreate_ShutdownRace_PostRegister: shutdown wins after the record
// is registered and the child is published — the snapshot MUST own the child
// (killed + reaped), the resumed create fails closed and rolls the record
// back, and nothing stays current.
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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := managed.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	// Shutdown's snapshot owned the published child: already killed + reaped.
	select {
	case <-fl.procs[0].killed:
	default:
		t.Fatal("published child not found by shutdown snapshot")
	}

	close(release) // resume: turn/start write must fail on the dead child
	err := <-errCh
	if err == nil {
		t.Fatal("create succeeded across shutdown")
	}
	if n := len(managed.Registry().List()); n != 0 {
		t.Fatalf("registry has %d records after raced create (record not rolled back)", n)
	}
	if n := runtimesLen(managed); n != 0 {
		t.Fatalf("runtime map has %d entries after raced create", n)
	}
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

// hangingAdapter blocks discovery until unblocked — a tmux/cmux backend that
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

// TestManagedREST_BoundedWhileDiscoveryHangs: with tmux/cmux-style discovery
// BLOCKED (not merely erroring), the dedicated managed list, the native-status
// get, and a managed create all complete within a bounded time — and a later
// discovery success does not change the managed status.
func TestManagedREST_BoundedWhileDiscoveryHangs(t *testing.T) {
	unblock := make(chan struct{})
	reg := mux.MustNewRegistry(
		hangingAdapter{name: "tmux", unblock: unblock},
		hangingAdapter{name: "cmux", unblock: unblock},
	)
	// Occupy discovery exactly like an observer caller would.
	discoveryDone := make(chan struct{})
	go func() {
		reg.Sessions(context.Background())
		close(discoveryDone)
	}()

	managed, id := createManagedForAPI(t)
	h := &Handlers{Registry: reg, Events: NewMemoryEventStore(), Managed: managed}

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
