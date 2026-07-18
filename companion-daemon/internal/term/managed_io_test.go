package term

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── SP0.5-A fakes ──

// interactiveAppServer is a scripted provider for prompt-driven sessions: it
// records every turn/start (params + count), emits turn/started immediately,
// and emits agentMessage + turn/completed when the per-turn gate allows.
type interactiveAppServer struct {
	threadID string
	mu       sync.Mutex
	turns    []map[string]any
	// hold, when non-nil, delays item/completed + turn/completed until the
	// channel is closed (one-shot; later turns complete immediately).
	hold  chan struct{}
	reply string
}

func (s *interactiveAppServer) turnCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.turns)
}

func (s *interactiveAppServer) turnParams(i int) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turns[i]
}

func (s *interactiveAppServer) handler() scriptHandler {
	return func(method string, id float64, params map[string]any, out func(map[string]any)) {
		switch method {
		case "initialize":
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
		case "thread/start":
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"threadId": s.threadID}})
		case "turn/start":
			s.mu.Lock()
			s.turns = append(s.turns, params)
			n := len(s.turns)
			hold := s.hold
			s.hold = nil
			reply := s.reply
			s.mu.Unlock()
			// Schema-exact (pinned 0.144.1): turn lifecycle notifications
			// nest the identity as params.turn.id; item notifications carry
			// a top-level params.turnId.
			turnID := fmt.Sprintf("turn-%d", n)
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"turn": map[string]any{"id": turnID}}})
			out(map[string]any{"jsonrpc": "2.0", "method": "turn/started", "params": map[string]any{
				"threadId": s.threadID, "turn": map[string]any{"id": turnID}}})
			go func() {
				if hold != nil {
					<-hold
				}
				if reply != "" {
					out(map[string]any{"jsonrpc": "2.0", "method": "item/completed", "params": map[string]any{
						"threadId": s.threadID, "turnId": turnID,
						"item": map[string]any{"type": "agentMessage", "id": "item-i", "text": reply},
					}})
				}
				out(map[string]any{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{
					"threadId": s.threadID, "turn": map[string]any{"id": turnID}}})
			}()
		}
	}
}

// sp05InteractiveRequest mirrors buildRunCreateRequest for non-detached
// `pokit run codex` (structured, no command string, no detach claim).
func sp05InteractiveRequest() map[string]any {
	return map[string]any{"version": 1, "operation": "create", "profileId": "codex", "cwd": ""}
}

func newInteractiveService(t *testing.T, app *interactiveAppServer) (*ManagedCodexService, *fakeLauncher) {
	t.Helper()
	fl := &fakeLauncher{handler: app.handler()}
	return newTestManagedService(fl), fl
}

// attachClient dials handleManagedAttach through the real IPC connection
// handler and returns helpers for reading lines and sending prompts.
type attachClient struct {
	t    *testing.T
	conn net.Conn
	sc   *bufio.Scanner
	raw  []string // every line received (for leak scans)
}

func newAttachClient(t *testing.T, managed *ManagedCodexService, sessionID string) *attachClient {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	go handleIPCConnection(serverConn, nil, nil, nil, nil, nil, managed, nil)
	req, _ := json.Marshal(map[string]any{"version": 1, "operation": "managed-attach", "sessionId": sessionID})
	if _, err := clientConn.Write(append(req, '\n')); err != nil {
		t.Fatalf("attach write: %v", err)
	}
	sc := bufio.NewScanner(clientConn)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	return &attachClient{t: t, conn: clientConn, sc: sc}
}

func (c *attachClient) sendPrompt(text string) {
	c.t.Helper()
	payload, _ := json.Marshal(map[string]string{"prompt": text})
	if _, err := c.conn.Write(append(payload, '\n')); err != nil {
		c.t.Fatalf("prompt write: %v", err)
	}
}

// next reads lines until one matching kind or error arrives (bounded).
func (c *attachClient) next(wantKinds ...string) map[string]string {
	c.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !c.sc.Scan() {
			c.t.Fatalf("attach stream ended waiting for %v (err=%v)", wantKinds, c.sc.Err())
		}
		line := c.sc.Text()
		c.raw = append(c.raw, line)
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		got := map[string]string{}
		for k, v := range m {
			if s, ok := v.(string); ok {
				got[k] = s
			}
		}
		if got["error"] != "" {
			return got
		}
		for _, k := range wantKinds {
			if got["kind"] == k {
				return got
			}
		}
	}
	c.t.Fatalf("timeout waiting for %v", wantKinds)
	return nil
}

// ── SP0.5-A focused tests ──

