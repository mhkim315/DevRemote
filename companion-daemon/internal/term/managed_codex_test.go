package term

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── Deterministic fake launcher / process / app-server script ──

// fakeManagedProc is an in-memory ManagedProcess: the runtime's stdin/stdout
// are pipe-connected to a scripted fake app-server.
type fakeManagedProc struct {
	stdinR  *io.PipeReader // script side reads what the runtime writes
	stdinW  *io.PipeWriter
	stdoutR *io.PipeReader // runtime side reads what the script writes
	stdoutW *io.PipeWriter

	ignoreTerm bool // simulate a child that ignores SIGTERM (escalation test)
	termOnce   sync.Once
	termed     chan struct{}
	killOnce   sync.Once
	killed     chan struct{}
	done       chan struct{} // script finished
}

func newFakeManagedProc() *fakeManagedProc {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	return &fakeManagedProc{
		stdinR: stdinR, stdinW: stdinW,
		stdoutR: stdoutR, stdoutW: stdoutW,
		termed: make(chan struct{}), killed: make(chan struct{}), done: make(chan struct{}),
	}
}

func (p *fakeManagedProc) Stdin() io.Writer  { return p.stdinW }
func (p *fakeManagedProc) Stdout() io.Reader { return p.stdoutR }

func (p *fakeManagedProc) Term() error {
	p.termOnce.Do(func() { close(p.termed) })
	if p.ignoreTerm {
		return nil // child ignores the graceful signal
	}
	return p.Kill() // graceful child: exits on TERM
}

func (p *fakeManagedProc) Kill() error {
	p.killOnce.Do(func() {
		close(p.killed)
		p.stdinR.CloseWithError(io.ErrClosedPipe)
		p.stdoutW.CloseWithError(io.EOF)
	})
	return nil
}

func (p *fakeManagedProc) Wait() error {
	select {
	case <-p.killed:
	case <-p.done:
	}
	return nil
}

func (p *fakeManagedProc) PID() int         { return 0 }
func (p *fakeManagedProc) OpaqueID() string { return "fake-proc" }

// scriptHandler decides the fake provider's reaction to one incoming request.
type scriptHandler func(method string, id float64, params map[string]any, out func(map[string]any))

// runAppServerScript consumes the runtime's writes and lets the handler emit
// provider messages. The script ends when the runtime's stdin closes.
func runAppServerScript(p *fakeManagedProc, handler scriptHandler) {
	go func() {
		defer close(p.done)
		sc := bufio.NewScanner(p.stdinR)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		out := func(m map[string]any) {
			b, _ := json.Marshal(m)
			p.stdoutW.Write(append(b, '\n'))
		}
		for sc.Scan() {
			var m map[string]any
			if json.Unmarshal(bytes.TrimSpace(sc.Bytes()), &m) != nil {
				continue
			}
			method, _ := m["method"].(string)
			id, _ := m["id"].(float64)
			params, _ := m["params"].(map[string]any)
			handler(method, id, params, out)
		}
	}()
}

// happyAppServer answers the evidence-exact handshake and accepts turn/start.
func happyAppServer(threadID string) scriptHandler {
	return func(method string, id float64, params map[string]any, out func(map[string]any)) {
		switch method {
		case "initialize":
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
		case "thread/start":
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"threadId": threadID}})
		}
	}
}

// fakeLauncher records the exact executable + argv of every Launch call.
type fakeLauncher struct {
	mu         sync.Mutex
	calls      int
	exe        string
	argv       []string
	err        error
	ignoreTerm bool // new procs simulate a TERM-ignoring child
	handler    scriptHandler
	procs      []*fakeManagedProc
}

func (l *fakeLauncher) Launch(exe string, argv []string) (ManagedProcess, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	l.exe = exe
	l.argv = append([]string(nil), argv...)
	if l.err != nil {
		return nil, l.err
	}
	p := newFakeManagedProc()
	p.ignoreTerm = l.ignoreTerm
	l.procs = append(l.procs, p)
	runAppServerScript(p, l.handler)
	return p, nil
}

func (l *fakeLauncher) callCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

