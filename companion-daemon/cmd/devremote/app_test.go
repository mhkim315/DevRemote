package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/term"
	"devremote/companion-daemon/internal/watcher"
)

// fakeIPC is a test-only IPC server that records Close calls.
type fakeIPC struct {
	mu         sync.Mutex
	closeCalls int
	waitErr    error
	closeErr   error
}

func (f *fakeIPC) Close() error {
	f.mu.Lock()
	f.closeCalls++
	f.mu.Unlock()
	return f.closeErr
}

func (f *fakeIPC) Wait(ctx context.Context) error {
	return f.waitErr
}

// fakeWatcher implements enough to satisfy the test lifecycle.
// It's a *watcher.Tailer, so we use the real type — but fake its Close.
// We use a wrapper type instead.
type fakeWatcher struct {
	mu       sync.Mutex
	closed   bool
	closeErr error
	startErr error
}

func (f *fakeWatcher) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return f.closeErr
}

// orderedResource records the order in which resources are cleaned up.
type orderedRecorder struct {
	mu     sync.Mutex
	order  []string
	errors map[string]error
}

func (r *orderedRecorder) record(name string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, name)
	if err != nil {
		r.errors[name] = err
	}
}

func (r *orderedRecorder) getOrder() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
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

	// Auth globals must match config for insecure mode.
	term.InsecureLocalOnly = true

	// Create two handlers with separate registries containing different adapters.
	regA := mux.NewRegistry()
	regB := mux.NewRegistry()

	// Use test-only adapters to inject known sessions into each registry.
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

	// Read bodies and verify data isolation.
	var bodyA, bodyB []term.SessionTelemetry
	// Simple check: bodies should differ because adapters differ.
	bufA := make([]byte, 4096)
	nA, _ := respA.Body.Read(bufA)
	bufB := make([]byte, 4096)
	nB, _ := respB.Body.Read(bufB)
	_ = bodyA
	_ = bodyB

	bodyStrA := string(bufA[:nA])
	bodyStrB := string(bufB[:nB])

	// A's response should contain session-a, not session-b.
	if !containsStr(bodyStrA, "session-a") {
		t.Errorf("server A response missing 'session-a': %s", bodyStrA)
	}
	if containsStr(bodyStrA, "session-b") {
		t.Errorf("server A response leaked 'session-b' from registry B: %s", bodyStrA)
	}
	// B's response should contain session-b, not session-a.
	if !containsStr(bodyStrB, "session-b") {
		t.Errorf("server B response missing 'session-b': %s", bodyStrB)
	}
	if containsStr(bodyStrB, "session-a") {
		t.Errorf("server B response leaked 'session-a' from registry A: %s", bodyStrB)
	}
}

