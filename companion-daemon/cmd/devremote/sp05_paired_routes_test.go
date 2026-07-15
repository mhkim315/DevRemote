package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/term"
)

// ── SP0.5-R1 blocker 4: real paired-device evidence on the managed routes ──

// cmdFakeProc / cmdFakeLauncher: a minimal deterministic app-server fake for
// production-route tests (implements the exported narrow seam).
type cmdFakeProc struct {
	stdinR  *io.PipeReader
	stdinW  *io.PipeWriter
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	killed  chan struct{}
	once    sync.Once
}

func (p *cmdFakeProc) Stdin() io.Writer  { return p.stdinW }
func (p *cmdFakeProc) Stdout() io.Reader { return p.stdoutR }
func (p *cmdFakeProc) Term() error       { return p.Kill() }
func (p *cmdFakeProc) Kill() error {
	p.once.Do(func() {
		close(p.killed)
		p.stdinR.CloseWithError(io.ErrClosedPipe)
		p.stdoutW.CloseWithError(io.EOF)
	})
	return nil
}
func (p *cmdFakeProc) Wait() error {
	<-p.killed
	return nil
}
func (p *cmdFakeProc) OpaqueID() string { return "cmd-fake-proc" }

type cmdFakeLauncher struct {
	mu    sync.Mutex
	turns int
}

func (l *cmdFakeLauncher) turnCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.turns
}

func (l *cmdFakeLauncher) Launch(_ string, _ []string) (term.ManagedProcess, error) {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	p := &cmdFakeProc{stdinR: stdinR, stdinW: stdinW, stdoutR: stdoutR, stdoutW: stdoutW, killed: make(chan struct{})}
	go func() {
		sc := bufio.NewScanner(stdinR)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		out := func(m map[string]any) {
			b, _ := json.Marshal(m)
			stdoutW.Write(append(b, '\n'))
		}
		for sc.Scan() {
			var m map[string]any
			if json.Unmarshal(bytes.TrimSpace(sc.Bytes()), &m) != nil {
				continue
			}
			id, _ := m["id"].(float64)
			switch m["method"] {
			case "initialize":
				out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
			case "thread/start":
				out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"threadId": "thread-R"}})
			case "turn/start":
				l.mu.Lock()
				l.turns++
				l.mu.Unlock()
				out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"turn": map[string]any{"id": "turn-R"}}})
				out(map[string]any{"jsonrpc": "2.0", "method": "turn/started", "params": map[string]any{"threadId": "thread-R", "turn": map[string]any{"id": "turn-R"}}})
				out(map[string]any{"jsonrpc": "2.0", "method": "item/completed", "params": map[string]any{
					"threadId": "thread-R", "turnId": "turn-R",
					"item": map[string]any{"type": "agentMessage", "id": "i", "text": "paired-route-reply"},
				}})
				out(map[string]any{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{"threadId": "thread-R", "turn": map[string]any{"id": "turn-R"}}})
			}
		}
	}()
	return p, nil
}

func managedRemoteFixture(t *testing.T) (*remoteFixture, *cmdFakeLauncher) {
	t.Helper()
	fl := &cmdFakeLauncher{}
	f := newRemoteFixtureWith(t, nil, nil, func(cfg *Config, deps *Dependencies) {
		cfg.EnableManagedCodex = true
		deps.Managed = term.NewManagedCodexServiceForTest(fl, func() error { return nil })
	})
	return f, fl
}

func (f *remoteFixture) doJSON(t *testing.T, method, path, bearer, body string) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, f.srv.URL+path, rd)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 1<<20)
	n, _ := resp.Body.Read(buf)
	return resp.StatusCode, string(buf[:n])
}

