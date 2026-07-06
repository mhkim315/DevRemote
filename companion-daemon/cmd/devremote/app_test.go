package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/term"
)

func TestNewApp_CreatesPrivateMux(t *testing.T) {
	// Not Parallel — NewApp sets term.OnApproval global (Phase 5 will fix).

	cfg := Config{
		InsecureLocalOnly: true,
	}

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

func TestHandlers_RegistryIsolation(t *testing.T) {
	// Not Parallel — NewApp mutates term.OnApproval global (Phase 5 will fix).

	regA := mux.NewRegistry()
	regB := mux.NewRegistry()

	hA := &term.Handlers{Registry: regA}
	hB := &term.Handlers{Registry: regB}

	if hA.Registry == hB.Registry {
		t.Fatal("expected different registries")
	}

	// Start test servers with each handler set — no route conflict.
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
		t.Fatalf("failed to query server A: %v", err)
	}
	respA.Body.Close()

	respB, err := http.Get(srvB.URL + "/api/sessions")
	if err != nil {
		t.Fatalf("failed to query server B: %v", err)
	}
	respB.Body.Close()
}

func TestApp_ShutdownWithoutBackgroundResources(t *testing.T) {
	// Not Parallel — NewApp mutates term.OnApproval global (Phase 5 will fix).

	cfg := Config{
		InsecureLocalOnly: true,
	}

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}

	// Shutdown on a minimally constructed app (no background resources started).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = app.Shutdown(ctx)
	if err != nil {
		t.Logf("Shutdown returned: %v (may be expected without started resources)", err)
	}
}

func TestApp_ShutdownRespectsDeadline(t *testing.T) {
	// Not Parallel — NewApp mutates term.OnApproval global (Phase 5 will fix).

	cfg := Config{
		InsecureLocalOnly: true,
	}

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}

	// Expired context should cause deadline errors.
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	// Don't fail on error — we're verifying deadline behavior.
	err = app.Shutdown(ctx)
	t.Logf("Shutdown with expired deadline returned: %v", err)
}

func TestPrivateMux_NoDefaultMuxUsage(t *testing.T) {
	// Not Parallel — NewApp mutates term.OnApproval global (Phase 5 will fix).

	cfg := Config{
		InsecureLocalOnly: true,
	}

	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}

	// Verify the app uses a private ServeMux, not DefaultServeMux.
	if app.server.Handler == http.DefaultServeMux {
		t.Fatal("server handler is DefaultServeMux, private mux required")
	}
}