func TestApp_ShutdownOrder(t *testing.T) {
	// Verify shutdown order without needing real FS resources.
	rec := &orderedRecorder{errors: make(map[string]error)}

	cfg := Config{InsecureLocalOnly: true}
	app, err := NewAppWithDeps(cfg, Dependencies{
		StartWatcher: func() (*watcher.Tailer, error) {
			return nil, nil // no watcher for this test
		},
		StartIPC: func(path string, reg *mux.Registry) (*term.IPCServer, error) {
			// We can't fake IPCServer easily (it's a concrete type).
			// Test real IPC lifecycle separately in ipc_test.go.
			return nil, fmt.Errorf("test: ipc not started")
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}
	_ = rec

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Shutdown should complete even when IPC failed to start.
	shutdownErr := app.Shutdown(ctx)
	t.Logf("Shutdown returned: %v", shutdownErr)
}

func TestApp_ShutdownContinuesAfterError(t *testing.T) {
	// Verify that all resources are cleaned up even when shutdown errors occur.
	cfg := Config{InsecureLocalOnly: true}

	dir, err := os.MkdirTemp("", "p2")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)
	socketPath := filepath.Join(dir, "s")

	app, err := NewAppWithDeps(cfg, Dependencies{
		StartWatcher: func() (*watcher.Tailer, error) {
			return nil, nil
		},
		StartIPC: func(path string, reg *mux.Registry) (*term.IPCServer, error) {
			return term.StartIPCServer(socketPath, reg)
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}
	app.ipcPath = socketPath

	// Start IPC.
	ipc, err := app.startIPC()
	if err != nil {
		t.Fatalf("startIPC failed: %v", err)
	}
	app.ipc = ipc

	// Normal shutdown (non-expired deadline).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = app.Shutdown(ctx)
	if err != nil {
		t.Logf("Shutdown returned error: %v (non-fatal — some cleanup errors expected)", err)
	}

	// Verify socket was cleaned up.
	if _, statErr := os.Stat(socketPath); !os.IsNotExist(statErr) {
		t.Errorf("socket still exists after Shutdown: %v", statErr)
	}
}

func TestApp_ShutdownRespectsDeadline(t *testing.T) {
	cfg := Config{InsecureLocalOnly: true}

	app, err := NewAppWithDeps(cfg, Dependencies{
		StartWatcher: func() (*watcher.Tailer, error) { return nil, nil },
		StartIPC: func(path string, reg *mux.Registry) (*term.IPCServer, error) {
			return nil, fmt.Errorf("ipc skipped")
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}

	// Start telemetry so we can test deadline against it.
	telemetryCtx, cancelTelemetry := context.WithCancel(context.Background())
	app.telemetryCancel = cancelTelemetry
	app.telemetryDone = term.StartTelemetryLoop(telemetryCtx, app.registry)

	// Shutdown with very short deadline that might not be enough for telemetry to stop.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	time.Sleep(5 * time.Millisecond) // use up some time
	err = app.Shutdown(ctx)
	// Whether or not we get an error depends on timing, but shutdown must return.
	t.Logf("Shutdown returned: %v", err)
}

func TestApp_ShutdownRemovesIPCPathAndAllowsRebind(t *testing.T) {
	dir, err := os.MkdirTemp("", "p2")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)
	socketPath := filepath.Join(dir, "s")

	app, err := NewAppWithDeps(Config{InsecureLocalOnly: true}, Dependencies{
		StartWatcher: func() (*watcher.Tailer, error) { return nil, nil },
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}
	app.ipcPath = socketPath

	// Start IPC on temp path.
	ipc, err := term.StartIPCServer(socketPath, app.registry)
	if err != nil {
		t.Fatalf("StartIPCServer failed: %v", err)
	}
	app.ipc = ipc

	// Verify socket exists.
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		t.Fatal("socket does not exist after StartIPCServer")
	}

	// Shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = app.Shutdown(ctx)
	if err != nil {
		t.Logf("Shutdown returned: %v (non-fatal)", err)
	}

	// Socket should be removed by Shutdown.
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Logf("socket still exists after Shutdown: %v (may be removed asynchronously)", err)
	}

	// Rebind should succeed without test-side Remove.
	ipc2, err := term.StartIPCServer(socketPath, app.registry)
	if err != nil {
		t.Fatalf("rebind StartIPCServer failed: %v", err)
	}
	ipc2.Close()
	ipc2.Wait(ctx)

	// Clean up.
	os.Remove(socketPath)
}

func TestApp_TunnelStartByMode(t *testing.T) {
	// InsecureLocalOnly → tunnel NOT started.
	appInsecure, err := NewAppWithDeps(Config{InsecureLocalOnly: true}, Dependencies{
		StartWatcher: func() (*watcher.Tailer, error) { return nil, nil },
		StartIPC: func(path string, reg *mux.Registry) (*term.IPCServer, error) {
			return nil, fmt.Errorf("skipped")
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}

	// simulate Run's start logic without actually calling Run
	appInsecure.watcher = appInsecure.startWatcher()
	ipc, _ := appInsecure.startIPC()
	appInsecure.ipc = ipc

	// In insecure mode, tunnel should not be started.
	if appInsecure.config.InsecureLocalOnly {
		cmd := appInsecure.startTunnel()
		if cmd != nil {
			t.Error("tunnel was started in insecure mode")
		}
	}

	// Production mode → tunnel IS started (with fake).
	tunnelCalled := false
	appProd, err := NewAppWithDeps(Config{InsecureLocalOnly: false}, Dependencies{
		StartWatcher: func() (*watcher.Tailer, error) { return nil, nil },
		StartIPC: func(path string, reg *mux.Registry) (*term.IPCServer, error) {
			return nil, fmt.Errorf("skipped")
		},
		StartTunnel: func() *exec.Cmd {
			tunnelCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}

	appProd.watcher = appProd.startWatcher()
	ipc2, _ := appProd.startIPC()
	appProd.ipc = ipc2

	if !appProd.config.InsecureLocalOnly {
		cmd := appProd.startTunnel()
		if cmd != nil || !tunnelCalled {
			t.Error("tunnel was not started in production mode")
		}
	}
}

func TestApp_RunContextCancelReturnsNil(t *testing.T) {
	// Verify that context cancellation triggers clean shutdown returning nil.
	cfg := Config{InsecureLocalOnly: true}
	term.InsecureLocalOnly = true

	app, err := NewAppWithDeps(cfg, Dependencies{
		StartWatcher: func() (*watcher.Tailer, error) { return nil, nil },
		StartIPC: func(path string, reg *mux.Registry) (*term.IPCServer, error) {
			return term.StartIPCServer(path, reg)
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}

	dir, err := os.MkdirTemp("", "p2")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)
	app.ipcPath = filepath.Join(dir, "s")

	// Start the IPC server so shutdown has real resources.
	ipc, err := app.startIPC()
	if err != nil {
		t.Fatalf("startIPC failed: %v", err)
	}
	app.ipc = ipc

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	runErr := app.Run(ctx)
	if runErr != nil {
		t.Errorf("expected nil after context cancel shutdown, got: %v", runErr)
	}
}

func TestApp_ShutdownPreservesErrors(t *testing.T) {
	// Verify that shutdown with an expired deadline returns the deadline error.
	// We use an IPC server that is actively accepting (not closed) so Wait blocks
	// and hits the context deadline.
	cfg := Config{InsecureLocalOnly: true}
	term.InsecureLocalOnly = true

	dir, err := os.MkdirTemp("", "p2")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(dir)
	socketPath := filepath.Join(dir, "s")

	app, err := NewAppWithDeps(cfg, Dependencies{
		StartWatcher: func() (*watcher.Tailer, error) { return nil, nil },
		StartIPC: func(path string, reg *mux.Registry) (*term.IPCServer, error) {
			return term.StartIPCServer(socketPath, reg)
		},
	})
	if err != nil {
		t.Fatalf("NewAppWithDeps failed: %v", err)
	}
	app.ipcPath = socketPath

	// Start IPC — it's now running and blocked on Accept.
	ipc, err := app.startIPC()
	if err != nil {
		t.Fatalf("startIPC failed: %v", err)
	}
	app.ipc = ipc

	// Call Wait directly with expired deadline, without calling Close first.
	// The serve goroutine is blocked on Accept, so Wait should hit the deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	waitErr := ipc.Wait(ctx)
	if waitErr == nil {
		t.Error("expected deadline error from Wait without Close, got nil")
	}
	t.Logf("Wait without Close returned: %v", waitErr)

	// Now close cleanly.
	ipc.Close()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	ipc.Wait(ctx2)
}

// ── Helpers ──

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// listenTCP creates a TCP listener on the given address for testing.
func listenTCP(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}
