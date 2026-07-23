package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/notification"
	"devremote/companion-daemon/internal/term"
)

// These fixtures exercise App's production composition seams. They do not
// emulate a registry, adapter, or legacy launcher.
type appV1Watcher struct {
	mu       sync.Mutex
	closeErr error
	closed   bool
}

func (w *appV1Watcher) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return w.closeErr
}

func (w *appV1Watcher) wasClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

type appV1IPC struct {
	mu        sync.Mutex
	closed    bool
	blockWait bool
	waitErr   error
}

type appV1Tunnel struct{ done chan struct{} }

func (t *appV1Tunnel) Done() <-chan struct{} { return t.done }
func (*appV1Tunnel) Signal(os.Signal) error  { return nil }

type appV1RejectingVerifier struct{ called bool }

func (v *appV1RejectingVerifier) Verify(context.Context, string) error {
	v.called = true
	return errors.New("injected verifier rejection")
}

type appV1CodexLauncher struct{}

func (appV1CodexLauncher) Launch(string, []string) (term.ManagedProcess, error) {
	return appV1CodexProcess{}, nil
}

type appV1CodexProcess struct{}

func (appV1CodexProcess) Stdin() io.Writer  { return io.Discard }
func (appV1CodexProcess) Stdout() io.Reader { return strings.NewReader("") }
func (appV1CodexProcess) Term() error       { return nil }
func (appV1CodexProcess) Kill() error       { return nil }
func (appV1CodexProcess) Wait() error       { return nil }
func (appV1CodexProcess) OpaqueID() string  { return "app-v1-test" }
func (appV1CodexProcess) PID() int          { return 1 }

func (s *appV1IPC) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *appV1IPC) Wait(ctx context.Context) error {
	if s.blockWait {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.waitErr
}

func (s *appV1IPC) wasClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func v1LifecycleDeps(w watcherResource, ipc ipcResource) Dependencies {
	return Dependencies{
		Cmds: term.NewCommandBroker(),
		StartWatcher: func() (watcherResource, error) {
			return w, nil
		},
		StartIPC: func(string, *term.TelemetryService, *term.LifecycleService) (ipcResource, error) {
			return ipc, nil
		},
	}
}

func reservedLoopbackAddr(t *testing.T) (string, net.Listener) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return l.Addr().String(), l
}

func TestAppV1_RunPropagatesServeFailureAndCleansStartedResources(t *testing.T) {
	addr, blocker := reservedLoopbackAddr(t)
	defer blocker.Close()
	watcher := &appV1Watcher{}
	ipc := &appV1IPC{}
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: true}, v1LifecycleDeps(watcher, ipc))
	if err != nil {
		t.Fatal(err)
	}
	app.watcher = watcher
	app.ipc = ipc
	app.server.Addr = addr

	err = app.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("Run error = %v, want bind failure", err)
	}
	if !watcher.wasClosed() || !ipc.wasClosed() {
		t.Fatalf("started resources were not cleaned up: watcher=%v ipc=%v", watcher.wasClosed(), ipc.wasClosed())
	}
}

func TestAppV1_RunJoinsServeAndShutdownFailures(t *testing.T) {
	addr, blocker := reservedLoopbackAddr(t)
	defer blocker.Close()
	shutdownErr := errors.New("ipc wait failed")
	watcher := &appV1Watcher{}
	ipc := &appV1IPC{waitErr: shutdownErr}
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: true}, v1LifecycleDeps(watcher, ipc))
	if err != nil {
		t.Fatal(err)
	}
	app.server.Addr = addr
	err = app.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "address already in use") || !errors.Is(err, shutdownErr) {
		t.Fatalf("Run error = %v, want joined serve and shutdown failures", err)
	}
}

func TestAppV1_CompositionUsesPrivateHTTPMux(t *testing.T) {
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: true}, testDeps())
	if err != nil {
		t.Fatal(err)
	}
	if app.server == nil || app.server.Handler == nil {
		t.Fatal("V1 composition did not create an HTTP server and handler")
	}
	if app.server.Handler == http.DefaultServeMux {
		t.Fatal("V1 composition must not expose the process-global HTTP mux")
	}
}

func TestAppV1_HandlerRuntimeIsolation(t *testing.T) {
	ownedA := term.NewOwnedPTYRuntime(nil, nil)
	ownedB := term.NewOwnedPTYRuntime(nil, nil)
	ownedA.RegisterForTest("controlled_pty:only-a", "", "A", nil)
	ownedB.RegisterForTest("controlled_pty:only-b", "", "B", nil)
	verifier := term.NewSupabaseVerifier(term.AuthConfig{InsecureLocalOnly: true})
	hA := &term.Handlers{Verifier: verifier, Lifecycle: term.NewLifecycleService(ownedA, nil)}
	hB := &term.Handlers{Verifier: verifier, Lifecycle: term.NewLifecycleService(ownedB, nil)}

	for name, tc := range map[string]struct {
		handler *term.Handlers
		own     string
		foreign string
	}{
		"A": {hA, "only-a", "only-b"},
		"B": {hB, "only-b", "only-a"},
	} {
		t.Run(name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			tc.handler.HandleSessionsAPI(rr, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d", rr.Code)
			}
			body := rr.Body.String()
			if !strings.Contains(body, tc.own) || strings.Contains(body, tc.foreign) {
				t.Fatalf("runtime isolation failed: %s", body)
			}
		})
	}
}

