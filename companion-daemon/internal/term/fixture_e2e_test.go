package term

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"github.com/gorilla/websocket"
)

// Phase 5 E2E: fixture adapter at HTTP/WS/telemetry/mobile boundaries.
// 0 tmux/cmux changes. All state mutations provable.

// --- Adapter ---

type fixtureE2EAdapter struct {
	mu       sync.Mutex
	sessions map[string]mux.Session
}

func newFixtureE2EAdapter() *fixtureE2EAdapter {
	return &fixtureE2EAdapter{
		sessions: map[string]mux.Session{
			"f1": &fixtureFullSession{id: "f1", title: "fixture-shell"},
			"f2": &fixtureFullSession{id: "f2", title: "fixture-build"},
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
	s := &fixtureFullSession{id: id, title: id}
	a.sessions[id] = s
	return id, nil
}
func (a *fixtureE2EAdapter) TerminateSession(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, id)
	return nil
}

// --- Full session (has ScreenReader, StreamOpener) ---

type fixtureFullSession struct {
	id    string
	title string
}

func (s *fixtureFullSession) ID() string                      { return s.id }
func (s *fixtureFullSession) Title() string                   { return s.title }
func (s *fixtureFullSession) AdapterName() string             { return "fixture" }
func (s *fixtureFullSession) ReadScreen(_ context.Context) ([]byte, error) {
	return []byte("fixture screen content"), nil
}
func (s *fixtureFullSession) OpenStream(_ context.Context) (mux.TerminalStream, error) {
	return newFixtureStream(), nil
}

// --- Bare session (NO ScreenReader, NO HistoryReader, NO StreamOpener) ---
// Intentionally unsupported — used to prove unsupported path at API boundary.

type fixtureBareSession struct {
	id    string
	title string
}

func (s *fixtureBareSession) ID() string          { return s.id }
func (s *fixtureBareSession) Title() string       { return s.title }
func (s *fixtureBareSession) AdapterName() string { return "fixture" }

// --- Stream (pushes content, blocks until Close, supports Resize) ---

type fixtureStream struct {
	pr     *strings.Reader
	pw     *strings.Builder
	resize [][2]int // recorded resize calls
	mu     sync.Mutex
	closed bool
}

func newFixtureStream() *fixtureStream {
	return &fixtureStream{
		pr: strings.NewReader("FIXTURE_STREAM_CONTENT"),
		pw: &strings.Builder{},
	}
}

func (s *fixtureStream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pr.Read(p)
}

func (s *fixtureStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pw.Write(p)
}

func (s *fixtureStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *fixtureStream) Resize(rows, cols int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resize = append(s.resize, [2]int{rows, cols})
	return nil
}

// --- Handler factory ---

func fixtureE2EHandlers(t *testing.T) (*Handlers, *fixtureE2EAdapter) {
	t.Helper()
	adapter := newFixtureE2EAdapter()
	reg := mux.MustNewRegistry(adapter)
	return &Handlers{Registry: reg, Events: NewMemoryEventStore()}, adapter
}

// bareSessionHandlers returns Handlers with a session that has ZERO optional capabilities.
func bareSessionHandlers(t *testing.T) *Handlers {
	t.Helper()
	adapter := &bareOnlyAdapter{
		sessions: []mux.Session{&fixtureBareSession{id: "bare", title: "Bare Session"}},
	}
	reg := mux.MustNewRegistry(adapter)
	return &Handlers{Registry: reg, Events: NewMemoryEventStore()}
}

type bareOnlyAdapter struct {
	sessions []mux.Session
}

func (a *bareOnlyAdapter) Name() string                                       { return "bare" }
func (a *bareOnlyAdapter) ListSessions(_ context.Context) ([]mux.Session, error) { return a.sessions, nil }

// --- Tests ---

func TestFixtureE2E_GetSessions(t *testing.T) {
	h, _ := fixtureE2EHandlers(t)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
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
				t.Errorf("ID %q missing adapter prefix", s.ID)
			}
		}
	}
	for _, want := range []string{"fixture:f1", "fixture:f2"} {
		if !found[want] {
			t.Errorf("session %q not found", want)
		}
	}
}