// TestManagedInteractiveCreate_StructuredNoShellNoAutoTurn: the exact
// non-detached CLI request creates the SAME managed service session, direct
// exec (no shell, no controlled PTY), and starts NO automatic turn.
func TestManagedInteractiveCreate_StructuredNoShellNoAutoTurn(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-A5", reply: "hi"}
	managed, fl := newInteractiveService(t, app)

	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	if resp["error"] != "" || !strings.HasPrefix(resp["id"], "codex_app_server:") {
		t.Fatalf("interactive create = %v", resp)
	}
	if fl.callCount() != 1 || fl.exe != "/pinned/toolchain/node_modules/.bin/codex" {
		t.Fatalf("launcher calls=%d exe=%q", fl.callCount(), fl.exe)
	}
	if len(fl.argv) != 2 || fl.argv[0] != "app-server" || fl.argv[1] != "--stdio" {
		t.Fatalf("argv = %v", fl.argv)
	}
	// No automatic turn for interactive sessions.
	time.Sleep(50 * time.Millisecond)
	if n := app.turnCount(); n != 0 {
		t.Fatalf("interactive create started %d automatic turns", n)
	}
	rec, _ := managed.Registry().Get(resp["id"])
	if rec.NativeStatus != ManagedStatusIdle {
		t.Fatalf("status = %s", rec.NativeStatus)
	}
}

// TestManagedIPCCreate_LegacyCodexCommandRemoved: the exact legacy command
// string "codex" is no longer shell-executed — explicit rejection, no session.
func TestManagedIPCCreate_LegacyCodexCommandRemoved(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-A6"}
	managed, fl := newInteractiveService(t, app)
	resp := ipcRoundTripWith(t, nil, managed, map[string]any{
		"version": 1, "operation": "create", "command": " codex ",
	})
	if !strings.Contains(resp["error"], "structured codex profile") {
		t.Fatalf("error = %q, want legacy-codex rejection", resp["error"])
	}
	if fl.callCount() != 0 || len(managed.Registry().List()) != 0 {
		t.Fatal("legacy codex command reached a launcher or registry")
	}
}

// TestManagedAttach_PromptDrivesExactTurnStart: one bounded prompt over the
// real attach path produces EXACTLY one native turn/start bound to the
// session thread, and the projected stream returns working → assistant →
// completed without ever echoing the prompt or raw protocol.
func TestManagedAttach_PromptDrivesExactTurnStart(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-A7", reply: "READY-REPLY"}
	managed, _ := newInteractiveService(t, app)
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	id := resp["id"]

	c := newAttachClient(t, managed, id)
	defer c.conn.Close()
	c.sendPrompt("what is the answer")

	if ev := c.next("working"); ev["error"] != "" {
		t.Fatalf("working: %v", ev)
	}
	ev := c.next("assistant")
	if ev["text"] != "READY-REPLY" {
		t.Fatalf("assistant = %v", ev)
	}
	c.next("completed")

	// Exact native request shape.
	if app.turnCount() != 1 {
		t.Fatalf("turn/start count = %d", app.turnCount())
	}
	p := app.turnParams(0)
	if p["threadId"] != "thread-A7" || p["approvalPolicy"] != "untrusted" {
		t.Fatalf("turn params = %v", p)
	}
	input, _ := p["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("input = %v", input)
	}
	first, _ := input[0].(map[string]any)
	if first["type"] != "text" || first["text"] != "what is the answer" {
		t.Fatalf("input[0] = %v", first)
	}

	// Privacy: the prompt is never echoed back on the public stream, and no
	// raw JSON-RPC fields leak into it.
	for _, line := range c.raw {
		for _, leak := range []string{"what is the answer", "jsonrpc", "threadId", "turn-1", "item-i"} {
			if strings.Contains(line, leak) {
				t.Fatalf("attach stream leaks %q: %s", leak, line)
			}
		}
	}
}

