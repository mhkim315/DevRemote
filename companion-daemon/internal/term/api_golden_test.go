package term

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"devremote/companion-daemon/internal/mux"
)

// Phase 0 API golden tests: lock current HTTP response schemas.
// Phase 1 will change create to return canonical ID; these tests
// capture the CURRENT (accidental) local-ID behavior as baseline.

func TestAPIGolden_GetSessions(t *testing.T) {
	h := goldenHandlers(t)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()

	h.HandleSessionsAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions: status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var sessions []SessionTelemetry
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("GET /api/sessions: invalid JSON: %v", err)
	}

	// Must contain our golden session.
	found := false
	for _, s := range sessions {
		if s.ID == "tmux:golden" {
			found = true
			if s.Adapter != "tmux" {
				t.Errorf("adapter = %q, want tmux", s.Adapter)
			}
		}
	}
	if !found {
		t.Error("GET /api/sessions: golden session 'tmux:golden' not found")
	}
}

func TestAPIGolden_PostCreateSession_ReturnsLocalID(t *testing.T) {
	// Phase 0 baseline: POST create returns LOCAL ID (not canonical).
	// This is accidental behavior. Phase 1 will change to canonical.
	h := goldenHandlers(t)

	body := strings.NewReader(`{"id":"tmux:test","runner":"claude","runnerColor":"#58a6ff"}`)
	req := httptest.NewRequest("POST", "/api/sessions", body)
	rec := httptest.NewRecorder()

	h.HandleSessionsAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/sessions: status = %d, want 200", rec.Code)
	}

	var resp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("POST /api/sessions: invalid JSON: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want ok", resp.Status)
	}
	// CURRENT BEHAVIOR: returns local ID "test", not canonical "tmux:test".
	// This is accidental. Phase 1 will change this to "tmux:test".
	if resp.ID != "test" {
		t.Errorf("id = %q, want \"test\" (current local-ID behavior; Phase 1 will canonicalize)", resp.ID)
	}
}

func TestAPIGolden_DeleteSession(t *testing.T) {
	h := goldenHandlers(t)

	req := httptest.NewRequest("DELETE", "/api/sessions?id=tmux:golden", nil)
	rec := httptest.NewRecorder()

	h.HandleSessionsAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/sessions: status = %d, want 200", rec.Code)
	}

	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("DELETE /api/sessions: invalid JSON: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want ok", resp.Status)
	}
}

func TestAPIGolden_GetSessionsHistory(t *testing.T) {
	h := goldenHandlers(t)
	req := httptest.NewRequest("GET", "/api/sessions?history=tmux:golden", nil)
	rec := httptest.NewRecorder()

	h.HandleSessionsAPI(rec, req)

	// History for a session without events returns 404.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/sessions?history=tmux:golden: status = %d, want 404", rec.Code)
	}
}

// goldenHandlers returns a Handlers wired with a fake Registry containing
// one static tmux session. No real tmux process is needed.
func goldenHandlers(t *testing.T) *Handlers {
	t.Helper()

	reg := mux.NewRegistry(&goldenAdapter{
		sessions: []mux.Session{&goldenSession{}},
	})

	return &Handlers{
		Registry: reg,
		Events:   NewMemoryEventStore(),
	}
}

// Inline test types for golden API tests.

type goldenSession struct{}

func (s *goldenSession) ID() string          { return "golden" }
func (s *goldenSession) Title() string       { return "Golden Session" }
func (s *goldenSession) AdapterName() string { return "tmux" }

type goldenAdapter struct {
	sessions []mux.Session
}

func (a *goldenAdapter) Name() string                         { return "tmux" }
func (a *goldenAdapter) ListSessions() ([]mux.Session, error) { return a.sessions, nil }
func (a *goldenAdapter) GetSession(id string) (mux.Session, error) {
	for _, s := range a.sessions {
		if s.ID() == id {
			return s, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

// SessionCreator/SessionTerminator for Phase 0 golden POST/DELETE tests.
func (a *goldenAdapter) CreateSession(_ context.Context, opts mux.CreateOptions) (string, error) {
	return opts.Name, nil // returns local name (current accidental behavior)
}
func (a *goldenAdapter) TerminateSession(_ context.Context, id string) error {
	return nil
}

var _ mux.Adapter = (*goldenAdapter)(nil) // compile-time check
