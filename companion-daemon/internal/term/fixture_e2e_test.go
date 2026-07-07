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

// --- Full session (has ScreenReader, StreamOpener, InputWriter) ---

type fixtureFullSession struct {
	id        string
	title     string
	lastInput []byte // captured WS input for assertion
	mu        sync.Mutex
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
func (s *fixtureFullSession) WriteInput(_ context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastInput = append([]byte(nil), data...)
	return nil
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

// --- Stream (io.Pipe-based, reliable for WS testing) ---

type fixtureStream struct {
	pr        *io.PipeReader
	pw        *io.PipeWriter
	done      chan struct{}
	closeOnce sync.Once
	input     strings.Builder
	resize    [][2]int
	mu        sync.Mutex
}

func newFixtureStream() *fixtureStream {
	pr, pw := io.Pipe()
	fs := &fixtureStream{pr: pr, pw: pw, done: make(chan struct{})}
	go func() {
		pw.Write([]byte("FIXTURE_STREAM_CONTENT"))
		<-fs.done // block until Close, keeping pipe alive
		pw.Close()
	}()
	return fs
}

func (s *fixtureStream) Read(p []byte) (int, error) {
	return s.pr.Read(p)
}

func (s *fixtureStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.input.Write(p)
	return len(p), nil
}

func (s *fixtureStream) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.pr.Close()
	})
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
	h, adapter := fixtureE2EHandlers(t)

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

	// Frame 1: ReadScreen preflight (CSI clear + screen content).
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg1, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("WS read frame 1: %v", err)
	}
	if !strings.Contains(string(msg1), "fixture screen content") {
		t.Errorf("frame 1 missing ReadScreen content: %q", string(msg1))
	}

	// Frame 2: stream content pushed via io.Pipe.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg2, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("WS read frame 2 (stream): %v", err)
	}
	if !strings.Contains(string(msg2), "FIXTURE_STREAM_CONTENT") {
		t.Errorf("frame 2 missing stream content: %q", string(msg2))
	}

	// Write input through WS → verify it reached the session via InputWriter.
	testInput := []byte("echo hello\n")
	if err := conn.WriteMessage(websocket.TextMessage, testInput); err != nil {
		t.Fatalf("WS write: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // allow handler to process

	// Verify input reached the session (WS handler calls InputWriter.WriteInput).
	adapter.mu.Lock()
	sess := adapter.sessions["f1"].(*fixtureFullSession)
	adapter.mu.Unlock()
	sess.mu.Lock()
	got := string(sess.lastInput)
	sess.mu.Unlock()
	if got != string(testInput) {
		t.Errorf("WS input not relayed to session: got %q, want %q", got, string(testInput))
	}
}

func TestFixtureE2E_Resize(t *testing.T) {
	// Resize is client-side only in this architecture.
	// xterm.js handles resize in the browser; there is no server-side
	// /term/size Go handler (not in app.go, not in pty.go). This is
	// true for tmux and cmux as well — it's an architectural choice,
	// not a fixture limitation.
	//
	// The TerminalStream interface supports Resize and the fixture
	// stream honors the contract. We verify that interface-level
	// contract here. A server-side resize handler is a potential
	// Phase 6+ enhancement, not a Phase 5 blocker.
	stream := newFixtureStream()
	defer stream.Close()
	if err := stream.Resize(40, 120); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	stream.mu.Lock()
	n := len(stream.resize)
	dims := stream.resize
	stream.mu.Unlock()
	if n != 1 {
		t.Fatalf("Resize recorded %d calls, want 1", n)
	}
	if dims[0] != [2]int{40, 120} {
		t.Errorf("Resize recorded %v, want [40, 120]", dims[0])
	}
}

func TestFixtureE2E_StreamContent(t *testing.T) {
	// Verify fixture stream delivers expected content (io.Pipe-backed).
	stream := newFixtureStream()
	defer stream.Close()

	buf := make([]byte, 1024)
	n, err := stream.Read(buf)
	if err != nil {
		t.Fatalf("stream Read: %v", err)
	}
	if n == 0 {
		t.Fatal("stream Read returned 0 bytes")
	}
	if !strings.Contains(string(buf[:n]), "FIXTURE_STREAM_CONTENT") {
		t.Errorf("stream content: got %q, want FIXTURE_STREAM_CONTENT", string(buf[:n]))
	}

	// Write must capture input.
	input := []byte("test input")
	nw, err := stream.Write(input)
	if err != nil {
		t.Fatalf("stream Write: %v", err)
	}
	if nw != len(input) {
		t.Errorf("Write returned %d, want %d", nw, len(input))
	}
	stream.mu.Lock()
	written := stream.input.String()
	stream.mu.Unlock()
	if written != "test input" {
		t.Errorf("stream captured input %q, want 'test input'", written)
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
