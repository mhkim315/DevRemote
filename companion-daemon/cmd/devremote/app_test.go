package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/term"
)

// ── Fake resources ──

type fakeIPC struct {
	mu        sync.Mutex
	closeErr  error
	waitErr   error
	closeCnt  int
	closeOrdr *[]string
	name      string
	blocking  bool // if true, Wait blocks until ctx is done
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
	close(t.done) // already exited
	return t
}

// ── Tests ──

func TestNewApp_CreatesPrivateMux(t *testing.T) {
	// Not Parallel — NewApp sets term.OnApproval global (Phase 5 deferred).
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
	// Not Parallel — NewApp sets term.OnApproval global (Phase 5 deferred).
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
	term.InsecureLocalOnly = true

	regA := mux.NewRegistry()
	regB := mux.NewRegistry()

	adapterA := &mux.StaticAdapter{
		AdapterNameStr: "test-a",
		SessionsList: []mux.Session{
			&mux.StaticSession{IDStr: "session-a", TitleStr: "Session A", AdapterStr: "test-a"},
		},
	}
	adapterB := &mux.StaticAdapter{
		AdapterNameStr: "test-b",
		SessionsList: []mux.Session{
			&mux.StaticSession{IDStr: "session-b", TitleStr: "Session B", AdapterStr: "test-b"},
		},
	}
	regA.Register(adapterA)
	regB.Register(adapterB)

	hA := &term.Handlers{Registry: regA}
	hB := &term.Handlers{Registry: regB}

	if hA.Registry == hB.Registry {
		t.Fatal("expected different registries")
	}

	muxA := http.NewServeMux()
	muxA.HandleFunc("/api/sessions", term.AuthMiddleware(hA.HandleSessionsAPI))
	srvA := httptest.NewServer(muxA)
	defer srvA.Close()

	muxB := http.NewServeMux()
	muxB.HandleFunc("/api/sessions", term.AuthMiddleware(hB.HandleSessionsAPI))
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

	// Read all body bytes.
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
	// Verify resources are cleaned up in the correct order.
	term.InsecureLocalOnly = true

	var order []string

	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, Dependencies{
		StartWatcher: func() (watcherResource, error) {
			return &fakeWatcher{closeOrdr: &order, name: "watcher"}, nil
		},
		StartIPC: func(path string, reg *mux.Registry) (ipcResource, error) {
			return &fakeIPC{closeOrdr: &order, name: "ipc"}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}

	// Inject fakes directly so Shutdown has resources to clean up.
	app.watcher = &fakeWatcher{closeOrdr: &order, name: "watcher"}
	app.ipc = &fakeIPC{closeOrdr: &order, name: "ipc"}

	// Start telemetry with cancel so we can observe its stop order.
	tCtx, tCancel := context.WithCancel(context.Background())
	app.telemetryCancel = tCancel
	app.telemetryDone = term.StartTelemetryLoop(tCtx, app.registry)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = app.Shutdown(ctx)
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	// Expected order: telemetry cancel, watcher close, then ipc close/wait.
	// The order must satisfy: telemetry.cancel → watcher:close → ipc:close → ipc:wait.
	t.Logf("shutdown order: %v", order)

	if len(order) < 3 {
		t.Fatalf("expected at least 3 order entries (watcher:close, ipc:close, ipc:wait), got %d: %v", len(order), order)
	}

	// Verify watcher is closed before IPC.
	watcherIdx := indexOf(order, "watcher:close")
	ipcCloseIdx := indexOf(order, "ipc:close")
	ipcWaitIdx := indexOf(order, "ipc:wait")

	if watcherIdx < 0 {
		t.Error("watcher was not closed")
	}
	if ipcCloseIdx < 0 {
		t.Error("ipc was not closed")
	}
	if ipcWaitIdx < 0 {
		t.Error("ipc Wait was not called")
	}
	if watcherIdx >= 0 && ipcCloseIdx >= 0 && watcherIdx > ipcCloseIdx {
		t.Error("watcher closed after ipc close — order violation")
	}
	if ipcCloseIdx >= 0 && ipcWaitIdx >= 0 && ipcCloseIdx > ipcWaitIdx {
		t.Error("ipc close after ipc wait — order violation")
	}
}

func TestApp_ShutdownContinuesAfterError(t *testing.T) {
	// Verify that when one resource errors, subsequent resources are still cleaned up.
	term.InsecureLocalOnly = true

	var order []string

	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, Dependencies{
		StartWatcher: func() (watcherResource, error) {
			return &fakeWatcher{closeOrdr: &order, name: "watcher", closeErr: errors.New("watcher close failed")}, nil
		},
		StartIPC: func(path string, reg *mux.Registry) (ipcResource, error) {
			return &fakeIPC{closeOrdr: &order, name: "ipc"}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}
	app.watcher = &fakeWatcher{closeOrdr: &order, name: "watcher", closeErr: errors.New("watcher close failed")}
	app.ipc = &fakeIPC{closeOrdr: &order, name: "ipc"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdownErr := app.Shutdown(ctx)
	if shutdownErr == nil {
		t.Fatal("expected shutdown error from failing watcher, got nil")
	}
	if !strings.Contains(shutdownErr.Error(), "watcher close failed") {
		t.Errorf("error should mention watcher: %v", shutdownErr)
	}

	// IPC should still have been cleaned up despite watcher error.
	if idx := indexOf(order, "ipc:close"); idx < 0 {
		t.Error("ipc was not closed — cleanup continued after error")
	}
}

func TestApp_ShutdownRespectsDeadline(t *testing.T) {
	// Verify that a blocking IPC Wait respects the shutdown deadline.
	term.InsecureLocalOnly = true

	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, Dependencies{})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}

	// Inject an IPC that blocks forever on Wait.
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
	// Verify App-level IPC lifecycle: bind, shutdown, rebind.
	dir, err := os.MkdirTemp("", "p2")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)
	socketPath := filepath.Join(dir, "s")

	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, Dependencies{})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}
	app.ipcPath = socketPath

	// Start real IPC.
	srv, err := term.StartIPCServer(socketPath, app.registry)
	if err != nil {
		t.Fatalf("StartIPCServer failed: %v", err)
	}
	app.ipc = srv

	// Shutdown should remove the socket.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Errorf("socket still exists after Shutdown: %v", err)
	}

	// Rebind without manual Remove.
	srv2, err := term.StartIPCServer(socketPath, app.registry)
	if err != nil {
		t.Fatalf("rebind StartIPCServer failed: %v", err)
	}
	srv2.Close()
	srv2.Wait(ctx)
}

