package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/term"
)

// ── Test-only adapter/session for registry isolation tests ──

type testSession struct {
	id, title, adapter string
}

func (s *testSession) ID() string          { return s.id }
func (s *testSession) Title() string       { return s.title }
func (s *testSession) AdapterName() string { return s.adapter }

type testAdapter struct {
	name     string
	sessions []mux.Session
}

func (a *testAdapter) Name() string { return a.name }
func (a *testAdapter) ListSessions(ctx context.Context) ([]mux.Session, error) {
	return a.sessions, nil
}
func (a *testAdapter) GetSession(id string) (mux.Session, error) {
	for _, s := range a.sessions {
		if s.ID() == id {
			return s, nil
		}
	}
	return nil, errors.New("not found")
}

// ── Fake resources for lifecycle tests ──

type fakeIPC struct {
	mu        sync.Mutex
	closeErr  error
	waitErr   error
	closeCnt  int
	closeOrdr *[]string
	name      string
	blocking  bool
}

func (f *fakeIPC) Close() error {
	f.mu.Lock()
	f.closeCnt++
	f.mu.Unlock()
	if f.closeOrdr != nil {
		*f.closeOrdr = append(*f.closeOrdr, f.name+":close")
	}
	return f.closeErr
}

func (f *fakeIPC) Wait(ctx context.Context) error {
	if f.closeOrdr != nil {
		*f.closeOrdr = append(*f.closeOrdr, f.name+":wait")
	}
	if f.waitErr != nil {
		return f.waitErr
	}
	if f.blocking {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

type fakeWatcher struct {
	mu        sync.Mutex
	closeErr  error
	closed    bool
	closeOrdr *[]string
	name      string
}

func (f *fakeWatcher) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	if f.closeOrdr != nil {
		*f.closeOrdr = append(*f.closeOrdr, f.name+":close")
	}
	return f.closeErr
}

type fakeTunnel struct {
	done      chan struct{}
	signalErr error
	sigOrdr   *[]string
	name      string
}

func (f *fakeTunnel) Done() <-chan struct{} { return f.done }

func (f *fakeTunnel) Signal(sig os.Signal) error {
	if f.sigOrdr != nil {
		*f.sigOrdr = append(*f.sigOrdr, f.name+":signal")
	}
	return f.signalErr
}

func newFakeTunnelDone() *fakeTunnel {
	t := &fakeTunnel{done: make(chan struct{})}
	close(t.done)
	return t
}

// ── Tests ──

func TestNewApp_CreatesPrivateMux(t *testing.T) {
	// Not Parallel — shared config mutation in verifier creation.
	cfg := Config{InsecureLocalOnly: true}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}
	if app.registry == nil {
		t.Fatal("registry is nil")
	}
	if app.server == nil {
		t.Fatal("server is nil")
	}
	if app.server.Handler == nil {
		t.Fatal("server handler is nil")
	}
}

func TestPrivateMux_NoDefaultMuxUsage(t *testing.T) {
	// Not Parallel — shared config mutation in verifier creation.
	cfg := Config{InsecureLocalOnly: true}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}
	if app.server.Handler == http.DefaultServeMux {
		t.Fatal("server handler is DefaultServeMux, private mux required")
	}
}

