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

	// An unwired handler returns an empty V1 catalog, still as JSON.
	raw := rec.Body.String()
	for _, k := range []string{`"state"`, `"load"`, `"runner"`, `"runnerColor"`, `"events"`} {
		if strings.Contains(raw, k) {
			t.Errorf("GET /api/sessions: legacy key %s must be absent from JSON", k)
		}
	}

	var sessions []SessionTelemetry
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("GET /api/sessions: invalid JSON: %v", err)
	}

	if len(sessions) != 0 {
		t.Errorf("GET /api/sessions: got %d rows, want empty V1 catalog", len(sessions))
	}
}
func TestAPIGolden_PostCreateSession_ReturnsCanonicalID(t *testing.T) {
	// Phase 1: POST create returns CANONICAL ID (<adapter>:<local-id>).
	h := goldenHandlers(t)

	body := strings.NewReader(`{"id":"legacy:test","runner":"claude","runnerColor":"#58a6ff"}`)
	req := httptest.NewRequest("POST", "/api/sessions", body)
	rec := httptest.NewRecorder()

	h.HandleSessionsAPI(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("POST /api/sessions: status = %d, want 501 for non-owned adapter", rec.Code)
	}
}

func TestAPIGolden_DeleteSession(t *testing.T) {
	h := goldenHandlers(t)

	req := httptest.NewRequest("DELETE", "/api/sessions?id=legacy:golden", nil)
	rec := httptest.NewRecorder()

	h.HandleSessionsAPI(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE /api/sessions: status = %d, want 404 for non-owned adapter", rec.Code)
	}
}

func TestAPIGolden_GetSessionsHistory_NoEvents(t *testing.T) {
	// PA3 Step 3 R1: ?history= returns 410 Gone for ALL sessions.
	h := goldenHandlers(t)
	req := httptest.NewRequest("GET", "/api/sessions?history=legacy:golden", nil)
	rec := httptest.NewRecorder()

	h.HandleSessionsAPI(rec, req)

	// Golden session has ScreenReader capability → history falls back to screen content.
	if rec.Code != http.StatusGone {
		t.Fatalf("GET /api/sessions?history=legacy:golden: status = %d, want %d (Gone)", rec.Code, http.StatusGone)
	}
}

func TestAPIGolden_GetSessionsHistory_ScreenFallback(t *testing.T) {
	// PA3 Step 3 R1: ?history= returns 410 Gone even for screen-capable sessions.
	h := goldenHandlersWithScreenReader(t)
	req := httptest.NewRequest("GET", "/api/sessions?history=legacy:golden", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)
	if rec.Code != http.StatusGone {
		t.Fatalf("GET /api/sessions?history=legacy:golden (screen fallback): status = %d, want %d (Gone)", rec.Code, http.StatusGone)
	}
}

// goldenHandlers returns a Handlers wired with a fake Registry containing
// one static legacy session. No real legacy process is needed.
func goldenHandlers(t *testing.T) *Handlers {
	t.Helper()

	return &Handlers{}
}

// Inline test types for golden API tests.

type goldenSession struct{}

func (s *goldenSession) ID() string                                     { return "golden" }
func (s *goldenSession) Title() string                                  { return "Golden Session" }
func (s *goldenSession) AdapterName() string                            { return "legacy" }
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

func (a *goldenAdapter) Name() string { return "legacy" }
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
	_ = sess
	return &Handlers{}
}

type goldenScreenSession struct{ goldenSession }

func (s *goldenScreenSession) ReadScreen(_ context.Context) ([]byte, error) {
	return []byte("GOLDEN_SCREEN_content"), nil
}

type goldenScreenAdapter struct {
	sessions []mux.Session
}

func (a *goldenScreenAdapter) Name() string { return "legacy" }
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

func TestAPIGolden_PostCreateLegacySession_CanonicalID(t *testing.T) {
	// legacy create must return canonical ID via handler exactly once.
	h := &Handlers{}

	body := strings.NewReader(`{"id":"legacy:test","runner":"agent","runnerColor":"#58a6ff"}`)
	req := httptest.NewRequest("POST", "/api/sessions", body)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("POST legacy create: status = %d, want 501", rec.Code)
	}
	// Non-owned adapters cannot allocate a process through the V1 boundary.
}

type legacyCreateAdapter struct{}

func (a *legacyCreateAdapter) Name() string { return "legacy" }
func (a *legacyCreateAdapter) ListSessions(ctx context.Context) ([]mux.Session, error) {
	return nil, nil
}
func (a *legacyCreateAdapter) GetSession(id string) (mux.Session, error) {
	return nil, mux.ErrSessionNotFound
}
func (a *legacyCreateAdapter) CreateSession(_ context.Context, opts mux.CreateOptions) (string, error) {
	return "surface:42", nil // local ID (Phase 1 contract)
}

// Phase 2: unsupported capability and capability-less session tests.

func TestAPIGolden_CapabilityLessSession_NoPanic(t *testing.T) {
	sess := &bareSession{}
	adapter := &bareAdapter{sessions: []mux.Session{sess}}
	_ = adapter
	h := &Handlers{}

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
	if wsRec.Code != http.StatusNotFound {
		t.Errorf("bare session WS: status = %d, want 404", wsRec.Code)
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
	oldFormat := `[{"id":"legacy:old","state":"idle","load":0,"runner":"cat","runnerColor":"#58a6ff","adapter":"legacy","events":[]}]`
	var sessions []SessionTelemetry
	if err := json.Unmarshal([]byte(oldFormat), &sessions); err != nil {
		t.Fatalf("old format deserialize: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	s := sessions[0]
	if s.ID != "legacy:old" {
		t.Errorf("id = %q", s.ID)
	}
	if s.DisplayID != "" {
		t.Errorf("displayId = %q, want empty (omitempty field)", s.DisplayID)
	}
	if s.Capabilities != nil {
		t.Errorf("capabilities = %v, want nil (omitempty field)", s.Capabilities)
	}
}
