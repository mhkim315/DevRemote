package term

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"devremote/companion-daemon/internal/mux"
)

// capBoundarySession is a minimal listed session for a given adapter.
type capBoundarySession struct {
	id      string
	adapter string
}

func (s *capBoundarySession) ID() string          { return s.id }
func (s *capBoundarySession) Title() string       { return s.id }
func (s *capBoundarySession) AdapterName() string { return s.adapter }

// capManagedAdapter mirrors controlled_pty: byte-stream + Pokit-managed.
type capManagedAdapter struct{ sessions []mux.Session }

func (a *capManagedAdapter) Name() string { return "controlled_pty" }
func (a *capManagedAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return a.sessions, nil
}
func (a *capManagedAdapter) TranscriptCaptureMode() mux.TranscriptCaptureMode {
	return mux.CaptureModeByteStream
}
func (a *capManagedAdapter) ManagedLifecycle() bool { return true }

// capExternalAdapter mirrors tmux: byte-stream (control/input) but NOT managed —
// it does not implement ManagedLifecycleProvider at all.
type capExternalAdapter struct{ sessions []mux.Session }

func (a *capExternalAdapter) Name() string { return "tmux" }
func (a *capExternalAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return a.sessions, nil
}
func (a *capExternalAdapter) TranscriptCaptureMode() mux.TranscriptCaptureMode {
	return mux.CaptureModeByteStream
}

// TestAPISessions_AdapterCapabilities_ManagedLifecycleBoundary proves the real
// product boundary: Registry sessions → HandleSessionsAPI → JSON serialization →
// each session's `adapterCapabilities`. controlled_pty exposes managedLifecycle;
// the external adapter (tmux) does not, while keeping control/input/liveTerminal/
// reliableTranscript. The exact JSON field name is asserted from the raw body.
func TestAPISessions_AdapterCapabilities_ManagedLifecycleBoundary(t *testing.T) {
	reg := mux.MustNewRegistry(
		&capManagedAdapter{sessions: []mux.Session{&capBoundarySession{id: "cp1", adapter: "controlled_pty"}}},
		&capExternalAdapter{sessions: []mux.Session{&capBoundarySession{id: "tm1", adapter: "tmux"}}},
	)
	h := &Handlers{Registry: reg, Events: nil}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rr := httptest.NewRecorder()
	h.HandleSessionsAPI(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	// Exact JSON field name must be adapterCapabilities.
	if !strings.Contains(rr.Body.String(), `"adapterCapabilities"`) {
		t.Fatalf("response missing adapterCapabilities field: %s", rr.Body.String())
	}

	var sessions []SessionTelemetry
	if err := json.Unmarshal(rr.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	byAdapter := map[string][]string{}
	for _, s := range sessions {
		byAdapter[s.Adapter] = s.AdapterCapabilities
	}

	cp, ok := byAdapter["controlled_pty"]
	if !ok {
		t.Fatalf("controlled_pty session not present in /api/sessions: %+v", byAdapter)
	}
	if !slices.Contains(cp, "managedLifecycle") {
		t.Fatalf("controlled_pty adapterCapabilities missing managedLifecycle: %v", cp)
	}

	tm, ok := byAdapter["tmux"]
	if !ok {
		t.Fatalf("tmux session not present in /api/sessions: %+v", byAdapter)
	}
	if slices.Contains(tm, "managedLifecycle") {
		t.Fatalf("tmux adapterCapabilities must not include managedLifecycle: %v", tm)
	}
	for _, want := range []string{"control", "input", "liveTerminal", "reliableTranscript"} {
		if !slices.Contains(tm, want) {
			t.Fatalf("tmux adapterCapabilities missing %q: %v", want, tm)
		}
	}
}
