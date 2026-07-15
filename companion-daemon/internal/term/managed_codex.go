// Package term — SP0-P1: structured detached launch of a POKIT-owned Codex
// app-server child. The daemon directly spawns the pinned certified executable
// with the exact app-server argv (no shell, no PTY, no Recorder); the runtime
// owns stdin, stdout, child wait/reap, and JSONL protocol framing. Native
// protocol events are the sole semantic-status authority (applied through the
// owned ManagedSessionRegistry). Interactive attach, Stop/Kill/Delete REST,
// reconnect, persistence, and approvals are explicitly out of SP0 scope.
package term

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	goruntime "runtime"
	"sync"
	"syscall"
	"time"
)

// codexAppServerAdapter is the canonical-ID adapter segment for managed
// sessions. It is NEVER registered in mux.Registry, so the identity space is
// disjoint from tmux/cmux/localpty/controlled_pty and unreachable through
// adapter discovery.
const codexAppServerAdapter = "codex_app_server"

// certificationPrompt is the fixed server-derived input of the single bounded
// SP0 certification turn. It is a constant owned by the daemon: no user input,
// no command execution requested, so no approval request is expected.
const certificationPrompt = "Reply with exactly the single word READY. Do not run any commands or use any tools."

const (
	managedHandshakeTimeout = 30 * time.Second
	maxManagedSessions      = 4
)

// ── Narrow OS process seam ──

// managedProcess is the narrow process handle. OS-specific details stay inside
// the launcher implementation; callers see only stdio, kill/reap, and an
// opaque identity token.
type managedProcess interface {
	Stdin() io.Writer
	Stdout() io.Reader
	Kill() error
	Wait() error // reap; safe to call more than once
	OpaqueID() string
}

// managedLauncher is the narrow process-launch seam (injectable for
// deterministic tests). Launch must exec exe with argv directly — never a
// shell, never a command string.
type managedLauncher interface {
	Launch(exe string, argv []string) (managedProcess, error)
}

// execLauncher is the production launcher: direct exec of the given
// executable with the exact argv, own process group, stdio pipes owned by the
// runtime. stderr is not consumed (provider diagnostics are not an authority).
type execLauncher struct{}

type execProcess struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	started  time.Time
	waitOnce sync.Once
	waitErr  error
}

func (execLauncher) Launch(exe string, argv []string) (managedProcess, error) {
	cmd := exec.Command(exe, argv...)
	// Own process group so Kill reliably takes the child and any descendants.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &execProcess{cmd: cmd, stdin: stdin, stdout: stdout, started: time.Now()}, nil
}

func (p *execProcess) Stdin() io.Writer  { return p.stdin }
func (p *execProcess) Stdout() io.Reader { return p.stdout }

func (p *execProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	// Kill the whole owned process group; fall back to the direct process.
	if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return p.cmd.Process.Kill()
	}
	return nil
}

func (p *execProcess) Wait() error {
	p.waitOnce.Do(func() { p.waitErr = p.cmd.Wait() })
	return p.waitErr
}

func (p *execProcess) OpaqueID() string {
	pid := 0
	if p.cmd.Process != nil {
		pid = p.cmd.Process.Pid
	}
	return fmt.Sprintf("proc-%d-%d", pid, p.started.UnixNano())
}

// ── Managed runtime (one per session) ──

// codexManagedRuntime owns exactly one app-server child: its stdio, protocol
// framing, handshake, and the single event-pump goroutine. Nothing else may
// touch the child's stdio.
type codexManagedRuntime struct {
	sessionID string
	epoch     int64
	proc      managedProcess
	reg       *ManagedSessionRegistry
	scanner   *bufio.Scanner
	writeMu   sync.Mutex
	nextID    int64
	threadID  string
}

func newCodexManagedRuntime(proc managedProcess, epoch int64, reg *ManagedSessionRegistry) *codexManagedRuntime {
	sc := bufio.NewScanner(proc.Stdout())
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	return &codexManagedRuntime{proc: proc, epoch: epoch, reg: reg, scanner: sc}
}

// send writes one JSON-RPC object as a JSONL line.
func (rt *codexManagedRuntime) send(obj map[string]any) error {
	b, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	rt.writeMu.Lock()
	defer rt.writeMu.Unlock()
	_, err = rt.proc.Stdin().Write(append(b, '\n'))
	return err
}