func TestHandlers_RegistryDataIsolation(t *testing.T) {
	// Not Parallel — NewApp sets term.OnApproval global.
	regA := mux.MustNewRegistry()
	regB := mux.MustNewRegistry()

	adapterA := &testAdapter{
		name: "test-a",
		sessions: []mux.Session{
			&testSession{id: "session-a", title: "Session A", adapter: "test-a"},
		},
	}
	adapterB := &testAdapter{
		name: "test-b",
		sessions: []mux.Session{
			&testSession{id: "session-b", title: "Session B", adapter: "test-b"},
		},
	}
	regA.Register(adapterA)
	regB.Register(adapterB)

	hA := &term.Handlers{Registry: regA, Verifier: insecureVerifier(), }
	hB := &term.Handlers{Registry: regB, Verifier: insecureVerifier(), }

	if hA.Registry == hB.Registry {
		t.Fatal("expected different registries")
	}

	muxA := http.NewServeMux()
	muxA.HandleFunc("/api/sessions", hA.AuthMiddleware(hA.HandleSessionsAPI))
	srvA := httptest.NewServer(muxA)
	defer srvA.Close()

	muxB := http.NewServeMux()
	muxB.HandleFunc("/api/sessions", hB.AuthMiddleware(hB.HandleSessionsAPI))
	srvB := httptest.NewServer(muxB)
	defer srvB.Close()

	respA, err := http.Get(srvA.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("server A request failed: %v", err)
	}
	defer respA.Body.Close()

	respB, err := http.Get(srvB.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("server B request failed: %v", err)
	}
	defer respB.Body.Close()

	if respA.StatusCode != http.StatusOK {
		t.Errorf("server A status = %d, want 200", respA.StatusCode)
	}
	if respB.StatusCode != http.StatusOK {
		t.Errorf("server B status = %d, want 200", respB.StatusCode)
	}

	bufA := make([]byte, 8192)
	nA, _ := respA.Body.Read(bufA)
	bodyStrA := string(bufA[:nA])

	bufB := make([]byte, 8192)
	nB, _ := respB.Body.Read(bufB)
	bodyStrB := string(bufB[:nB])

	if !strings.Contains(bodyStrA, "session-a") {
		t.Errorf("server A response missing 'session-a': %s", bodyStrA)
	}
	if strings.Contains(bodyStrA, "session-b") {
		t.Errorf("server A response leaked 'session-b' from registry B: %s", bodyStrA)
	}
	if !strings.Contains(bodyStrB, "session-b") {
		t.Errorf("server B response missing 'session-b': %s", bodyStrB)
	}
	if strings.Contains(bodyStrB, "session-a") {
		t.Errorf("server B response leaked 'session-a' from registry A: %s", bodyStrB)
	}
}

func TestApp_ShutdownOrder(t *testing.T) {
	// Verify shutdown order: HTTP → telemetry → watcher → IPC.
	// HTTP shutdown is verified by checking the listener is closed after Shutdown.
	// telemetry/watcher/IPC order is verified via the recorder.
	var order []string

	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, testDeps())
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}

	// Real listener so we can verify it's closed after Shutdown.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	app.server.Addr = l.Addr().String()
	go app.server.Serve(l)

	// Inject fake resources with order recorder.
	app.watcher = &fakeWatcher{closeOrdr: &order, name: "watcher"}
	app.ipc = &fakeIPC{closeOrdr: &order, name: "ipc"}

	// Telemetry: record cancel timing. Use a minimal service so Done() is already closed.
	app.telemetry = term.NewTelemetryService(app.registry, nil, nil, nil, nil)
	app.telemetryCtxCancel = func() {
		order = append(order, "telemetry:cancel")
		// Start + immediately cancel so Done() is closed.
		ctx, c := context.WithCancel(context.Background())
		go app.telemetry.Run(ctx)
		c()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = app.Shutdown(ctx)
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	t.Logf("shutdown order: %v", order)

	// 1. HTTP server stopped accepting (listener closed).
	if _, err := net.Dial("tcp", l.Addr().String()); err == nil {
		t.Error("HTTP listener still accepting after Shutdown")
	}

	// 2. Verify telemetry → watcher → IPC order.
	telIdx := indexOf(order, "telemetry:cancel")
	watcherIdx := indexOf(order, "watcher:close")
	ipcCloseIdx := indexOf(order, "ipc:close")
	ipcWaitIdx := indexOf(order, "ipc:wait")

	if telIdx < 0 {
		t.Error("telemetry was not cancelled")
	}
	if watcherIdx < 0 {
		t.Error("watcher was not closed")
	}
	if ipcCloseIdx < 0 {
		t.Error("ipc was not closed")
	}
	if ipcWaitIdx < 0 {
		t.Error("ipc Wait was not called")
	}

	// Telemetry must precede watcher.
	if telIdx >= 0 && watcherIdx >= 0 && telIdx > watcherIdx {
		t.Error("telemetry cancelled after watcher close — order violation")
	}
	// Watcher must precede IPC.
	if watcherIdx >= 0 && ipcCloseIdx >= 0 && watcherIdx > ipcCloseIdx {
		t.Error("watcher closed after ipc close — order violation")
	}
	// IPC close before IPC wait.
	if ipcCloseIdx >= 0 && ipcWaitIdx >= 0 && ipcCloseIdx > ipcWaitIdx {
		t.Error("ipc close after ipc wait — order violation")
	}
}

