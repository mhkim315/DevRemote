package term

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"devremote/companion-daemon/internal/mux"
)

// fakeControlledAdapter registers as "controlled_pty" and records CreateOptions
// without spawning a real process. It also returns findable sessions with a
// controllable OpenStream so the readiness/cleanup contract can be tested.
type fakeControlledAdapter struct {
	mu            sync.Mutex
	sessions      map[string]*fakeControlledSession
	lastOpts      mux.CreateOptions
	createCalls   int
	terminated    []string
	openStreamErr bool
}

func newFakeAdapter() *fakeControlledAdapter {
	return &fakeControlledAdapter{sessions: map[string]*fakeControlledSession{}}
}
func (a *fakeControlledAdapter) Name() string { return "controlled_pty" }
func (a *fakeControlledAdapter) ListSessions(ctx context.Context) ([]mux.Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]mux.Session, 0, len(a.sessions))
	for _, s := range a.sessions {
		out = append(out, s)
	}
	return out, nil
}
func (a *fakeControlledAdapter) CreateSession(ctx context.Context, opts mux.CreateOptions) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.createCalls++
	a.lastOpts = opts
	id := opts.Name
	if id == "" {
		id = "generated"
	}
	a.sessions[id] = &fakeControlledSession{id: id, adapter: a}
	return id, nil
}
func (a *fakeControlledAdapter) TerminateSession(ctx context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.terminated = append(a.terminated, id)
	delete(a.sessions, id)
	return nil
}
func (a *fakeControlledAdapter) snapshot() (mux.CreateOptions, int, []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastOpts, a.createCalls, append([]string(nil), a.terminated...)
}

type fakeControlledSession struct {
	id      string
	adapter *fakeControlledAdapter
}

func (s *fakeControlledSession) ID() string          { return s.id }
func (s *fakeControlledSession) AdapterName() string { return "controlled_pty" }
func (s *fakeControlledSession) Title() string       { return s.id }

// TerminateGroup: real controlled-PTY sessions are mux.ManagedProcess; the
// PA2c-R2 mandatory handle capture requires the fake to expose it too.
func (s *fakeControlledSession) TerminateGroup(bool) error { return nil }
func (s *fakeControlledSession) OpenStream(ctx context.Context) (mux.TerminalStream, error) {
	s.adapter.mu.Lock()
	fail := s.adapter.openStreamErr
	s.adapter.mu.Unlock()
	if fail {
		return nil, fmt.Errorf("openstream failed")
	}
	return &fakeStream{closed: make(chan struct{})}, nil
}

// fakeStream blocks in Read until closed so the Recorder stays alive (mimics a
// live PTY with no immediate output).
type fakeStream struct {
	closed    chan struct{}
	closeOnce sync.Once
}

func (s *fakeStream) Read(p []byte) (int, error) { <-s.closed; return 0, io.EOF }
func (s *fakeStream) Write(p []byte) (int, error) {
	return len(p), nil
}
func (s *fakeStream) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}
func (s *fakeStream) Resize(rows, cols int) error { return nil }

func newTestHandlers(t *testing.T) (*Handlers, *fakeControlledAdapter) {
	t.Helper()
	fa := newFakeAdapter()
	reg := mux.MustNewRegistry(fa)
	activity := NewActivityBuffer(10)
	// PA2c: profile creation dispatches through the OwnedPTYRuntime owner
	// (production parity — app.go always wires lifecycle + owned PTY).
	owned := NewOwnedPTYRuntime(reg, activity, nil)
	h := &Handlers{Registry: reg, Activity: activity, Lifecycle: NewLifecycleService(owned, activity, nil)}
	return h, fa
}

func postSessions(h *Handlers, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.HandleSessionsAPI(rr, req)
	return rr
}

func subscriberCount(rec *Recorder) int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.subscribers)
}

func TestSessionProfiles_ReturnsSafePresets(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/session-profiles", nil)
	rr := httptest.NewRecorder()
	HandleSessionProfiles(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var profiles []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &profiles); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ids := map[string]bool{}
	for _, p := range profiles {
		ids[p["id"].(string)] = true
		if _, ok := p["executable"]; ok {
			t.Fatalf("profile leaked executable field: %v", p)
		}
	}
	for _, want := range []string{"shell", "codex", "claude"} {
		if !ids[want] {
			t.Fatalf("missing profile %q; got %v", want, ids)
		}
	}
}

