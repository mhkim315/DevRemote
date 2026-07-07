package term

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"github.com/gorilla/websocket"
)

// Phase 5 E2E: fixture adapter at HTTP/WS/telemetry boundaries.
// Tests use httptest + inline adapter — 0 tmux/cmux changes.

// --- Mutable fixture adapter (stateful, shared for create→discover→delete) ---

type fixtureE2EAdapter struct {
	mu       sync.Mutex
	sessions map[string]mux.Session
}

func newFixtureE2EAdapter() *fixtureE2EAdapter {
	return &fixtureE2EAdapter{
		sessions: map[string]mux.Session{
			"f1": &fixtureE2ESession{id: "f1", title: "fixture-shell"},
			"f2": &fixtureE2ESession{id: "f2", title: "fixture-build"},
		},
	}
}

func (a *fixtureE2EAdapter) Name() string { return "fixture" }
func (a *fixtureE2EAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]mux.Session, 0, len(a.sessions))
	for _, s := range a.sessions {
		out = append(out, s)
	}
	return out, nil
}
func (a *fixtureE2EAdapter) CreateSession(_ context.Context, opts mux.CreateOptions) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := opts.Name
	if id == "" {
		id = "new"
	}
	s := &fixtureE2ESession{id: id, title: id}
	a.sessions[id] = s
	return id, nil
}
func (a *fixtureE2EAdapter) TerminateSession(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, id)
	return nil
}

type fixtureE2ESession struct {
	id    string
	title string
}

func (s *fixtureE2ESession) ID() string                      { return s.id }
func (s *fixtureE2ESession) Title() string                   { return s.title }
func (s *fixtureE2ESession) AdapterName() string             { return "fixture" }
func (s *fixtureE2ESession) ReadScreen(_ context.Context) ([]byte, error) {
	return []byte("fixture screen content"), nil
}
func (s *fixtureE2ESession) OpenStream(_ context.Context) (mux.TerminalStream, error) {
	return &fixtureE2EStream{}, nil
}

type fixtureE2EStream struct{}

func (s *fixtureE2EStream) Read(p []byte) (int, error)  { return 0, io.EOF }
func (s *fixtureE2EStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *fixtureE2EStream) Close() error                { return nil }
func (s *fixtureE2EStream) Resize(rows, cols int) error { return nil }

// fixtureE2EHandlers creates Handlers with a fresh fixture adapter.
func fixtureE2EHandlers(t *testing.T) (*Handlers, *fixtureE2EAdapter) {
	t.Helper()
	adapter := newFixtureE2EAdapter()
	reg := mux.MustNewRegistry(adapter)
	return &Handlers{Registry: reg, Events: NewMemoryEventStore()}, adapter
}

// --- Tests ---