func TestApp_ShutdownContinuesAfterError(t *testing.T) {
	// When one resource errors, subsequent resources are still cleaned up.
	var order []string
	watcherCloseErr := errors.New("watcher close failed")

	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, testDeps())
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}

	app.watcher = &fakeWatcher{closeOrdr: &order, name: "watcher", closeErr: watcherCloseErr}
	app.ipc = &fakeIPC{closeOrdr: &order, name: "ipc"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdownErr := app.Shutdown(ctx)
	if shutdownErr == nil {
		t.Fatal("expected shutdown error from failing watcher, got nil")
	}
	if !errors.Is(shutdownErr, watcherCloseErr) {
		t.Errorf("shutdown error does not wrap watcher error: %v", shutdownErr)
	}

	// IPC should still have been cleaned up despite watcher error.
	if idx := indexOf(order, "ipc:close"); idx < 0 {
		t.Error("ipc was not closed — cleanup did not continue after error")
	}
}

func TestApp_ShutdownRespectsDeadline(t *testing.T) {
	// A blocking IPC Wait must respect the shutdown deadline.
	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, testDeps())
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}
	app.ipc = &fakeIPC{blocking: true}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	shutdownErr := app.Shutdown(ctx)
	if shutdownErr == nil {
		t.Fatal("expected shutdown deadline error, got nil")
	}
	if !errors.Is(shutdownErr, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got: %v", shutdownErr)
	}
}

func TestApp_ShutdownRemovesIPCPathAndAllowsRebind(t *testing.T) {
	dir, err := os.MkdirTemp("", "p2")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)
	socketPath := filepath.Join(dir, "s")

	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, testDeps())
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}
	app.ipcPath = socketPath

	srv, err := term.StartIPCServer(socketPath, app.registry, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("StartIPCServer failed: %v", err)
	}
	app.ipc = srv

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Errorf("socket still exists after Shutdown: %v", err)
	}

	// Rebind without manual Remove.
	srv2, err := term.StartIPCServer(socketPath, app.registry, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("rebind StartIPCServer failed: %v", err)
	}
	srv2.Close()
	srv2.Wait(ctx)
}