// readNext returns the next parseable JSON object from the child's stdout.
// Malformed lines are skipped — they can never influence status.
func (rt *codexManagedRuntime) readNext() (map[string]any, error) {
	for rt.scanner.Scan() {
		line := bytes.TrimSpace(rt.scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			continue
		}
		return m, nil
	}
	if err := rt.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

// awaitResult reads until the response for the given request id arrives.
// A JSON-RPC error response fails the call.
func (rt *codexManagedRuntime) awaitResult(id int64) (map[string]any, error) {
	for {
		m, err := rt.readNext()
		if err != nil {
			return nil, err
		}
		got, ok := m["id"].(float64)
		if !ok || int64(got) != id {
			continue // notification or unrelated response — ignore
		}
		if errObj, hasErr := m["error"]; hasErr && errObj != nil {
			return nil, fmt.Errorf("provider error for request %d", id)
		}
		res, _ := m["result"].(map[string]any)
		return res, nil
	}
}

func (rt *codexManagedRuntime) call(method string, params map[string]any) (map[string]any, error) {
	rt.nextID++
	id := rt.nextID
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	if err := rt.send(req); err != nil {
		return nil, err
	}
	return rt.awaitResult(id)
}

// handshake performs initialize → initialized → thread/start with a bounded
// wall-clock deadline. On deadline the child is killed, which fails the
// in-flight read. The request/param shapes are evidence-exact (CP0 report 3).
func (rt *codexManagedRuntime) handshake(cwd string, timeout time.Duration) error {
	watchdog := time.AfterFunc(timeout, func() { _ = rt.proc.Kill() })
	defer watchdog.Stop()

	if _, err := rt.call("initialize", map[string]any{
		"clientInfo": map[string]any{"name": "pokit-sp0", "version": "0.0.0"},
	}); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if err := rt.send(map[string]any{"jsonrpc": "2.0", "method": "initialized"}); err != nil {
		return fmt.Errorf("initialized: %w", err)
	}
	res, err := rt.call("thread/start", map[string]any{
		"approvalPolicy": "untrusted",
		"cwd":            cwd,
		"config":         map[string]any{"sandbox_mode": "read-only"},
	})
	if err != nil {
		return fmt.Errorf("thread/start: %w", err)
	}
	tid, _ := res["threadId"].(string)
	if tid == "" {
		if th, ok := res["thread"].(map[string]any); ok {
			tid, _ = th["id"].(string)
		}
	}
	if tid == "" {
		return fmt.Errorf("thread/start: no threadId in response")
	}
	rt.threadID = tid
	return nil
}

// startCertificationTurn sends the single bounded server-derived turn. The
// input is the fixed constant — closed vocabulary, no caller-supplied text.
func (rt *codexManagedRuntime) startCertificationTurn() error {
	rt.nextID++
	return rt.send(map[string]any{
		"jsonrpc": "2.0", "id": rt.nextID, "method": "turn/start",
		"params": map[string]any{
			"threadId":       rt.threadID,
			"input":          []map[string]any{{"type": "text", "text": certificationPrompt}},
			"approvalPolicy": "untrusted",
		},
	})
}

// pump is the production event pump: it maps exact native notifications for
// the bound thread onto registry status transitions. Unknown methods,
// unmatched threads, and response objects are ignored — they can never
// fabricate a known status. On child EOF/error it marks the record exited and
// reaps the child.
func (rt *codexManagedRuntime) pump() {
	for {
		m, err := rt.readNext()
		if err != nil {
			break
		}
		method, _ := m["method"].(string)
		if method == "" {
			continue
		}
		params, _ := m["params"].(map[string]any)
		tid, _ := params["threadId"].(string)
		if tid != rt.threadID {
			continue
		}
		switch method {
		case "turn/started":
			rt.reg.UpdateNativeStatus(rt.sessionID, rt.epoch, ManagedStatusWorking)
		case "turn/completed":
			rt.reg.UpdateNativeStatus(rt.sessionID, rt.epoch, ManagedStatusCompleted)
		}
	}
	rt.reg.MarkExited(rt.sessionID, rt.epoch)
	_ = rt.proc.Wait() // reap
}

// stop kills and reaps the owned child. Safe to call on any failure path.
func (rt *codexManagedRuntime) stop() {
	_ = rt.proc.Kill()
	_ = rt.proc.Wait()
}

// ── Service (composition-root owned) ──

// ManagedCodexService owns the launcher, the owned-session registry, the
// launch-generation counter, and every managed runtime. Constructed by the
// production composition root behind Config.EnableManagedCodex.
type ManagedCodexService struct {
	cfg              CodexAppServerEntryConfig
	launcher         managedLauncher
	verify           func() error // fail-closed identity check before every spawn
	handshakeTimeout time.Duration
	reg              *ManagedSessionRegistry

	mu       sync.Mutex
	gen      int64
	runtimes map[string]*codexManagedRuntime
}

// NewManagedCodexService creates the service. launcher nil means the
// production execLauncher. Identity verification runs per create, not here,
// so daemon boot never executes the provider.
func NewManagedCodexService(cfg CodexAppServerEntryConfig, launcher managedLauncher) *ManagedCodexService {
	if launcher == nil {
		launcher = execLauncher{}
	}
	return &ManagedCodexService{
		cfg:              cfg,
		launcher:         launcher,
		verify:           cfg.Verify,
		handshakeTimeout: managedHandshakeTimeout,
		reg:              NewManagedSessionRegistry(maxManagedSessions),
		runtimes:         make(map[string]*codexManagedRuntime),
	}
}

// Registry exposes the owned-session registry for the read-only REST surface.
func (s *ManagedCodexService) Registry() *ManagedSessionRegistry { return s.reg }

// CreateDetached is the production managed launch: verify pinned identity →
// direct spawn with the exact app-server argv → bounded handshake → register →
// start the single server-derived certification turn → start the event pump.
// Every failure before the pump kills and reaps the child and leaves no
// visible session.
func (s *ManagedCodexService) CreateDetached(cwd string) (string, error) {
	if err := validateCWD(cwd); err != nil {
		return "", err
	}
	if err := s.verify(); err != nil {
		return "", fmt.Errorf("managed codex verify: %w", err)
	}
	proc, err := s.launcher.Launch(s.cfg.Bin, []string{"app-server", "--stdio"})
	if err != nil {
		return "", fmt.Errorf("managed codex launch: %w", err)
	}

	s.mu.Lock()
	s.gen++
	epoch := s.gen
	s.mu.Unlock()

	rt := newCodexManagedRuntime(proc, epoch, s.reg)
	if err := rt.handshake(cwd, s.handshakeTimeout); err != nil {
		rt.stop()
		return "", fmt.Errorf("managed codex handshake: %w", err)
	}

	id := fmt.Sprintf("%s:%s", codexAppServerAdapter, genLocalID("codex-app"))
	rt.sessionID = id
	rec := ManagedSessionRecord{
		SessionID: id,
		Provider:  "codex",
		Version:   s.cfg.Version,
		Epoch:     epoch,
		ProcessID: proc.OpaqueID(),
		OS:        goruntime.GOOS,
		Arch:      goruntime.GOARCH,
		CreatedAt: time.Now(),
	}
	if err := s.reg.Register(rec); err != nil {
		rt.stop()
		return "", fmt.Errorf("managed codex register: %w", err)
	}

	if err := rt.startCertificationTurn(); err != nil {
		s.reg.Remove(id)
		rt.stop()
		return "", fmt.Errorf("managed codex turn start: %w", err)
	}

	s.mu.Lock()
	s.runtimes[id] = rt
	s.mu.Unlock()

	go rt.pump()
	return id, nil
}

// Shutdown closes the registry first (late pump events are rejected), then
// kills every owned child and reaps them with the caller's bounded deadline.
func (s *ManagedCodexService) Shutdown(ctx context.Context) error {
	s.reg.Close()
	s.mu.Lock()
	rts := make([]*codexManagedRuntime, 0, len(s.runtimes))
	for _, rt := range s.runtimes {
		rts = append(rts, rt)
	}
	s.runtimes = make(map[string]*codexManagedRuntime)
	s.mu.Unlock()

	for _, rt := range rts {
		_ = rt.proc.Kill()
	}
	done := make(chan struct{})
	go func() {
		for _, rt := range rts {
			_ = rt.proc.Wait()
		}
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("managed codex shutdown reap: %w", ctx.Err())
	}
}