// TestManagedPrompt_SecondWhileActiveConflicts: one active turn per session —
// a concurrent prompt conflicts with ZERO provider write, and a new prompt
// succeeds after completion.
func TestManagedPrompt_SecondWhileActiveConflicts(t *testing.T) {
	gate := make(chan struct{})
	app := &interactiveAppServer{threadID: "thread-A8", reply: "r1", hold: gate}
	managed, _ := newInteractiveService(t, app)
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	id, epoch := resp["id"], int64(1)

	if err := managed.SubmitPrompt(id, epoch, "first"); err != nil {
		t.Fatalf("first prompt: %v", err)
	}
	if err := managed.SubmitPrompt(id, epoch, "second"); err == nil || !strings.Contains(err.Error(), "turn already active") {
		t.Fatalf("second prompt err = %v, want conflict", err)
	}
	// The fake consumes the first turn/start asynchronously — wait for the
	// exactly-one write, then confirm the conflict added none.
	deadline := time.Now().Add(2 * time.Second)
	for app.turnCount() != 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if app.turnCount() != 1 {
		t.Fatalf("provider writes = %d after conflict, want 1", app.turnCount())
	}

	close(gate) // finish the first turn
	waitForStatus(t, managed.Registry(), id, ManagedStatusCompleted)
	if err := managed.SubmitPrompt(id, epoch, "third"); err != nil {
		t.Fatalf("post-completion prompt: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for app.turnCount() != 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if app.turnCount() != 2 {
		t.Fatalf("provider writes = %d after third prompt", app.turnCount())
	}
}

// TestManagedPrompt_BoundsFailClosed: malformed, oversized, non-UTF-8,
// control-byte, and multiline prompts fail closed with zero provider writes.
func TestManagedPrompt_BoundsFailClosed(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-A9"}
	managed, _ := newInteractiveService(t, app)
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	id := resp["id"]

	bad := []string{
		"",
		"   ",
		strings.Repeat("x", managedPromptMaxBytes+1),
		"bad\xffutf8",
		"has\x01control",
		"two\nlines",
	}
	for i, text := range bad {
		if err := managed.SubmitPrompt(id, 1, text); err == nil {
			t.Fatalf("case %d accepted invalid prompt", i)
		}
	}
	if app.turnCount() != 0 {
		t.Fatalf("provider writes = %d for invalid prompts", app.turnCount())
	}
}

// TestManagedPrompt_WrongBindingZeroWrites: unknown session, stale epoch, and
// a closed (exited) session all fail closed with zero provider writes.
func TestManagedPrompt_WrongBindingZeroWrites(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-A10"}
	managed, fl := newInteractiveService(t, app)
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	id := resp["id"]

	if err := managed.SubmitPrompt("codex_app_server:nope", 1, "hello"); err == nil {
		t.Fatal("unknown session accepted")
	}
	if err := managed.SubmitPrompt(id, 99, "hello"); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale epoch err = %v", err)
	}

	fl.procs[0].Kill() // child dies
	waitForStatus(t, managed.Registry(), id, ManagedStatusExited)
	if err := managed.SubmitPrompt(id, 1, "hello"); err == nil {
		t.Fatal("closed session accepted a prompt")
	}
	if app.turnCount() != 0 {
		t.Fatalf("provider writes = %d", app.turnCount())
	}
}

// TestManagedAttach_DetachDoesNotKillRuntime: closing the local viewer leaves
// the runtime and registry record fully alive.
func TestManagedAttach_DetachDoesNotKillRuntime(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-A11", reply: "r"}
	managed, fl := newInteractiveService(t, app)
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	id := resp["id"]

	c := newAttachClient(t, managed, id)
	c.conn.Close() // Ctrl-D / viewer gone
	time.Sleep(50 * time.Millisecond)

	select {
	case <-fl.procs[0].killed:
		t.Fatal("detach killed the managed child")
	default:
	}
	rec, ok := managed.Registry().Get(id)
	if !ok || rec.Exited {
		t.Fatalf("record after detach = %+v ok=%v", rec, ok)
	}
	// The session still accepts a prompt after the viewer detached.
	if err := managed.SubmitPrompt(id, 1, "still alive"); err != nil {
		t.Fatalf("post-detach prompt: %v", err)
	}
}

// TestManagedAttach_CoalescedRequestAndPrompt: the attach request and an
// early prompt arriving in ONE socket read (kernel write coalescing) must not
// lose the prompt to the request decoder's internal buffer. Regression for
// the live-run failure where the first prompt was silently swallowed.
func TestManagedAttach_CoalescedRequestAndPrompt(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-A13", reply: "coalesced-ok"}
	managed, _ := newInteractiveService(t, app)
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	id := resp["id"]

	clientConn, serverConn := net.Pipe()
	go handleIPCConnection(serverConn, nil, nil, nil, nil, nil, managed, nil)
	attach, _ := json.Marshal(map[string]any{"version": 1, "operation": "managed-attach", "sessionId": id})
	prompt, _ := json.Marshal(map[string]string{"prompt": "early prompt"})
	// ONE write carrying both lines — the decoder buffers past the request.
	both := append(append(attach, '\n'), append(prompt, '\n')...)
	if _, err := clientConn.Write(both); err != nil {
		t.Fatalf("coalesced write: %v", err)
	}
	c := &attachClient{t: t, conn: clientConn, sc: bufio.NewScanner(clientConn)}
	c.sc.Buffer(make([]byte, 64*1024), 1024*1024)
	defer clientConn.Close()

	c.next("working")
	if ev := c.next("assistant"); ev["text"] != "coalesced-ok" {
		t.Fatalf("assistant = %v", ev)
	}
	c.next("completed")
	if app.turnCount() != 1 {
		t.Fatalf("turn/start count = %d, want 1 (prompt lost to decoder buffer?)", app.turnCount())
	}
}

// TestManagedAttach_WrongSessionFailsClosed: attaching to an unknown session
// yields an explicit error and no stream.
func TestManagedAttach_WrongSessionFailsClosed(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-A12"}
	managed, _ := newInteractiveService(t, app)
	c := newAttachClient(t, managed, "codex_app_server:missing")
	defer c.conn.Close()
	ev := c.next() // first line must be the error
	if !strings.Contains(ev["error"], "not found") {
		t.Fatalf("attach error = %v", ev)
	}
}
