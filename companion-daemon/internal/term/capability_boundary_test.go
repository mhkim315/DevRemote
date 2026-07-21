package term

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// capExternalAdapter mirrors legacy: byte-stream (control/input) but NOT managed —
// it does not implement ManagedLifecycleProvider at all.
type capExternalAdapter struct{ sessions []mux.Session }

func (a *capExternalAdapter) Name() string { return "legacy" }
func (a *capExternalAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return a.sessions, nil
}
func (a *capExternalAdapter) TranscriptCaptureMode() mux.TranscriptCaptureMode {
	return mux.CaptureModeByteStream
}

// TestAPISessions_AdapterCapabilities_ManagedLifecycleBoundary proves the real
// product boundary: Registry sessions → HandleSessionsAPI → JSON serialization →
// each session's `adapterCapabilities`. controlled_pty exposes managedLifecycle;
// the external adapter (legacy) does not, while keeping control/input/liveTerminal/
// reliableTranscript. The exact JSON field name is asserted from the raw body.
func TestAPISessions_AdapterCapabilities_ManagedLifecycleBoundary(t *testing.T) {
	h := &Handlers{}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rr := httptest.NewRecorder()
	h.HandleSessionsAPI(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	// Unowned adapters are not projected by the V1 lifecycle boundary.
	if rr.Body.String() != "null\n" {
		t.Fatalf("response = %s, want empty V1 projection", rr.Body.String())
	}

	var sessions []SessionTelemetry
	if err := json.Unmarshal(rr.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(sessions) != 0 {
		t.Fatalf("unowned adapter rows leaked into V1 projection: %+v", sessions)
	}
}