// TestManagedRoutes_PairedDeviceProductionPath: the REQUIRED positive and
// negative paired-device evidence on the managed production routes — real
// challenge/verify bearers from a real device registry, through the real
// remote-mode router.
func TestManagedRoutes_PairedDeviceProductionPath(t *testing.T) {
	f, fl := managedRemoteFixture(t)

	// First paired device = owner (full permissions incl. terminal:input);
	// second = member (sessions:read ONLY).
	ownerPriv, ownerID := f.pairDevice(t, "owner-phone")
	memberPriv, memberID := f.pairDevice(t, "member-phone")
	ownerToken := f.token(t, ownerID, ownerPriv)
	memberToken := f.token(t, memberID, memberPriv)

	// A real managed session (deterministic fake provider, production service).
	sessionID, err := f.app.managed.CreateAttached("")
	if err != nil {
		t.Fatalf("managed create: %v", err)
	}
	eventsPath := "/api/managed-sessions/" + sessionID + "/events?epoch=1&cursor=0"
	promptPath := "/api/managed-sessions/" + sessionID + "/prompt"

	// POSITIVE: paired owner reads the managed list and events.
	if code, body := f.doJSON(t, "GET", "/api/managed-sessions", ownerToken, ""); code != 200 || !strings.Contains(body, sessionID) {
		t.Fatalf("owner managed list: code=%d body=%s", code, body)
	}
	code, body := f.doJSON(t, "GET", eventsPath, ownerToken, "")
	if code != 200 || !strings.Contains(body, `"contractVersion":"pokit.managed.v1"`) {
		t.Fatalf("owner events read: code=%d body=%s", code, body)
	}

	// POSITIVE: paired member (sessions:read) can ALSO read.
	if code, _ := f.doJSON(t, "GET", eventsPath, memberToken, ""); code != 200 {
		t.Fatalf("member events read code = %d", code)
	}

	// NEGATIVE: read-only member prompt → 403 with ZERO provider writes.
	code, _ = f.doJSON(t, "POST", promptPath, memberToken, `{"epoch":1,"text":"member tries"}`)
	if code != http.StatusForbidden {
		t.Fatalf("member prompt code = %d, want 403", code)
	}
	if fl.turnCount() != 0 {
		t.Fatalf("read-only principal caused %d provider writes", fl.turnCount())
	}

	// POSITIVE: owner (terminal:input) prompt succeeds and reaches the
	// provider exactly once.
	code, body = f.doJSON(t, "POST", promptPath, ownerToken, `{"epoch":1,"text":"owner prompt"}`)
	if code != 200 {
		t.Fatalf("owner prompt: code=%d body=%s", code, body)
	}
	deadline := time.Now().Add(2 * time.Second)
	for fl.turnCount() != 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fl.turnCount() != 1 {
		t.Fatalf("owner prompt provider writes = %d, want 1", fl.turnCount())
	}

	// The projected assistant output is then readable by the paired member.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, body := f.doJSON(t, "GET", eventsPath, memberToken, ""); strings.Contains(body, "paired-route-reply") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("assistant projection never became readable over the paired route")
}

// TestManagedRoutes_LegacyAndCrossHostRejected: a legacy dev token and a
// bearer minted by a DIFFERENT host are both rejected on the managed routes
// with zero provider writes.
func TestManagedRoutes_LegacyAndCrossHostRejected(t *testing.T) {
	f, fl := managedRemoteFixture(t)
	sessionID, err := f.app.managed.CreateAttached("")
	if err != nil {
		t.Fatalf("managed create: %v", err)
	}
	eventsPath := "/api/managed-sessions/" + sessionID + "/events?epoch=1&cursor=0"
	promptPath := "/api/managed-sessions/" + sessionID + "/prompt"

	// Legacy dev token has no device principal in remote mode.
	if code, _ := f.doJSON(t, "GET", eventsPath, "dev-token", ""); code != http.StatusUnauthorized {
		t.Fatalf("legacy bearer events code = %d, want 401", code)
	}
	if code, _ := f.doJSON(t, "POST", promptPath, "dev-token", `{"epoch":1,"text":"legacy"}`); code != http.StatusUnauthorized {
		t.Fatalf("legacy bearer prompt code = %d, want 401", code)
	}

	// A bearer minted by ANOTHER host's registry/identity is rejected here.
	other, _ := managedRemoteFixture(t)
	priv, deviceID := other.pairDevice(t, "other-host-phone")
	foreignToken := other.token(t, deviceID, priv)
	if code, _ := f.doJSON(t, "GET", eventsPath, foreignToken, ""); code != http.StatusUnauthorized {
		t.Fatalf("cross-host events code = %d, want 401", code)
	}
	if code, _ := f.doJSON(t, "POST", promptPath, foreignToken, `{"epoch":1,"text":"foreign"}`); code != http.StatusUnauthorized {
		t.Fatalf("cross-host prompt code = %d, want 401", code)
	}
	if fl.turnCount() != 0 {
		t.Fatalf("rejected credentials caused %d provider writes", fl.turnCount())
	}
}

// TestManagedPromptAPI_TrailingBodyRejected: trailing JSON after the body is
// fail-closed on prompt and lifecycle decoders (trust-boundary hardening).
func TestManagedPromptAPI_TrailingBodyRejected(t *testing.T) {
	f, fl := managedRemoteFixture(t)
	ownerPriv, ownerID := f.pairDevice(t, "owner")
	ownerToken := f.token(t, ownerID, ownerPriv)
	sessionID, err := f.app.managed.CreateAttached("")
	if err != nil {
		t.Fatalf("managed create: %v", err)
	}
	promptPath := "/api/managed-sessions/" + sessionID + "/prompt"

	if code, _ := f.doJSON(t, "POST", promptPath, ownerToken, `{"epoch":1,"text":"x"}{"epoch":1}`); code != http.StatusBadRequest {
		t.Fatalf("trailing prompt body code = %d, want 400", code)
	}
	if code, _ := f.doJSON(t, "POST", "/api/managed-sessions/"+sessionID+"/stop", ownerToken, `{"epoch":1}[1,2]`); code != http.StatusBadRequest {
		t.Fatalf("trailing lifecycle body code = %d, want 400", code)
	}
	if fl.turnCount() != 0 {
		t.Fatalf("trailing bodies caused %d provider writes", fl.turnCount())
	}
}