func TestApp_TunnelNotStartedInInsecureMode(t *testing.T) {
	// Insecure mode: Run must NOT start a tunnel.
	term.InsecureLocalOnly = true

	tunnelStarted := false
	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, Dependencies{
		StartWatcher: func() (watcherResource, error) { return &fakeWatcher{}, nil },
		StartIPC: func(path string, reg *mux.Registry) (ipcResource, error) {
			return &fakeIPC{waitErr: context.DeadlineExceeded}, nil
		},
		StartTunnel: func() tunnelResource {
			tunnelStarted = true
			return newFakeTunnelDone()
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}

	// Simulate what Run does WITHOUT calling Run (which would ListenAndServe).
	app.watcher = app.startWatcher()
	ipc, _ := app.startIPC()
	app.ipc = ipc
	if !app.config.InsecureLocalOnly {
		app.tunnel = app.startTunnel()
	}

	if tunnelStarted {
		t.Error("tunnel was started in insecure mode")
	}
	if app.tunnel != nil {
		t.Error("tunnel resource is non-nil in insecure mode")
	}
}

func TestApp_TunnelStartedInProductionMode(t *testing.T) {
	// Production mode: Run must start a tunnel.
	term.InsecureLocalOnly = true // for auth bypass

	tunnelStarted := false
	cfg := Config{InsecureLocalOnly: false}
	app, err := NewAppWithDeps(cfg, Dependencies{
		StartWatcher: func() (watcherResource, error) { return &fakeWatcher{}, nil },
		StartIPC: func(path string, reg *mux.Registry) (ipcResource, error) {
			return &fakeIPC{waitErr: context.DeadlineExceeded}, nil
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
		t.Error("tunnel was NOT started in production mode")
	}
	if app.tunnel == nil {
		t.Error("tunnel resource is nil in production mode")
	}
}

func TestApp_RunContextCancelReturnsNil(t *testing.T) {
	// Verify that context cancellation triggers clean shutdown returning nil.
	term.InsecureLocalOnly = true

	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, Dependencies{
		StartWatcher: func() (watcherResource, error) { return &fakeWatcher{}, nil },
		StartIPC: func(path string, reg *mux.Registry) (ipcResource, error) {
			return &fakeIPC{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}

	app.watcher = app.startWatcher()
	ipc, _ := app.startIPC()
	app.ipc = ipc

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runErr := app.Run(ctx)
	if runErr != nil {
		t.Errorf("expected nil after context cancel shutdown, got: %v", runErr)
	}
}

func TestApp_RunReturnsHTTPServeError(t *testing.T) {
	// Verify that a non-ErrServerClosed HTTP error is preserved by Run.
	// We keep a listener open on the port so ListenAndServe cannot bind.
	term.InsecureLocalOnly = true

	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, Dependencies{
		StartWatcher: func() (watcherResource, error) { return &fakeWatcher{}, nil },
		StartIPC: func(path string, reg *mux.Registry) (ipcResource, error) {
			return &fakeIPC{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}

	app.watcher = app.startWatcher()
	ipc, _ := app.startIPC()
	app.ipc = ipc

	// Keep a listener open to block the port.
	l, err := listenFreeTCP()
	if err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	defer l.Close()
	addr := l.Addr().String()
	app.server.Addr = addr

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runErr := app.Run(ctx)
	if runErr == nil {
		t.Error("expected non-nil error from Run (serve error or context deadline)")
	}
	t.Logf("Run error: %v", runErr)
}

func TestApp_RunJoinsServeAndShutdownErrors(t *testing.T) {
	// Verify errors.Join preserves both serve and shutdown errors.
	term.InsecureLocalOnly = true

	shutdownErr := errors.New("shutdown-failed")
	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, Dependencies{
		StartWatcher: func() (watcherResource, error) { return &fakeWatcher{}, nil },
		StartIPC: func(path string, reg *mux.Registry) (ipcResource, error) {
			// IPC that fails on Wait.
			return &fakeIPC{waitErr: shutdownErr}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}

	app.watcher = app.startWatcher()
	ipc, _ := app.startIPC()
	app.ipc = ipc

	l, err := listenFreeTCP()
	if err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	defer l.Close()
	addr := l.Addr().String()

	app.server.Addr = addr

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	runErr := app.Run(ctx)
	if runErr == nil {
		t.Fatal("expected joined error, got nil")
	}
	// shutdownErr should be detectable via errors.Is.
	if !errors.Is(runErr, shutdownErr) {
		t.Errorf("shutdown error %q not found in Run result: %v", shutdownErr, runErr)
	}
}

func TestApp_TunnelShutdownSignalsAndWaits(t *testing.T) {
	// Verify that Shutdown signals the tunnel and waits for Done().
	term.InsecureLocalOnly = true

	var order []string
	tun := &fakeTunnel{
		done:    make(chan struct{}),
		sigOrdr: &order,
		name:    "tunnel",
	}

	cfg := Config{InsecureLocalOnly: false}
	app, err := NewAppWithDeps(cfg, Dependencies{})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}
	app.tunnel = tun
	app.ipc = &fakeIPC{} // prevent nil panic

	// Shutdown in a goroutine — it will block on tunnel.Done().
	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		errCh <- app.Shutdown(ctx)
	}()

	// Give it time to signal.
	time.Sleep(100 * time.Millisecond)
	close(tun.done) // tunnel exits

	select {
	case shutdownErr := <-errCh:
		if shutdownErr != nil {
			t.Fatalf("Shutdown failed: %v", shutdownErr)
		}
		// Verify signal was called.
		if idx := indexOf(order, "tunnel:signal"); idx < 0 {
			t.Error("tunnel was not signalled")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Shutdown did not complete within deadline")
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

func listenFreeTCP() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}

// Ensure net import is used (referenced from helpers).
var _ = fmt.Sprintf