func TestFixtureE2E_GetSessions(t *testing.T) {
	h, _ := fixtureE2EHandlers(t)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions: status = %d, want 200", rec.Code)
	}

	var sessions []SessionTelemetry
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	found := map[string]bool{}
	for _, s := range sessions {
		found[s.ID] = true
		if s.Adapter == "fixture" {
			if !strings.HasPrefix(s.ID, "fixture:") {
				t.Errorf("fixture session ID %q missing adapter prefix", s.ID)
			}
			if s.DisplayID == "" {
				t.Error("fixture session has empty displayId")
			}
			if len(s.Capabilities) == 0 {
				t.Error("fixture session has empty capabilities")
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
	h, adapter := fixtureE2EHandlers(t)

	// Verify initial count.
	req0 := httptest.NewRequest("GET", "/api/sessions", nil)
	rec0 := httptest.NewRecorder()
	h.HandleSessionsAPI(rec0, req0)
	var before []SessionTelemetry
	json.Unmarshal(rec0.Body.Bytes(), &before)
	beforeCount := len(before)

	// Create.
	body := strings.NewReader(`{"id":"fixture:test-create","runner":"claude"}`)
	req := httptest.NewRequest("POST", "/api/sessions", body)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST: status = %d, want 200", rec.Code)
	}
	var cr struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &cr)
	if !strings.HasPrefix(cr.ID, "fixture:") {
		t.Errorf("created ID %q missing fixture: prefix", cr.ID)
	}

	// Verify count increased (state mutation proof).
	req1 := httptest.NewRequest("GET", "/api/sessions", nil)
	rec1 := httptest.NewRecorder()
	h.HandleSessionsAPI(rec1, req1)
	var after []SessionTelemetry
	json.Unmarshal(rec1.Body.Bytes(), &after)
	if len(after) != beforeCount+1 {
		t.Errorf("after create: %d sessions, want %d", len(after), beforeCount+1)
	}

	// Delete using correct query param: ?id=<canonicalID>
	delReq := httptest.NewRequest("DELETE", "/api/sessions?id="+cr.ID, nil)
	delRec := httptest.NewRecorder()
	h.HandleSessionsAPI(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Errorf("DELETE: status = %d, want 200", delRec.Code)
	}

	// Verify count decreased (state mutation proof via adapter).
	req2 := httptest.NewRequest("GET", "/api/sessions", nil)
	rec2 := httptest.NewRecorder()
	h.HandleSessionsAPI(rec2, req2)
	var final []SessionTelemetry
	json.Unmarshal(rec2.Body.Bytes(), &final)
	if len(final) != beforeCount {
		t.Errorf("after delete: %d sessions, want %d", len(final), beforeCount)
	}
	_ = adapter // used for state tracking
}

func TestFixtureE2E_UnsupportedCapability(t *testing.T) {
	// fixture session has ScreenReader but NOT HistoryReader.
	// GET /api/sessions?history=fixture:f1 should use ScreenReader fallback.
	h, _ := fixtureE2EHandlers(t)
	req := httptest.NewRequest("GET", "/api/sessions?history=fixture:f1", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)

	// ScreenReader fallback should succeed (200) and return events JSON.
	if rec.Code != http.StatusOK {
		t.Errorf("history with screen fallback: status = %d, want 200 (has ScreenReader)", rec.Code)
	}

	// Body should be valid JSON array (events).
	var events []struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Detail  string `json:"detail"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("history response: invalid JSON: %v", err)
	}
	if len(events) == 0 {
		t.Error("history fallback returned 0 events")
	}
}

func TestFixtureE2E_WebSocket(t *testing.T) {
	h, _ := fixtureE2EHandlers(t)

	// Use httptest.NewServer for real WebSocket upgrade.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.RawQuery = "session=fixture:f1"
		h.HandleWS(w, r)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/term/ws?session=fixture:f1"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("WebSocket dial: %v", err)
	}
	defer conn.Close()

	// Read a message (the WS handler reads from session stream).
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		// Stream returns io.EOF which the handler will close — expected.
		t.Logf("WS read (expected for mock): %v", err)
	}
	_ = msg
}

func TestFixtureE2E_Telemetry(t *testing.T) {
	h, _ := fixtureE2EHandlers(t)

	// Use TelemetryService directly to verify fixture sessions appear in snapshot.
	svc := NewTelemetryService(h.Registry, h.Events, NewNopLinkStore(), NoopNotifier{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Run for one tick so snapshot populates.
	go svc.Run(ctx)
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-svc.Done()

	snapshot := svc.Snapshot(h.Registry)
	found := map[string]bool{}
	for _, s := range snapshot {
		found[s.Adapter] = true
		if s.Adapter == "fixture" {
			if s.ID == "" {
				t.Error("fixture telemetry session has empty ID")
			}
			if s.Adapter == "" {
				t.Error("fixture telemetry session has empty adapter")
			}
		}
	}
	if !found["fixture"] {
		t.Error("fixture adapter not found in TelemetryService snapshot")
	}
}
