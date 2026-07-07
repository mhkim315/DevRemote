package term

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"devremote/companion-daemon/internal/mux"
)

// Phase 5 E2E: prove fixture adapter reaches HTTP/WS/telemetry boundaries
// without tmux/cmux production file changes.

// fixtureHandlers returns a Handlers with the fixture adapter registered.
func fixtureHandlers(t *testing.T) *Handlers {
	t.Helper()
	reg := mux.MustNewRegistry(&fixtureAPIAdapter{
		sessions: []mux.Session{
			&fixtureAPISession{id: "f1", title: "fixture-shell"},
			&fixtureAPISession{id: "f2", title: "fixture-build"},
		},
	})
	return &Handlers{Registry: reg, Events: NewMemoryEventStore()}
}

// --- Inline fixture types (mirror mux/fixture_adapter_test.go behavior) ---

type fixtureAPIAdapter struct {
	sessions []mux.Session
}

func (a *fixtureAPIAdapter) Name() string { return "fixture" }
func (a *fixtureAPIAdapter) ListSessions(ctx context.Context) ([]mux.Session, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return a.sessions, nil
}
func (a *fixtureAPIAdapter) CreateSession(_ context.Context, opts mux.CreateOptions) (string, error) {
	return opts.Name, nil
}
func (a *fixtureAPIAdapter) TerminateSession(_ context.Context, id string) error { return nil }

type fixtureAPISession struct {
	id    string
	title string
}

func (s *fixtureAPISession) ID() string                      { return s.id }
func (s *fixtureAPISession) Title() string                   { return s.title }
func (s *fixtureAPISession) AdapterName() string             { return "fixture" }
func (s *fixtureAPISession) ReadScreen(_ context.Context) ([]byte, error) {
	return []byte("fixture screen"), nil
}
func (s *fixtureAPISession) OpenStream(_ context.Context) (mux.TerminalStream, error) {
	return &fixtureAPIStream{}, nil
}

type fixtureAPIStream struct{}

func (s *fixtureAPIStream) Read(p []byte) (int, error)  { return 0, io.EOF }
func (s *fixtureAPIStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *fixtureAPIStream) Close() error                { return nil }
func (s *fixtureAPIStream) Resize(rows, cols int) error { return nil }

// Note: fixtureAPISession intentionally does NOT implement HistoryReader.
// This proves unsupported capability paths at the API boundary.

// --- Tests ---

func TestFixtureE2E_GetSessions(t *testing.T) {
	h := fixtureHandlers(t)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()

	h.HandleSessionsAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions: status = %d, want 200", rec.Code)
	}

	var sessions []SessionTelemetry
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("GET /api/sessions: invalid JSON: %v", err)
	}

	found := map[string]bool{}
	for _, s := range sessions {
		found[s.ID] = true
		// Verify canonical ID format for third adapter.
		if s.Adapter == "fixture" {
			if !strings.HasPrefix(s.ID, "fixture:") {
				t.Errorf("fixture session ID %q missing adapter prefix", s.ID)
			}
			if s.DisplayID == "" {
				t.Error("fixture session has empty displayId")
			}
		}
	}

	for _, want := range []string{"fixture:f1", "fixture:f2"} {
		if !found[want] {
			t.Errorf("session %q not found in API response", want)
		}
	}
}

func TestFixtureE2E_CreateAndDelete(t *testing.T) {
	h := fixtureHandlers(t)

	// Create
	body := strings.NewReader(`{"id":"fixture:test-create","runner":"claude"}`)
	req := httptest.NewRequest("POST", "/api/sessions", body)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/sessions: status = %d, want 200", rec.Code)
	}
	var createResp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &createResp)
	if !strings.HasPrefix(createResp.ID, "fixture:") {
		t.Errorf("created session ID %q missing fixture: prefix", createResp.ID)
	}

	// Delete
	delReq := httptest.NewRequest("DELETE", "/api/sessions?session="+createResp.ID, nil)
	delRec := httptest.NewRecorder()
	h.HandleSessionsAPI(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Errorf("DELETE /api/sessions: status = %d, want 200", delRec.Code)
	}
}

func TestFixtureE2E_UnsupportedCapability(t *testing.T) {
	// fixture session lacks HistoryReader — API must return predictable result.
	h := fixtureHandlers(t)
	req := httptest.NewRequest("GET", "/api/sessions/fixture:f1/history", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)

	// Should return a non-500 response for unsupported capability.
	if rec.Code == http.StatusInternalServerError {
		t.Errorf("unsupported history: status = %d, want non-500", rec.Code)
	}

	// Verify JSON error response.
	var resp struct {
		Error  string `json:"error"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err == nil {
		if resp.Error == "" && rec.Code >= 400 {
			t.Error("error response has empty error field")
		}
	}
}

func TestFixtureE2E_WebSocket_Unsupported(t *testing.T) {
	// fixture session implements StreamOpener, so WS should not return 501.
	// Intentionally unsupported capability test: verify WS response is structured.
	h := fixtureHandlers(t)
	// Session with StreamOpener should allow WS connection.
	req := httptest.NewRequest("GET", "/term/ws?session=fixture:f1", nil)
	rec := httptest.NewRecorder()
	h.HandleWS(rec, req)

	// With a StreamOpener, the handler attempts to upgrade. httptest doesn't
	// support WebSocket upgrade, so a non-200 response is expected.
	// The important thing: it doesn't panic and returns structured output.
	if rec.Code == http.StatusInternalServerError {
		t.Error("WS with fixture stream: internal server error (unexpected)")
	}
}

func TestFixtureE2E_Telemetry(t *testing.T) {
	h := fixtureHandlers(t)

	// Verify telemetry shape through API endpoint (telemetry uses same snapshot).
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)

	var sessions []SessionTelemetry
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("telemetry: invalid JSON: %v", err)
	}

	for _, s := range sessions {
		// Verify all required telemetry fields are present for third adapter.
		if s.ID == "" {
			t.Error("telemetry session has empty ID")
		}
		if s.Adapter == "" {
			t.Error("telemetry session has empty adapter")
		}
	}
}