// newTestManagedService builds a service with an injected launcher and a
// no-op verifier (verify fail-closure is tested separately with a failing
// verifier — the production verifier would exec the pinned binary).
func newTestManagedService(l ManagedLauncher) *ManagedCodexService {
	s := NewManagedCodexService(CodexAppServerEntryConfig{
		Bin:              "/pinned/toolchain/node_modules/.bin/codex",
		Version:          "codex-cli 0.144.1",
		AuthorityVersion: certifiedCodexAuthorityVersion,
	}, l)
	s.verify = func() error { return nil }
	return s
}

// ipcCreateRoundTrip drives the REAL production IPC connection handler with
// the given request body and returns the decoded response.
func ipcCreateRoundTrip(t *testing.T, managed *ManagedCodexService, body map[string]any) map[string]string {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	go handleIPCConnection(serverConn, nil, nil, managed, nil)
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := clientConn.Write(append(payload, '\n')); err != nil {
		t.Fatalf("write request: %v", err)
	}
	line, err := bufio.NewReader(clientConn).ReadString('\n')
	if err != nil && err != io.EOF {
		t.Fatalf("read response: %v", err)
	}
	clientConn.Close()
	var resp map[string]string
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("decode response %q: %v", line, err)
	}
	return resp
}

// sp0DetachCodexRequest is the exact request shape `pokit run --detach codex`
// sends (mirrors buildRunCreateRequest in cmd/devremote — a structured
// recognized-profile request with NO command string).
func sp0DetachCodexRequest() map[string]any {
	return map[string]any{
		"version":   1,
		"operation": "create",
		"profileId": "codex",
		"cwd":       "",
		"detach":    true,
	}
}

// ── P1 focused tests ──

// TestManagedIPCCreate_StructuredRequest_ExactArgv: real CLI request shape →
// real IPC create handler → injected launcher. Asserts the exact certified
// executable + app-server argv and the absence of any shell/command string.
func TestManagedIPCCreate_StructuredRequest_ExactArgv(t *testing.T) {
	fl := &fakeLauncher{handler: happyAppServer("thread-T1")}
	managed := newTestManagedService(fl)

	resp := ipcCreateRoundTrip(t, managed, sp0DetachCodexRequest())
	if resp["error"] != "" {
		t.Fatalf("managed create failed: %s", resp["error"])
	}
	if !strings.HasPrefix(resp["id"], "codex_app_server:") {
		t.Fatalf("id = %q, want codex_app_server:<local> canonical form", resp["id"])
	}
	if resp["state"] != "running" {
		t.Fatalf("state = %q, want running", resp["state"])
	}

	if fl.callCount() != 1 {
		t.Fatalf("launcher calls = %d, want exactly 1", fl.callCount())
	}
	if fl.exe != "/pinned/toolchain/node_modules/.bin/codex" {
		t.Fatalf("exe = %q, want the pinned certified executable", fl.exe)
	}
	wantArgv := []string{"app-server", "--stdio"}
	if len(fl.argv) != len(wantArgv) || fl.argv[0] != wantArgv[0] || fl.argv[1] != wantArgv[1] {
		t.Fatalf("argv = %v, want exact %v", fl.argv, wantArgv)
	}
	// No shell wrapper, no command string anywhere in the exec.
	for _, tok := range append([]string{fl.exe}, fl.argv...) {
		if tok == "-c" || tok == "sh" || tok == "bash" || strings.HasSuffix(tok, "/sh") || strings.HasSuffix(tok, "/bash") {
			t.Fatalf("shell token %q in exec", tok)
		}
	}

	recs := managed.Registry().List()
	if len(recs) != 1 {
		t.Fatalf("registry records = %d, want 1", len(recs))
	}
	if recs[0].SessionID != resp["id"] || recs[0].NativeStatus != ManagedStatusIdle {
		t.Fatalf("record = %+v, want registered id with idle status", recs[0])
	}
	if recs[0].Provider != "codex" || recs[0].Version != "codex-cli 0.144.1" {
		t.Fatalf("identity binding = %+v", recs[0])
	}
}

