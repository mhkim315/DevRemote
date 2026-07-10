package term

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"devremote/companion-daemon/internal/mux"
)

// recordingAdapter registers as "controlled_pty" and records the CreateOptions
// it receives without spawning a real process, so the M1 create tests can
// assert the daemon resolved the profile to the correct executable/argv/cwd.
type recordingAdapter struct {
	mu    sync.Mutex
	last  mux.CreateOptions
	calls int
}

func (a *recordingAdapter) Name() string { return "controlled_pty" }
func (a *recordingAdapter) ListSessions(ctx context.Context) ([]mux.Session, error) {
	return nil, nil
}
func (a *recordingAdapter) CreateSession(ctx context.Context, opts mux.CreateOptions) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	a.last = opts
	if opts.Name != "" {
		return opts.Name, nil
	}
	return "generated", nil
}
func (a *recordingAdapter) snapshot() (mux.CreateOptions, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.last, a.calls
}

func newTestHandlers(t *testing.T, insecureLocal bool) (*Handlers, *recordingAdapter) {
	t.Helper()
	rec := &recordingAdapter{}
	reg := mux.MustNewRegistry(rec)
	h := &Handlers{Registry: reg, Activity: NewActivityBuffer(10), InsecureLocalOnly: insecureLocal}
	return h, rec
}

func postSessions(h *Handlers, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.HandleSessionsAPI(rr, req)
	return rr
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
		t.Fatalf("decode: %v (body=%s)", err, rr.Body.String())
	}
	ids := map[string]bool{}
	for _, p := range profiles {
		ids[p["id"].(string)] = true
		// Executable policy must not leak to clients.
		if _, ok := p["executable"]; ok {
			t.Fatalf("profile leaked executable field: %v", p)
		}
		if _, ok := p["available"]; !ok {
			t.Fatalf("profile missing available field: %v", p)
		}
	}
	for _, want := range []string{"shell", "codex", "claude"} {
		if !ids[want] {
			t.Fatalf("missing profile %q; got %v", want, ids)
		}
	}
	// The shell profile resolves to a real binary in any test environment.
	if exe, _, avail, ok := ResolveProfile("shell"); !ok || !avail || !filepath.IsAbs(exe) {
		t.Fatalf("shell profile resolve = (%q, avail=%v, ok=%v), want available abs path", exe, avail, ok)
	}
}

func TestCreate_ProfileShell_DaemonResolvesExecutable(t *testing.T) {
	h, rec := newTestHandlers(t, false)
	dir := t.TempDir()
	rr := postSessions(h, `{"adapter":"controlled_pty","profileId":"shell","name":"work","cwd":"`+dir+`"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var lc SessionLifecycle
	if err := json.Unmarshal(rr.Body.Bytes(), &lc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Daemon generated the canonical ID; client did not supply it.
	if !strings.HasPrefix(lc.ID, "controlled_pty:shell-") {
		t.Fatalf("canonical id = %q, want daemon-generated controlled_pty:shell-*", lc.ID)
	}
	if lc.State != LifecycleRunning {
		t.Fatalf("state = %q, want running", lc.State)
	}
	if lc.ProfileID != "shell" || lc.Name != "work" {
		t.Fatalf("lifecycle = %+v, want profileId=shell name=work", lc)
	}
	// The daemon passed a resolved executable + exact CWD, NOT a bash -c string.
	opts, calls := rec.snapshot()
	if calls != 1 {
		t.Fatalf("create calls = %d, want 1", calls)
	}
	if opts.Executable == "" || !filepath.IsAbs(opts.Executable) {
		t.Fatalf("executable = %q, want resolved absolute path", opts.Executable)
	}
	if opts.Command != "" {
		t.Fatalf("legacy Command should be empty for profile create, got %q", opts.Command)
	}
	if opts.CWD != dir {
		t.Fatalf("cwd = %q, want %q (exact propagation)", opts.CWD, dir)
	}
}

func TestCreate_UnknownProfile_Rejected(t *testing.T) {
	h, _ := newTestHandlers(t, true)
	rr := postSessions(h, `{"adapter":"controlled_pty","profileId":"nope","name":"x"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestCreate_CustomCommand_DeniedForRemote(t *testing.T) {
	h, rec := newTestHandlers(t, false) // remote (auth) daemon
	rr := postSessions(h, `{"adapter":"controlled_pty","profileId":"custom","name":"x","command":{"executable":"bash"}}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
	if _, calls := rec.snapshot(); calls != 0 {
		t.Fatalf("custom command must not create a session when denied (calls=%d)", calls)
	}
}

func TestCreate_CustomCommand_AllowedLocalWithArgv(t *testing.T) {
	h, rec := newTestHandlers(t, true) // local-only daemon
	rr := postSessions(h, `{"adapter":"controlled_pty","profileId":"custom","name":"echoer","command":{"executable":"bash","args":["-lc","echo hi"]}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	opts, calls := rec.snapshot()
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if filepath.Base(opts.Executable) != "bash" {
		t.Fatalf("executable = %q, want resolved bash", opts.Executable)
	}
	if len(opts.Args) != 2 || opts.Args[0] != "-lc" {
		t.Fatalf("args = %v, want [-lc, echo hi]", opts.Args)
	}
}

func TestCreate_InvalidCWD_Rejected(t *testing.T) {
	h, _ := newTestHandlers(t, true)
	// relative path
	if rr := postSessions(h, `{"profileId":"shell","name":"x","cwd":"relative/dir"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("relative cwd status = %d, want 400", rr.Code)
	}
	// nonexistent absolute path
	if rr := postSessions(h, `{"profileId":"shell","name":"x","cwd":"/no/such/dir/xyz123"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("nonexistent cwd status = %d, want 400", rr.Code)
	}
}

func TestCreate_InvalidName_Rejected(t *testing.T) {
	h, _ := newTestHandlers(t, true)
	if rr := postSessions(h, `{"profileId":"shell","name":""}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("empty name status = %d, want 400", rr.Code)
	}
	if rr := postSessions(h, `{"profileId":"shell","name":"bad/name"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("path-separator name status = %d, want 400", rr.Code)
	}
}

func TestCreate_LegacyCLIPayload_StillCompatible(t *testing.T) {
	h, rec := newTestHandlers(t, true)
	rr := postSessions(h, `{"id":"controlled_pty:run-123","runner":"bash","runnerColor":"#fff","command":"bash","cwd":""}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" || resp.ID != "controlled_pty:run-123" {
		t.Fatalf("legacy response = %+v, want status=ok id=controlled_pty:run-123", resp)
	}
	// Legacy path uses the shell Command string, NOT the argv executable.
	opts, _ := rec.snapshot()
	if opts.Command != "bash" || opts.Executable != "" {
		t.Fatalf("legacy opts = %+v, want Command=bash Executable empty", opts)
	}
}