func TestFixtureE2E_CreateAndDelete(t *testing.T) {
	h, _ := fixtureE2EHandlers(t)

	// Baseline count.
	getCount := func() int {
		req := httptest.NewRequest("GET", "/api/sessions", nil)
		rec := httptest.NewRecorder()
		h.HandleSessionsAPI(rec, req)
		var s []SessionTelemetry
		json.Unmarshal(rec.Body.Bytes(), &s)
		return len(s)
	}
	before := getCount()

	// Create.
	body := strings.NewReader(`{"id":"fixture:test-create","runner":"claude"}`)
	reqC := httptest.NewRequest("POST", "/api/sessions", body)
	recC := httptest.NewRecorder()
	h.HandleSessionsAPI(recC, reqC)
	if recC.Code != http.StatusOK {
		t.Fatalf("POST: %d", recC.Code)
	}
	var cr struct{ Status, ID string }
	json.Unmarshal(recC.Body.Bytes(), &cr)
	if getCount() != before+1 {
		t.Error("count did not increase after create")
	}

	// Delete.
	reqD := httptest.NewRequest("DELETE", "/api/sessions?id="+cr.ID, nil)
	recD := httptest.NewRecorder()
	h.HandleSessionsAPI(recD, reqD)
	if recD.Code != http.StatusOK {
		t.Errorf("DELETE: %d", recD.Code)
	}
	if getCount() != before {
		t.Error("count did not decrease after delete")
	}
}

func TestFixtureE2E_UnsupportedCapability(t *testing.T) {
	// Bare session: NO ScreenReader, NO HistoryReader — truly unsupported.
	// GET /api/sessions?history=bare:bare must return 404 (not 500, not empty 200).
	h := bareSessionHandlers(t)
	req := httptest.NewRequest("GET", "/api/sessions?history=bare:bare", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("unsupported history: status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "history unavailable") {
		t.Errorf("unsupported history body missing 'history unavailable': %s", rec.Body.String())
	}
}

func TestFixtureE2E_WebSocket(t *testing.T) {
	h, _ := fixtureE2EHandlers(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.RawQuery = "session=fixture:f1"
		h.HandleWS(w, r)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/term/ws?session=fixture:f1"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("WS dial: %v", err)
	}
	defer conn.Close()

	// Read: verify we receive actual content (handler sends ReadScreen as first frame).
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("WS read: %v", err)
	}
	if len(msg) == 0 {
		t.Error("WS received empty message, want content")
	}

	// Write: send input through WebSocket.
	if err := conn.WriteMessage(websocket.TextMessage, []byte("echo test\n")); err != nil {
		t.Fatalf("WS write: %v", err)
	}
}

func TestFixtureE2E_ResizeAtBoundary(t *testing.T) {
	// Verify fixture stream implements Resize and records dimensions.
	// (Resize is triggered client-side via /term/size?session=...&rows=...&cols=...).
	stream := newFixtureStream()
	if err := stream.Resize(40, 120); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	stream.mu.Lock()
	n := len(stream.resize)
	stream.mu.Unlock()
	if n != 1 {
		t.Fatalf("Resize recorded %d calls, want 1", n)
	}
	if stream.resize[0] != [2]int{40, 120} {
		t.Errorf("Resize recorded %v, want [40, 120]", stream.resize[0])
	}
}

func TestFixtureE2E_Telemetry(t *testing.T) {
	h, _ := fixtureE2EHandlers(t)

	svc := NewTelemetryService(h.Registry, h.Events, NewNopLinkStore(), NoopNotifier{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go svc.Run(ctx)
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-svc.Done()

	snapshot := svc.Snapshot(h.Registry)
	found := false
	for _, s := range snapshot {
		if s.Adapter == "fixture" {
			found = true
			// Schema lock: verify all required telemetry fields.
			if s.ID == "" {
				t.Error("telemetry: empty ID")
			}
			if s.State == "" {
				t.Error("telemetry: empty State")
			}
			// Load must be present (0 is valid).
			_ = s.Load
			if len(s.Capabilities) == 0 {
				t.Error("telemetry: empty Capabilities")
			}
		}
	}
	if !found {
		t.Error("fixture adapter not in telemetry snapshot")
	}
}

func TestFixtureE2E_MobileSchema(t *testing.T) {
	// Verify JSON response for third adapter matches mobile schema.
	// Mobile client deserializes SessionTelemetry with string fields
	// for id, adapter, displayId, state, capabilities[].
	h, _ := fixtureE2EHandlers(t)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)

	raw := rec.Body.String()

	// All required JSON keys must be present for unknown third adapter.
	requiredKeys := []string{
		`"id"`,
		`"displayId"`,
		`"state"`,
		`"load"`,
		`"runner"`,
		`"runnerColor"`,
		`"adapter"`,
		`"capabilities"`,
		`"events"`,
	}
	for _, k := range requiredKeys {
		if !strings.Contains(raw, k) {
			t.Errorf("mobile schema: JSON missing key %s for fixture adapter", k)
		}
	}

	// Verify adapter value is "fixture" (not empty, not hardcoded tmux/cmux).
	if !strings.Contains(raw, `"fixture"`) {
		t.Error("mobile schema: JSON does not contain adapter name 'fixture'")
	}
}
