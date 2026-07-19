package term

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	if rec.Code != 200 {
		t.Fatalf("GET /api/sessions: status %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	// PA3 Step 2 R4: legacy keys must be ABSENT, required keys PRESENT.
	raw := rec.Body.String()
	for _, k := range []string{`"state"`, `"load"`, `"runner"`, `"runnerColor"`, `"events"`} {
		if strings.Contains(raw, k) {
			t.Errorf("GET /api/sessions: legacy key %s must be absent from JSON", k)
		}
	}
	for _, k := range []string{`"id"`, `"displayId"`, `"adapter"`, `"capabilities"`} {
		if !strings.Contains(raw, k) {
			t.Errorf("GET /api/sessions: required key %s must be present in JSON", k)
		}
	}

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
			if s.DisplayID != "golden" {
				t.Errorf("displayId = %q, want 'golden'", s.DisplayID)
			}
		}
	}
	if !found {
		t.Error("GET /api/sessions: did not find tmux:golden in response")
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
	// PA3 Step 3 R1: ?history= returns 410 Gone for ALL sessions.
	h := goldenHandlers(t)
	req := httptest.NewRequest("GET", "/api/sessions?history=tmux:golden", nil)
	rec := httptest.NewRecorder()

	h.HandleSessionsAPI(rec, req)

	// Golden session has ScreenReader capability → history falls back to screen content.
	if rec.Code != http.StatusGone {
		t.Fatalf("GET /api/sessions?history=tmux:golden: status = %d, want %d (Gone)", rec.Code, http.StatusGone)
	}
}

func TestAPIGolden_GetSessionsHistory_ScreenFallback(t *testing.T) {
	// PA3 Step 3 R1: ?history= returns 410 Gone even for screen-capable sessions.
	h := goldenHandlersWithScreenReader(t)
	req := httptest.NewRequest("GET", "/api/sessions?history=tmux:golden", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)
	if rec.Code != http.StatusGone {
		t.Fatalf("GET /api/sessions?history=tmux:golden (screen fallback): status = %d, want %d (Gone)", rec.Code, http.StatusGone)
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

func (s *goldenSession) ID() string                                     { return "golden" }
func (s *goldenSession) Title() string                                  { return "Golden Session" }
func (s *goldenSession) AdapterName() string                            { return "tmux" }
func (s *goldenSession) ReadScreen(ctx context.Context) ([]byte, error) { return []byte("screen"), nil }
func (s *goldenSession) OpenStream(ctx context.Context) (mux.TerminalStream, error) {
	return &goldenStream{}, nil
}

type goldenStream struct{}

func (s *goldenStream) Read(p []byte) (int, error)  { return 0, io.EOF }
func (s *goldenStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *goldenStream) Close() error                { return nil }
func (s *goldenStream) Resize(rows, cols int) error { return nil }

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

func TestAPIGolden_PostCreateCmuxSession_CanonicalID(t *testing.T) {
	// cmux create must return canonical ID via handler exactly once.
	reg := mux.MustNewRegistry(&cmuxCreateAdapter{})
	h := &Handlers{Registry: reg, Events: NewMemoryEventStore()}

	body := strings.NewReader(`{"id":"cmux:test","runner":"agent","runnerColor":"#58a6ff"}`)
	req := httptest.NewRequest("POST", "/api/sessions", body)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST cmux create: status = %d, want 200", rec.Code)
	}
	var resp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("POST cmux create: invalid JSON: %v", err)
	}
	// Handler must canonicalize exactly once: adapter "cmux" + ":" + local "surface:42" = "cmux:surface:42"
	if resp.ID != "cmux:surface:42" {
		t.Errorf("id = %q, want canonical 'cmux:surface:42' (not double-prefixed)", resp.ID)
	}
}

type cmuxCreateAdapter struct{}