// BLOCKER 2: running only when the Recorder is alive, with no phantom viewer.
func TestCreate_ProfileShell_RunningRecorderReadyNoPhantom(t *testing.T) {
	h, fa := newTestHandlers(t)
	dir := t.TempDir()
	rr := postSessions(h, `{"adapter":"controlled_pty","profileId":"shell","name":"work","cwd":"`+dir+`"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var lc SessionLifecycle
	json.Unmarshal(rr.Body.Bytes(), &lc)
	if !strings.HasPrefix(lc.ID, "controlled_pty:shell-") || lc.State != LifecycleRunning {
		t.Fatalf("lifecycle = %+v, want daemon id + running", lc)
	}
	t.Cleanup(func() { DeleteRecorder(lc.ID) })

	opts, calls, _ := fa.snapshot()
	if calls != 1 || opts.Executable == "" || !filepath.IsAbs(opts.Executable) {
		t.Fatalf("opts = %+v calls=%d, want 1 call + resolved abs executable", opts, calls)
	}
	if opts.Command != "" {
		t.Fatalf("profile create must not use bash -c Command, got %q", opts.Command)
	}
	if opts.CWD != dir {
		t.Fatalf("cwd = %q, want exact %q", opts.CWD, dir)
	}

	rec := GetRecorder(lc.ID)
	if rec == nil || !rec.IsAlive() {
		t.Fatalf("recorder not alive after create")
	}
	if n := subscriberCount(rec); n != 0 {
		t.Fatalf("subscriber count = %d, want 0 (no phantom starter subscriber)", n)
	}
}

// M3a: an empty display name is accepted and the daemon derives a non-empty
// default from the profile label, returning a running controlled_pty session.
func TestCreate_ProfileShell_EmptyName_DefaultsToLabel(t *testing.T) {
	h, _ := newTestHandlers(t)
	rr := postSessions(h, `{"profileId":"shell","name":""}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var lc SessionLifecycle
	json.Unmarshal(rr.Body.Bytes(), &lc)
	if lc.Name != "Shell" {
		t.Fatalf("name = %q, want server-derived default %q", lc.Name, "Shell")
	}
	if lc.Adapter != "controlled_pty" || lc.State != LifecycleRunning || lc.ID == "" {
		t.Fatalf("lifecycle = %+v, want running controlled_pty with id", lc)
	}
	t.Cleanup(func() { DeleteRecorder(lc.ID) })
}

// BLOCKER 2: OpenStream failure must never report running and must clean up.
func TestCreate_OpenStreamFailure_NotRunningCleansUp(t *testing.T) {
	h, fa := newTestHandlers(t)
	fa.openStreamErr = true
	rr := postSessions(h, `{"adapter":"controlled_pty","profileId":"shell","name":"work"}`)
	if rr.Code == http.StatusOK {
		t.Fatalf("status = 200, want non-200 on readiness failure (body=%s)", rr.Body.String())
	}
	var lc SessionLifecycle
	json.Unmarshal(rr.Body.Bytes(), &lc)
	if lc.State == LifecycleRunning {
		t.Fatalf("state = running on readiness failure, want failed")
	}
	_, calls, terminated := fa.snapshot()
	if calls != 1 {
		t.Fatalf("create calls = %d, want 1", calls)
	}
	if len(terminated) != 1 {
		t.Fatalf("terminated = %v, want the unrecorded runtime cleaned up", terminated)
	}
}

// BLOCKER 1: HTTP custom is denied — no InsecureLocalOnly / loopback bypass.
func TestCreate_CustomOverHTTP_Denied(t *testing.T) {
	for _, insecure := range []bool{false, true} {
		h, fa := newTestHandlers(t)
		h.InsecureLocalOnly = insecure
		rr := postSessions(h, `{"adapter":"controlled_pty","profileId":"custom","name":"x","command":{"executable":"bash"}}`)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("insecure=%v: status = %d, want 403", insecure, rr.Code)
		}
		if _, calls, _ := fa.snapshot(); calls != 0 {
			t.Fatalf("insecure=%v: custom over HTTP created a session (calls=%d)", insecure, calls)
		}
	}
}

