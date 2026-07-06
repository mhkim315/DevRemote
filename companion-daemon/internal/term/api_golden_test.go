package term

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"devremote/companion-daemon/internal/models"
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

	// Verify raw JSON keys. Missing/renamed keys would still produce zero values
	// after Go unmarshal, so we check raw body for expected field names.
	raw := rec.Body.String()
	requiredKeys := []string{`"id"`, `"state"`, `"load"`, `"runner"`, `"runnerColor"`, `"adapter"`, `"events"`}
	for _, k := range requiredKeys {
		if !strings.Contains(raw, k) {
			t.Errorf("GET /api/sessions: JSON missing key %s", k)
		}
	}
	// Optional omitempty fields: stale, lastSuccessAt, lastError — present only when non-zero.
	// We verify they don't appear unexpectedly for a healthy session.

	var sessions []SessionTelemetry
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("GET /api/sessions: invalid JSON: %v", err)
	}

	found := false
	for _, s := range sessions {
		if s.ID == "tmux:golden" {
			found = true
			if s.Adapter != "tmux" {
				t.Errorf("adapter = %q, want tmux", s.Adapter)
			}
			if s.State == "" {
				t.Error("state is empty")
			}
			// Healthy session without lastError should not have stale=true.
			if s.Stale {
				t.Error("stale is true for healthy golden session")
			}
		}
	}
	if !found {
		t.Error("GET /api/sessions: golden session 'tmux:golden' not found")
	}
}

func TestAPIGolden_PostCreateSession_ReturnsCanonicalID(t *testing.T) {
	// Phase 1: POST create returns CANONICAL ID (<adapter>:<local-id>).
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
	// Phase 1: canonical ID includes adapter prefix.
	if resp.ID != "tmux:test" {
		t.Errorf("id = %q, want canonical \"tmux:test\"", resp.ID)
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

func TestAPIGolden_GetSessionsHistory_NoEvents(t *testing.T) {
	h := goldenHandlers(t)
	req := httptest.NewRequest("GET", "/api/sessions?history=tmux:golden", nil)
	rec := httptest.NewRecorder()

	h.HandleSessionsAPI(rec, req)

	// History for a session without events and without ScreenReader → 404.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/sessions?history=tmux:golden (no events): status = %d, want 404", rec.Code)
	}
}

func TestAPIGolden_GetSessionsHistory_ScreenFallback(t *testing.T) {
	// When a session implements ScreenReader, history falls back to screen content.
	h := goldenHandlersWithScreenReader(t)
	req := httptest.NewRequest("GET", "/api/sessions?history=tmux:golden", nil)
	rec := httptest.NewRecorder()

	h.HandleSessionsAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions?history=tmux:golden (screen fallback): status = %d, want 200", rec.Code)
	}
	raw := rec.Body.String()
	agentEventKeys := []string{`"id"`, `"session"`, `"type"`, `"summary"`, `"detail"`, `"timestamp"`, `"agent"`, `"toolCallId"`}
	for _, k := range agentEventKeys {
		if !strings.Contains(raw, k) {
			t.Errorf("history response missing AgentEvent key %s", k)
		}
	}
	if !strings.Contains(raw, "GOLDEN_SCREEN") {
		t.Errorf("history response missing screen content: %s", raw)
	}

	// Typed decode to verify value types, not just key presence.
	var events []models.AgentEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("history response: invalid AgentEvent JSON: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("history response: empty events array")
	}
	e := events[0]
	if e.ID == "" || e.Session == "" || e.Type == "" || e.Timestamp == "" {
		t.Errorf("history event has empty required field: id=%q session=%q type=%q ts=%q", e.ID, e.Session, e.Type, e.Timestamp)
	}
	if e.Detail == "" && e.Summary == "" {
		t.Error("history event has empty detail AND summary")
	}
}

// goldenHandlers returns a Handlers wired with a fake Registry containing
// one static tmux session. No real tmux process is needed.
func goldenHandlers(t *testing.T) *Handlers {
	t.Helper()

	reg := mux.MustNewRegistry(&goldenAdapter{
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

func (a *goldenAdapter) Name() string { return "tmux" }
func (a *goldenAdapter) ListSessions(ctx context.Context) ([]mux.Session, error) {
	return a.sessions, nil
}
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

// goldenHandlersWithScreenReader returns handlers where the session implements ScreenReader.
func goldenHandlersWithScreenReader(t *testing.T) *Handlers {
	t.Helper()
	sess := &goldenScreenSession{}
	reg := mux.MustNewRegistry(&goldenScreenAdapter{sessions: []mux.Session{sess}})
	return &Handlers{Registry: reg, Events: NewMemoryEventStore()}
}

type goldenScreenSession struct{ goldenSession }

func (s *goldenScreenSession) ReadScreen(_ context.Context) ([]byte, error) {
	return []byte("GOLDEN_SCREEN_content"), nil
}

type goldenScreenAdapter struct {
	sessions []mux.Session
}

func (a *goldenScreenAdapter) Name() string { return "tmux" }
func (a *goldenScreenAdapter) ListSessions(ctx context.Context) ([]mux.Session, error) {
	return a.sessions, nil
}
func (a *goldenScreenAdapter) GetSession(id string) (mux.Session, error) {
	for _, s := range a.sessions {
		if s.ID() == id {
			return s, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

var _ mux.Adapter = (*goldenAdapter)(nil) // compile-time check