func TestApp_TunnelNotStartedInInsecureMode(t *testing.T) {
	tunnelStarted := false
	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, Dependencies{
		Cmds:         term.NewCommandBroker(),
		StartWatcher: func() (watcherResource, error) { return &fakeWatcher{}, nil },
		StartIPC: func(path string, reg *mux.Registry, telemetry *term.TelemetryService) (ipcResource, error) {
			return &fakeIPC{}, nil
		},
		StartTunnel: func() tunnelResource {
			tunnelStarted = true
			return newFakeTunnelDone()
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}

	// Simulate Run's resource startup (without ListenAndServe).
	app.watcher = app.startWatcher()
	ipc, _ := app.startIPC()
	app.ipc = ipc
	if !app.config.InsecureLocalOnly {
		app.tunnel = app.startTunnel()
	}

	if tunnelStarted {
		t.Error("tunnel factory was called in insecure mode")
	}
	if app.tunnel != nil {
		t.Error("tunnel resource is non-nil in insecure mode")
	}
}

func TestApp_TunnelStartedInProductionMode(t *testing.T) {
	tunnelStarted := false
	cfg := Config{InsecureLocalOnly: false}
	app, err := NewAppWithDeps(cfg, Dependencies{
		Cmds:         term.NewCommandBroker(),
		StartWatcher: func() (watcherResource, error) { return &fakeWatcher{}, nil },
		StartIPC: func(path string, reg *mux.Registry, telemetry *term.TelemetryService) (ipcResource, error) {
			return &fakeIPC{}, nil
		},
		StartTunnel: func() tunnelResource {
			tunnelStarted = true
			return newFakeTunnelDone()
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}

	app.watcher = app.startWatcher()
	ipc, _ := app.startIPC()
	app.ipc = ipc
	if !app.config.InsecureLocalOnly {
		app.tunnel = app.startTunnel()
	}

	if !tunnelStarted {
		t.Error("tunnel factory was NOT called in production mode")
	}
	if app.tunnel == nil {
		t.Error("tunnel resource is nil in production mode")
	}
}

func TestApp_RunContextCancelReturnsNil(t *testing.T) {
	// Cancel before Run starts → clean shutdown, nil return.
	// Use httptest to get a unique port, then use it for the app.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := ts.Listener.Addr().String()
	ts.Close() // free the port

	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, Dependencies{
		Cmds:         term.NewCommandBroker(),
		StartWatcher: func() (watcherResource, error) { return &fakeWatcher{}, nil },
		StartIPC: func(path string, reg *mux.Registry, telemetry *term.TelemetryService) (ipcResource, error) {
			return &fakeIPC{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}
	app.server.Addr = addr

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runErr := app.Run(ctx)
	if runErr != nil {
		t.Errorf("expected nil after context cancel shutdown, got: %v", runErr)
	}
}

func TestApp_RunReturnsHTTPServeError(t *testing.T) {
	// A non-ErrServerClosed serve error must be preserved by Run.
	// Use httptest to get a unique port.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := ts.Listener.Addr().String()
	ts.Close()

	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, Dependencies{
		Cmds:         term.NewCommandBroker(),
		StartWatcher: func() (watcherResource, error) { return &fakeWatcher{}, nil },
		StartIPC: func(path string, reg *mux.Registry, telemetry *term.TelemetryService) (ipcResource, error) {
			return &fakeIPC{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}
	app.server.Addr = addr

	// Block the port so ListenAndServe fails.
	l, err := listenFreeTCPOnAddr(addr)
	if err != nil {
		t.Skipf("cannot block port: %v", err)
	}
	defer l.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runErr := app.Run(ctx)
	if runErr == nil {
		t.Fatal("expected non-nil error from Run")
	}
	// The serve error should be address-already-in-use.
	if !strings.Contains(runErr.Error(), "address already in use") {
		t.Errorf("expected 'address already in use' in error, got: %v", runErr)
	}
	t.Logf("Run error: %v", runErr)
}

func TestApp_RunJoinsServeAndShutdownErrors(t *testing.T) {
	// Both serve and shutdown errors must be present via errors.Is.
	shutdownErr := errors.New("shutdown-ipc-failed")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := ts.Listener.Addr().String()
	ts.Close()

	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, Dependencies{
		Cmds:         term.NewCommandBroker(),
		StartWatcher: func() (watcherResource, error) { return &fakeWatcher{}, nil },
		StartIPC: func(path string, reg *mux.Registry, telemetry *term.TelemetryService) (ipcResource, error) {
			return &fakeIPC{waitErr: shutdownErr}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}
	app.server.Addr = addr

	// Block the port.
	l, err := listenFreeTCPOnAddr(addr)
	if err != nil {
		t.Skipf("cannot block port: %v", err)
	}
	defer l.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	runErr := app.Run(ctx)
	if runErr == nil {
		t.Fatal("expected joined error, got nil")
	}

	// Both serve error and shutdown error must be detectable.
	if !strings.Contains(runErr.Error(), "address already in use") {
		t.Errorf("serve error not found in Run result: %v", runErr)
	}
	if !errors.Is(runErr, shutdownErr) {
		t.Errorf("shutdown error %q not found via errors.Is in: %v", shutdownErr, runErr)
	}
	t.Logf("Run joined error: %v", runErr)
}

func TestApp_TunnelShutdownSignalsAndWaits(t *testing.T) {
	var order []string
	tun := &fakeTunnel{
		done:    make(chan struct{}),
		sigOrdr: &order,
		name:    "tunnel",
	}

	cfg := Config{InsecureLocalOnly: false}
	app, err := NewAppWithDeps(cfg, testDeps())
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}
	app.tunnel = tun
	app.ipc = &fakeIPC{}

	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		errCh <- app.Shutdown(ctx)
	}()

	time.Sleep(100 * time.Millisecond)
	close(tun.done)

	select {
	case shutdownErr := <-errCh:
		if shutdownErr != nil {
			t.Fatalf("Shutdown failed: %v", shutdownErr)
		}
		if idx := indexOf(order, "tunnel:signal"); idx < 0 {
			t.Error("tunnel was not signalled")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Shutdown did not complete within deadline")
	}
}

func TestApp_TunnelAlreadyExitedSkipsSignal(t *testing.T) {
	// When the tunnel already exited, Signal returns os.ErrProcessDone
	// which must be ignored (not treated as a shutdown error).
	var order []string
	tun := &fakeTunnel{
		done:      make(chan struct{}),
		sigOrdr:   &order,
		name:      "tunnel",
		signalErr: os.ErrProcessDone,
	}
	close(tun.done) // already exited

	cfg := Config{InsecureLocalOnly: false}
	app, err := NewAppWithDeps(cfg, testDeps())
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}
	app.tunnel = tun
	app.ipc = &fakeIPC{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdownErr := app.Shutdown(ctx)
	if shutdownErr != nil {
		t.Fatalf("Shutdown should ignore os.ErrProcessDone, got: %v", shutdownErr)
	}
	// Signal was called and returned os.ErrProcessDone which was ignored.
	if idx := indexOf(order, "tunnel:signal"); idx < 0 {
		t.Error("tunnel was not signalled")
	}
}

// insecureVerifier returns a verifier that accepts empty tokens (insecure mode).
func insecureVerifier() term.TokenVerifier {
	return term.NewSupabaseVerifier(term.AuthConfig{InsecureLocalOnly: true})
}

// testDeps returns Dependencies with no-op stores so tests never touch
// real files or network. Individual fields can be overridden.
func testDeps() Dependencies {
	return Dependencies{
		Cmds:   term.NewCommandBroker(),
	}
}

// ── Helpers ──

func indexOf(slice []string, s string) int {
	for i, v := range slice {
		if v == s {
			return i
		}
	}
	return -1
}

func TestApp_InjectedVerifierUsedByRoutes(t *testing.T) {
	// Verify that a fake verifier injected via Dependencies.Verifier
	// is actually called when hitting app routes.
	cfg := Config{
		InsecureLocalOnly:  true,
		OwnerUUID:          "owner",
		SupabaseProjectRef: "test",
	}

	fv := &fakeAuthVerifier{reject: "injected-verifier"}
	app, err := NewAppWithDeps(cfg, Dependencies{
		Verifier:     fv,
		StartWatcher: func() (watcherResource, error) { return &fakeWatcher{}, nil },
		StartIPC: func(path string, reg *mux.Registry, telemetry *term.TelemetryService) (ipcResource, error) {
			return &fakeIPC{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}

	ts := httptest.NewServer(app.server.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Errorf("got %d, want 401 (rejected by injected verifier)", resp.StatusCode)
	}
	if !fv.called {
		t.Error("injected verifier was not called by route handler")
	}
}

type fakeAuthVerifier struct {
	called bool
	reject string
}

func (f *fakeAuthVerifier) Verify(ctx context.Context, token string) error {
	f.called = true
	if f.reject != "" {
		return fmt.Errorf("%s", f.reject)
	}
	return nil
}

func TestPushNotifier_TokenRace(t *testing.T) {
	// Verify that concurrent SetToken and ApprovalRequired don't race.
	n := &pushNotifier{send: func(token, msg, sessionID string) {}} // no-op sender
	n.SetToken("test-token")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			n.SetToken("t")
		}()
		go func() {
			defer wg.Done()
			_ = n.ApprovalRequired(context.Background(), "", "")
		}()
	}
	wg.Wait()
}

// TestNewAppWithDeps_CatalogWiring proves that when managed Codex is
// enabled the composition root installs Catalog on Handlers and wires
// RuntimeOf to Catalog.RuntimeOf.
func TestNewAppWithDeps_CatalogWiring(t *testing.T) {
	// Create a fresh managed Codex service without pre-configuring the
	// approval store — let NewAppWithDeps own the composition.
	launcher := &fakeCodexLauncher{}
	svc := term.NewManagedCodexService(term.CodexAppServerEntryConfig{
		Bin:              "/pinned/toolchain/node_modules/.bin/codex",
		Version:          "codex-cli 0.144.1",
		AuthorityVersion: "0.144.1",
	}, launcher)

	cfg := Config{
		InsecureLocalOnly:  true,
		EnableManagedCodex: true,
		OwnerUUID:          "test-owner",
		SupabaseProjectRef: "test-ref",
	}

	app, err := NewAppWithDeps(cfg, Dependencies{
		Managed: svc,
		Cmds:    term.NewCommandBroker(),
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps: %v", err)
	}

	// Catalog must be installed on Handlers.
	if app.handlers == nil {
		t.Fatal("handlers is nil")
	}
	if app.handlers.Catalog == nil {
		t.Fatal("Catalog is nil — should be installed when managed codex is enabled")
	}

	// RuntimeOf must delegate to Catalog.RuntimeOf (same function pointer
	// when the catalog owns the resolver). Both must return the same
	// result for any lookup.
	unknownID := "codex_app_server:nonexistent"
	_, catOK := app.handlers.Catalog.RuntimeOf(unknownID)
	if catOK {
		t.Fatal("Catalog.RuntimeOf returned true for nonexistent ID")
	}
	_, rtOK := app.handlers.RuntimeOf(unknownID)
	if rtOK {
		t.Fatal("RuntimeOf returned true for nonexistent ID")
	}
}

// fakeCodexLauncher is a minimal launcher for composition tests.
type fakeCodexLauncher struct{}

func (l *fakeCodexLauncher) Launch(exe string, argv []string) (term.ManagedProcess, error) {
	return &fakeCodexProc{}, nil
}

type fakeCodexProc struct{}

func (p *fakeCodexProc) Stdin() io.Writer  { return io.Discard }
func (p *fakeCodexProc) Stdout() io.Reader { return strings.NewReader("") }
func (p *fakeCodexProc) Term() error       { return nil }
func (p *fakeCodexProc) Kill() error       { return nil }
func (p *fakeCodexProc) Wait() error       { return nil }
func (p *fakeCodexProc) OpaqueID() string  { return "fake-opaque-id" }
func (p *fakeCodexProc) PID() int          { return 99999 }

// TestPA2a_LinkRoutesRemoved_Insecure verifies /api/v2/links returns 404
// (unregistered, not auth-failing) in insecure local-only mode.
func TestPA2a_LinkRoutesRemoved_Insecure(t *testing.T) {
	cfg := Config{InsecureLocalOnly: true}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	srv := httptest.NewServer(app.server.Handler)
	defer srv.Close()

	for _, method := range []string{"GET", "POST", "DELETE"} {
		req, _ := http.NewRequest(method, srv.URL+"/api/v2/links", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s /api/v2/links: %v", method, err)
		}
		resp.Body.Close()
		// Must be 404 (route unregistered), not 401/403 (auth failure).
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s /api/v2/links: %d want 404 (route must be unregistered)", method, resp.StatusCode)
		}
	}
}

// TestPA2a_LegacyLinkFileNotRead creates a malformed ~/.devremote/links.json
// with sentinel bytes, runs NewAppWithDeps, and verifies the file is
// completely untouched (bytes, mode, mtime, inode unchanged).
func TestPA2a_LegacyLinkFileNotRead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create ~/.devremote/links.json with sentinel bytes.
	linkDir := home + "/.devremote"
	if err := os.MkdirAll(linkDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	linkPath := linkDir + "/links.json"
	sentinel := []byte("PA2A-SENTINEL-DO-NOT-READ-OR-MUTATE\n")
	if err := os.WriteFile(linkPath, sentinel, 0644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	// Record pre-App file state with inode identity.
	preInfo, err := os.Stat(linkPath)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}
	preIno := inodeFromInfo(preInfo)
	cfg := Config{InsecureLocalOnly: true}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}

	// Verify file untouched after NewApp (bytes, size, mtime, mode, inode).
	postInfo, err := os.Stat(linkPath)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	postBytes, err := os.ReadFile(linkPath)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	postIno := inodeFromInfo(postInfo)
	if string(postBytes) != string(sentinel) {
		t.Fatalf("links.json bytes changed: got %q want %q", postBytes, sentinel)
	}
	if postInfo.Size() != preInfo.Size() {
		t.Fatalf("links.json size changed: %d -> %d", preInfo.Size(), postInfo.Size())
	}
	if !postInfo.ModTime().Equal(preInfo.ModTime()) {
		t.Fatalf("links.json mtime changed: %v -> %v", preInfo.ModTime(), postInfo.ModTime())
	}
	if postInfo.Mode() != preInfo.Mode() {
		t.Fatalf("links.json mode changed: %v -> %v", preInfo.Mode(), postInfo.Mode())
	}
	if postIno != preIno {
		t.Fatalf("links.json inode changed: %d -> %d (file was replaced)", preIno, postIno)
	}

	// Shutdown and verify file still untouched (bytes, size, mtime, mode, inode).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	app.Shutdown(ctx)
	postSDInfo, err := os.Stat(linkPath)
	if err != nil {
		t.Fatalf("stat after shutdown: %v", err)
	}
	postSDBytes, err := os.ReadFile(linkPath)
	if err != nil {
		t.Fatalf("read after shutdown: %v", err)
	}
	postSDIno := inodeFromInfo(postSDInfo)
	if string(postSDBytes) != string(sentinel) {
		t.Fatalf("links.json modified during shutdown: got %q want %q", postSDBytes, sentinel)
	}
	if postSDInfo.Size() != preInfo.Size() {
		t.Fatalf("links.json size changed during shutdown: %d -> %d", preInfo.Size(), postSDInfo.Size())
	}
	if !postSDInfo.ModTime().Equal(preInfo.ModTime()) {
		t.Fatalf("links.json mtime changed during shutdown: %v -> %v", preInfo.ModTime(), postSDInfo.ModTime())
	}
	if postSDInfo.Mode() != preInfo.Mode() {
		t.Fatalf("links.json mode changed during shutdown: %v -> %v", preInfo.Mode(), postSDInfo.Mode())
	}
	if postSDIno != preIno {
		t.Fatalf("links.json inode changed during shutdown: %d -> %d", preIno, postSDIno)
	}
}

// TestPA2a_CLILinkCommandsRemoved verifies that link, unlink, and
// links are no longer valid subcommands.
func TestPA2a_CLILinkCommandsRemoved(t *testing.T) {
	for _, cmd := range []string{"link", "unlink", "links"} {
		if validSubcommands[cmd] {
			t.Errorf("%q still in validSubcommands", cmd)
		}
		if dispatchSubcommand(cmd, nil) {
			t.Errorf("dispatchSubcommand(%q) returned true", cmd)
		}
	}
	for _, cmd := range []string{"xyz", "unknown-cmd"} {
		if dispatchSubcommand(cmd, nil) {
			t.Errorf("dispatchSubcommand(%q) returned true", cmd)
		}
	}
	if !validSubcommands["daemon"] || !validSubcommands["run"] || !validSubcommands["pair"] {
		t.Error("known subcommand missing from validSubcommands")
	}
}

// inodeFromInfo extracts the inode from FileInfo. Returns 0 on platforms
// where the inode is not accessible through the os package.
func inodeFromInfo(fi os.FileInfo) uint64 {
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		return stat.Ino
	}
	return 0
}

func listenFreeTCPOnAddr(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}