// BLOCKER 1: controlled_pty legacy HTTP request must not execute a command.
func TestCreate_LegacyShapeOverHTTP_Rejected(t *testing.T) {
	h, fa := newTestHandlers(t)
	rr := postSessions(h, `{"id":"controlled_pty:run-1","runner":"bash","command":"bash"}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (controlled_pty HTTP command must be rejected)", rr.Code)
	}
	if _, calls, _ := fa.snapshot(); calls != 0 {
		t.Fatalf("legacy HTTP shape created a session (calls=%d)", calls)
	}
}

func TestCreate_UnknownProfile_Rejected(t *testing.T) {
	h, _ := newTestHandlers(t)
	if rr := postSessions(h, `{"profileId":"nope","name":"x"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestCreate_InvalidCWDAndName_Rejected(t *testing.T) {
	h, _ := newTestHandlers(t)
	cases := []string{
		`{"profileId":"shell","name":"x","cwd":"relative/dir"}`,
		`{"profileId":"shell","name":"x","cwd":"/no/such/dir/xyz123"}`,
		`{"profileId":"shell","name":"bad/name"}`,
	}
	for _, body := range cases {
		if rr := postSessions(h, body); rr.Code != http.StatusBadRequest {
			t.Fatalf("body %s -> status %d, want 400", body, rr.Code)
		}
	}
}

// BLOCKER 1: privileged local create still runs an arbitrary command.
func TestPrivilegedLocalCreate_LegacyCommandWorks(t *testing.T) {
	_, fa := newTestHandlers(t)
	reg := mux.MustNewRegistry(fa)
	activity := NewActivityBuffer(10)
	id, state, err := createLocalControlled(context.Background(), NewOwnedPTYRuntime(reg, activity, nil), localCreateSpec{Command: json.RawMessage(`"bash"`)})
	if err != nil {
		t.Fatalf("local create err: %v", err)
	}
	t.Cleanup(func() { DeleteRecorder(id) })
	if state != LifecycleRunning || !strings.HasPrefix(id, "controlled_pty:run-") {
		t.Fatalf("id=%q state=%q, want running run-*", id, state)
	}
	opts, _, _ := fa.snapshot()
	if opts.Command != "bash" || opts.Executable != "" {
		t.Fatalf("opts=%+v, want legacy Command=bash", opts)
	}
}

func TestPrivilegedLocalCreate_CustomArgvWorks(t *testing.T) {
	_, fa := newTestHandlers(t)
	reg := mux.MustNewRegistry(fa)
	id, _, err := createLocalControlled(context.Background(), NewOwnedPTYRuntime(reg, NewActivityBuffer(10), nil),
		localCreateSpec{Executable: "bash", Args: []string{"-lc", "echo hi"}})
	if err != nil {
		t.Fatalf("custom argv err: %v", err)
	}
	t.Cleanup(func() { DeleteRecorder(id) })
	opts, _, _ := fa.snapshot()
	if filepath.Base(opts.Executable) != "bash" || len(opts.Args) != 2 {
		t.Fatalf("opts=%+v, want resolved bash + 2 args", opts)
	}
}

// BLOCKER 3: malformed legacy command creates no session.
func TestPrivilegedLocalCreate_StrictDecodeRejectsMalformed(t *testing.T) {
	_, fa := newTestHandlers(t)
	reg := mux.MustNewRegistry(fa)
	activity := NewActivityBuffer(10)
	malformed := []json.RawMessage{
		json.RawMessage(`{"executable":"bash"}`), // object
		json.RawMessage(`123`),                   // number
		json.RawMessage(`null`),                  // null
		nil,                                      // missing
		json.RawMessage(`""`),                    // empty string
	}
	for _, cmd := range malformed {
		id, state, err := createLocalControlled(context.Background(), NewOwnedPTYRuntime(reg, activity, nil), localCreateSpec{Command: cmd})
		if err == nil {
			DeleteRecorder(id)
			t.Fatalf("command %q accepted, want rejection", string(cmd))
		}
		if state != LifecycleFailed || id != "" {
			t.Fatalf("command %q -> id=%q state=%q, want failed/empty", string(cmd), id, state)
		}
	}
	if _, calls, _ := fa.snapshot(); calls != 0 {
		t.Fatalf("malformed commands created %d sessions, want 0", calls)
	}
}