func TestAppV1_InjectedVerifierGuardsComposedRoutes(t *testing.T) {
	verifier := &appV1RejectingVerifier{}
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: true}, Dependencies{Verifier: verifier, Cmds: term.NewCommandBroker()})
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	app.server.Handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if rr.Code != http.StatusUnauthorized || !verifier.called {
		t.Fatalf("composed auth result: status=%d verifierCalled=%v", rr.Code, verifier.called)
	}
}

func TestAppV1_PushNotifierConcurrentAccess(t *testing.T) {
	devices := notification.NewDeviceStore()
	notifier := &pushNotifier{send: func(string, string, string) {}, devices: devices}
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(2)
		go func() { defer wg.Done(); devices.Bind("device", "device-token") }()
		go func() { defer wg.Done(); _ = notifier.ApprovalRequired(context.Background(), "s", "") }()
	}
	wg.Wait()
}

func TestAppV1_ManagedCatalogIsCompositionOwned(t *testing.T) {
	managed := term.NewManagedCodexService(term.CodexAppServerEntryConfig{
		Bin: "/pinned/toolchain/node_modules/.bin/codex", Version: "codex-cli 0.144.1", AuthorityVersion: "0.144.1",
	}, appV1CodexLauncher{})
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: true, EnableManagedCodex: true}, Dependencies{
		Cmds: term.NewCommandBroker(), Managed: managed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if app.handlers == nil || app.handlers.Catalog == nil || app.handlers.RuntimeOf == nil {
		t.Fatal("managed catalog and resolver were not installed by composition")
	}
	if _, ok := app.handlers.RuntimeOf("codex_app_server:unknown"); ok {
		t.Fatal("catalog resolver accepted an unknown runtime")
	}
}

func TestAppV1_RunCleansPartialStartWhenIPCUnavailable(t *testing.T) {
	watcher := &appV1Watcher{}
	ipcErr := errors.New("ipc unavailable")
	deps := Dependencies{
		Cmds:         term.NewCommandBroker(),
		StartWatcher: func() (watcherResource, error) { return watcher, nil },
		StartIPC: func(string, *term.TelemetryService, *term.LifecycleService) (ipcResource, error) {
			return nil, ipcErr
		},
	}
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: true}, deps)
	if err != nil {
		t.Fatal(err)
	}
	addr, listener := reservedLoopbackAddr(t)
	listener.Close()
	app.server.Addr = addr
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Run after cancellation = %v, want clean partial-start shutdown", err)
	}
	if !watcher.wasClosed() {
		t.Fatal("watcher from partial start was not closed")
	}
}

func TestAppV1_TunnelStartupRespectsMode(t *testing.T) {
	for _, tc := range []struct {
		name      string
		insecure  bool
		wantStart bool
	}{
		{name: "local_only", insecure: true, wantStart: false},
		{name: "remote", insecure: false, wantStart: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := false
			watcher := &appV1Watcher{}
			ipc := &appV1IPC{}
			deps := v1LifecycleDeps(watcher, ipc)
			deps.StartTunnel = func() tunnelResource {
				started = true
				done := make(chan struct{})
				close(done)
				return &appV1Tunnel{done: done}
			}
			app, err := NewAppWithDeps(Config{InsecureLocalOnly: tc.insecure}, deps)
			if err != nil {
				t.Fatal(err)
			}
			addr, listener := reservedLoopbackAddr(t)
			listener.Close()
			app.server.Addr = addr
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := app.Run(ctx); err != nil {
				t.Fatal(err)
			}
			if started != tc.wantStart {
				t.Fatalf("tunnel started=%v, want %v", started, tc.wantStart)
			}
		})
	}
}

func TestAppV1_ShutdownIsBoundedAndContinuesAfterFailure(t *testing.T) {
	watcherErr := errors.New("watcher close failed")
	watcher := &appV1Watcher{closeErr: watcherErr}
	ipc := &appV1IPC{blockWait: true}
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: true}, v1LifecycleDeps(watcher, ipc))
	if err != nil {
		t.Fatal(err)
	}
	app.watcher = watcher
	app.ipc = ipc
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = app.Shutdown(ctx)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Shutdown exceeded bounded deadline: %v", elapsed)
	}
	if !errors.Is(err, watcherErr) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want watcher and deadline errors", err)
	}
	if !ipc.wasClosed() {
		t.Fatal("IPC close was skipped after watcher failure")
	}
}

func TestAppV1_ShutdownRebindsIPCOnRepeatedFreshCycles(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pb-v1-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "daemon.sock")
	for cycle := 0; cycle < 2; cycle++ {
		app, err := NewAppWithDeps(Config{InsecureLocalOnly: true}, testDeps())
		if err != nil {
			t.Fatal(err)
		}
		app.ipcPath = path
		ipc, err := term.StartIPCServer(path, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("cycle %d start IPC: %v", cycle, err)
		}
		app.ipc = ipc
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = app.Shutdown(ctx)
		cancel()
		if err != nil {
			t.Fatalf("cycle %d shutdown: %v", cycle, err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cycle %d socket remains: %v", cycle, err)
		}
	}
}