// TestManagedIPCCreate_ConflictingInputs_FailClosedBeforeSpawn: any request
// selecting more than one of profile/executable/command is rejected before
// the launcher is ever invoked.
func TestManagedIPCCreate_ConflictingInputs_FailClosedBeforeSpawn(t *testing.T) {
	cases := []map[string]any{
		{"version": 1, "operation": "create", "profileId": "codex", "command": "codex", "detach": true},
		{"version": 1, "operation": "create", "profileId": "codex", "executable": "codex", "detach": true},
		{"version": 1, "operation": "create", "executable": "bash", "command": "echo hi"},
	}
	for i, body := range cases {
		fl := &fakeLauncher{handler: happyAppServer("thread-X")}
		managed := newTestManagedService(fl)
		resp := ipcCreateRoundTrip(t, managed, body)
		if !strings.Contains(resp["error"], "conflicting launch inputs") {
			t.Fatalf("case %d: error = %q, want conflicting-inputs rejection", i, resp["error"])
		}
		if fl.callCount() != 0 {
			t.Fatalf("case %d: launcher invoked %d times before validation", i, fl.callCount())
		}
		if n := len(managed.Registry().List()); n != 0 {
			t.Fatalf("case %d: %d sessions registered after rejected create", i, n)
		}
	}
	// Deepest boundary: toOptions itself rejects the same conflict for every
	// caller, not only the IPC handler.
	if _, err := (localCreateSpec{ProfileID: "codex", Command: json.RawMessage(`"codex"`)}).toOptions(); err == nil {
		t.Fatal("toOptions accepted conflicting profile+command")
	}
}

// TestManagedCreate_FailedVerify_NoChildNoSession: identity verification
// fail-closes BEFORE any spawn.
func TestManagedCreate_FailedVerify_NoChildNoSession(t *testing.T) {
	fl := &fakeLauncher{handler: happyAppServer("thread-V")}
	managed := newTestManagedService(fl)
	managed.verify = func() error { return fmt.Errorf("digest mismatch") }

	if _, err := managed.CreateDetached(""); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("err = %v, want verify failure", err)
	}
	if fl.callCount() != 0 {
		t.Fatalf("launcher invoked despite failed verify")
	}
	if n := len(managed.Registry().List()); n != 0 {
		t.Fatalf("%d sessions registered despite failed verify", n)
	}
}

// TestManagedCreate_FailedInitialize_KillsChildNoSession: a provider error on
// initialize terminates/reaps the child and leaves no visible session.
func TestManagedCreate_FailedInitialize_KillsChildNoSession(t *testing.T) {
	fl := &fakeLauncher{handler: func(method string, id float64, _ map[string]any, out func(map[string]any)) {
		if method == "initialize" {
			out(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32000, "message": "nope"}})
		}
	}}
	managed := newTestManagedService(fl)

	if _, err := managed.CreateDetached(""); err == nil || !strings.Contains(err.Error(), "initialize") {
		t.Fatalf("err = %v, want initialize failure", err)
	}
	select {
	case <-fl.procs[0].killed:
	case <-time.After(2 * time.Second):
		t.Fatal("child not killed after failed initialize")
	}
	if n := len(managed.Registry().List()); n != 0 {
		t.Fatalf("%d sessions registered despite failed initialize", n)
	}
}

// TestManagedCreate_HandshakeDeadline_KillsChildNoSession: a silent provider
// is bounded by the handshake watchdog — the child is killed and no session
// becomes visible.
func TestManagedCreate_HandshakeDeadline_KillsChildNoSession(t *testing.T) {
	fl := &fakeLauncher{handler: func(string, float64, map[string]any, func(map[string]any)) {}}
	managed := newTestManagedService(fl)
	managed.handshakeTimeout = 100 * time.Millisecond

	if _, err := managed.CreateDetached(""); err == nil {
		t.Fatal("expected handshake failure for a silent provider")
	}
	select {
	case <-fl.procs[0].killed:
	case <-time.After(2 * time.Second):
		t.Fatal("child not killed after handshake deadline")
	}
	if n := len(managed.Registry().List()); n != 0 {
		t.Fatalf("%d sessions registered despite handshake deadline", n)
	}
}