func (a *cmuxCreateAdapter) Name() string { return "cmux" }
func (a *cmuxCreateAdapter) ListSessions(ctx context.Context) ([]mux.Session, error) {
	return nil, nil
}
func (a *cmuxCreateAdapter) GetSession(id string) (mux.Session, error) {
	return nil, mux.ErrSessionNotFound
}
func (a *cmuxCreateAdapter) CreateSession(_ context.Context, opts mux.CreateOptions) (string, error) {
	return "surface:42", nil // local ID (Phase 1 contract)
}

// Phase 2: unsupported capability and capability-less session tests.

func TestAPIGolden_CapabilityLessSession_NoPanic(t *testing.T) {
	sess := &bareSession{}
	adapter := &bareAdapter{sessions: []mux.Session{sess}}
	reg := mux.MustNewRegistry(adapter)
	h := &Handlers{Registry: reg, Events: NewMemoryEventStore()}

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bare session GET: status = %d, want 200", rec.Code)
	}
	var sessions []SessionTelemetry
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("bare session GET: invalid JSON: %v", err)
	}
	for _, s := range sessions {
		if s.ID == "bare:test" && len(s.Capabilities) != 0 {
			t.Errorf("bare session has capabilities %v, want empty", s.Capabilities)
		}
	}

	wsReq := httptest.NewRequest("GET", "/term/ws?session=bare:test", nil)
	wsRec := httptest.NewRecorder()
	h.HandleWS(wsRec, wsReq)
	if wsRec.Code != http.StatusNotImplemented {
		t.Errorf("bare session WS: status = %d, want 501", wsRec.Code)
	}
	if !strings.Contains(wsRec.Body.String(), "\"error\"") {
		t.Errorf("bare session WS error is not JSON: %s", wsRec.Body.String())
	}
	ct := wsRec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("WS unsupported: Content-Type = %q, want application/json", ct)
	}
	var errResp struct {
		Error  string `json:"error"`
		Detail string `json:"detail"`
	}
	if e := json.Unmarshal(wsRec.Body.Bytes(), &errResp); e != nil {
		t.Fatalf("WS unsupported: invalid JSON: %v", e)
	}
	if errResp.Error != "unsupported" {
		t.Errorf("WS unsupported: error = %q, want 'unsupported'", errResp.Error)
	}
	if errResp.Detail == "" {
		t.Error("WS unsupported: detail is empty")
	}
}

type bareSession struct{}

func (s *bareSession) ID() string          { return "test" }
func (s *bareSession) Title() string       { return "Bare" }
func (s *bareSession) AdapterName() string { return "bare" }

type bareAdapter struct{ sessions []mux.Session }

func (a *bareAdapter) Name() string { return "bare" }
func (a *bareAdapter) ListSessions(ctx context.Context) ([]mux.Session, error) {
	return a.sessions, nil
}
func (a *bareAdapter) GetSession(id string) (mux.Session, error) {
	for _, s := range a.sessions {
		if s.ID() == id {
			return s, nil
		}
	}
	return nil, mux.ErrSessionNotFound
}

func TestAPIGolden_BackwardCompat_MissingNewFields(t *testing.T) {
	// Verify that old daemon responses without displayId/capabilities
	// still deserialize correctly. New fields are omitempty.
	oldFormat := `[{"id":"tmux:old","state":"idle","load":0,"runner":"cat","runnerColor":"#58a6ff","adapter":"tmux","events":[]}]`
	var sessions []SessionTelemetry
	if err := json.Unmarshal([]byte(oldFormat), &sessions); err != nil {
		t.Fatalf("old format deserialize: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	s := sessions[0]
	if s.ID != "tmux:old" {
		t.Errorf("id = %q", s.ID)
	}
	if s.DisplayID != "" {
		t.Errorf("displayId = %q, want empty (omitempty field)", s.DisplayID)
	}
	if s.Capabilities != nil {
		t.Errorf("capabilities = %v, want nil (omitempty field)", s.Capabilities)
	}
}
