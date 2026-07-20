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
// disjoint from controlled_pty and unreachable through
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

// ManagedProcess is the narrow process handle. OS-specific details stay inside
// the launcher implementation; callers see only stdio, kill/reap, and an
// opaque identity token.
type ManagedProcess interface {
	Stdin() io.Writer
	Stdout() io.Reader
	Term() error // graceful group TERM (Stop's first phase)
	Kill() error // immediate group kill
	Wait() error // reap; safe to call more than once
	OpaqueID() string
	PID() int // OS process ID; never exposed in DTOs
}

// ManagedLauncher is the narrow process-launch seam (injectable for
// deterministic tests). Launch must exec exe with argv directly — never a
// shell, never a command string.
type ManagedLauncher interface {
	Launch(exe string, argv []string) (ManagedProcess, error)
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

func (execLauncher) Launch(exe string, argv []string) (ManagedProcess, error) {
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

func (p *execProcess) Term() error {
	if p.cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM); err != nil {
		return p.cmd.Process.Signal(syscall.SIGTERM)
	}
	return nil
}

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

func (p *execProcess) PID() int { return p.cmd.Process.Pid }
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
	proc      ManagedProcess
	reg       *ManagedSessionRegistry
	scanner   *bufio.Scanner
	writeMu   sync.Mutex
	nextID    int64
	threadID  string
	// events is the per-session bounded projection store (SP0.5). The pump
	// is its only producer.
	events *managedEventStore
	// turnMu guards the SP0.5 one-active-turn input state. The claim is taken
	// under the lock; the provider write happens after release; a failed
	// write rolls the claim back. The pump clears the claim on turn
	// completion or child exit.
	turnMu      sync.Mutex
	turnActive  bool
	turnClosed  bool   // child exited / session stopped: no further prompts
	currentTurn string // provider turnId bound to the active invocation ("" = none)
	pendingReq  int64  // JSON-RPC id of the outstanding turn/start (0 = none)
	// exited is closed at the end of the pump (child EOF + MarkExited + reap
	// started) so lifecycle operations can wait deterministically.
	exited chan struct{}
	// SP1-P1: bounded PRIVATE pending provider approval requests, owned by the
	// pump under turnMu; approvalRejects counts totally-rejected observations
	// (internal diagnostics only). approvals is the non-actionable ingest sink
	// (nil ⇒ observation only); authorityVersion is the pinned grammar-valid
	// canonical version, never the display string.
	pendingApprovals map[int64]pendingProviderRequest
	approvalRejects  int
	approvals        *AuthoritativeApprovalStore
	authorityVersion string
	// actionableActive (SP1-P2B) is the runtime's activation state, copied
	// once from the service at create time — an ACTIVE runtime ingests newly
	// observed certified requests as actionable with certified options and
	// delivery material; an inactive runtime keeps the frozen P1
	// non-actionable observation.
	actionableActive bool
	// SP1-P2A: pump-owned bounded pending-response waiters keyed by the exact
	// native request id. respMu is independent of turnMu (never nested); the
	// pump is the only router; respClosed rejects arming after exit.
	respMu      sync.Mutex
	respClosed  bool
	respWaiters map[int64]*approvalResponseWaiter
	// observer is a NARROW test seam (nil in production): called once per
	// pumped provider message that carries a method, so deterministic tests
	// can prove a crafted message was consumed WITHOUT affecting status.
	observer func(method string)
}

func newCodexManagedRuntime(proc ManagedProcess, epoch int64, reg *ManagedSessionRegistry) *codexManagedRuntime {
	sc := bufio.NewScanner(proc.Stdout())
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	return &codexManagedRuntime{proc: proc, epoch: epoch, reg: reg, scanner: sc, exited: make(chan struct{}),
		pendingApprovals: make(map[int64]pendingProviderRequest),
		respWaiters:      make(map[int64]*approvalResponseWaiter)}
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

// readNext returns the next parseable JSON object from the child's stdout,
// plus the raw line it was decoded from. The raw slice aliases the scanner
// buffer and is valid ONLY until the next readNext call — a consumer that
// retains anything must copy. Malformed lines are skipped — they can never
// influence status.
func (rt *codexManagedRuntime) readNext() (map[string]any, []byte, error) {
	for rt.scanner.Scan() {
		line := bytes.TrimSpace(rt.scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			continue
		}
		return m, line, nil
	}
	if err := rt.scanner.Err(); err != nil {
		return nil, nil, err
	}
	return nil, nil, io.EOF
}

// awaitResult reads until the response for the given request id arrives.
// A JSON-RPC error response fails the call.
func (rt *codexManagedRuntime) awaitResult(id int64) (map[string]any, error) {
	for {
		m, _, err := rt.readNext()
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
// It claims the one-active-turn state so a concurrent prompt conflicts.
func (rt *codexManagedRuntime) startCertificationTurn() error {
	rt.turnMu.Lock()
	rt.turnActive = true
	rt.currentTurn = ""
	rt.nextID++
	id := rt.nextID
	rt.pendingReq = id
	rt.turnMu.Unlock()
	return rt.send(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "turn/start",
		"params": map[string]any{
			"threadId":       rt.threadID,
			"input":          []map[string]any{{"type": "text", "text": certificationPrompt}},
			"approvalPolicy": "untrusted",
		},
	})
}

// turnIDOf extracts the provider turn identity from a notification's params.
// Turn lifecycle events carry it as params.turn.id; item events carry it as a
// top-level params.turnId (per the pinned 0.144.1 schema). Returns "" when
// absent.
func turnIDOf(params map[string]any) string {
	if turn, ok := params["turn"].(map[string]any); ok {
		if id, _ := turn["id"].(string); id != "" {
			return id
		}
	}
	if id, _ := params["turnId"].(string); id != "" {
		return id
	}
	return ""
}

// pump is the production event pump: it maps exact native notifications for
// the bound thread AND the exact current turn onto registry status
// transitions and the bounded event projection (SP0.5 structural allowlist —
// never raw JSON-RPC). Unknown methods, unmatched threads, and stale/
// duplicate/wrong-turn completions are ignored — they can never fabricate a
// known status or release the active turn. On child EOF/error it marks the
// record exited and reaps the child.
func (rt *codexManagedRuntime) pump() {
	for {
		m, raw, err := rt.readNext()
		if err != nil {
			break
		}
		method, _ := m["method"].(string)
		if method == "" {
			// Response objects: the turn/start response carries the provider
			// turn identity (result.turn.id) — the EXACT binding for the
			// invocation we claimed (JSON-RPC id correlation). An error
			// response releases the claim so the session is not stuck.
			if got, ok := m["id"].(float64); ok {
				rt.turnMu.Lock()
				if rt.pendingReq != 0 && int64(got) == rt.pendingReq {
					rt.pendingReq = 0
					if errObj, hasErr := m["error"]; hasErr && errObj != nil {
						rt.turnActive = false
						rt.currentTurn = ""
					} else if res, ok := m["result"].(map[string]any); ok {
						rt.currentTurn = turnIDOf(res)
					}
				}
				rt.turnMu.Unlock()
			}
			continue
		}
		if rt.observer != nil {
			rt.observer(method)
		}
		params, _ := m["params"].(map[string]any)
		tid, _ := params["threadId"].(string)
		if tid != rt.threadID {
			continue
		}
		turnID := turnIDOf(params)
		switch method {
		case "turn/started":
			// Only the EXACT turn bound at turn/start-response time can enter
			// working. A ghost/foreign/late turn/started is inert — there is
			// no first-come adoption.
			rt.turnMu.Lock()
			match := rt.turnActive && turnID != "" && turnID == rt.currentTurn
			rt.turnMu.Unlock()
			if match {
				rt.reg.UpdateNativeStatus(rt.sessionID, rt.epoch, ManagedStatusWorking)
				rt.appendEvent(ManagedEventWorking, "")
			}
		case "turn/completed":
			// Only the EXACT current turn's completion returns us to idle;
			// a stale/duplicate/wrong-turn completion is inert. Completion
			// also drops the turn's pending approval observations and
			// invalidates ONLY their display records (SP1-P1): a request the
			// provider no longer holds open is never shown as current.
			var staleApprovals []string
			rt.turnMu.Lock()
			match := rt.turnActive && turnID != "" && turnID == rt.currentTurn
			if match {
				rt.turnActive = false
				rt.currentTurn = ""
				staleApprovals = rt.clearPendingForTurnLocked(turnID)
			}
			rt.turnMu.Unlock()
			if rt.approvals != nil {
				for _, aid := range staleApprovals {
					rt.approvals.InvalidateRecord(rt.sessionID, aid)
				}
			}
			if match {
				rt.reg.UpdateNativeStatus(rt.sessionID, rt.epoch, ManagedStatusCompleted)
				rt.appendEvent(ManagedEventCompleted, "")
			}
		case "item/completed":
			// Structural allowlist: only the verified assistant message text
			// of the EXACT current turn is projected, byte bounded. All other
			// item variants and wrong-turn items stay internal.
			rt.turnMu.Lock()
			cur := rt.currentTurn
			rt.turnMu.Unlock()
			if turnID == "" || turnID != cur {
				continue
			}
			item, _ := params["item"].(map[string]any)
			if it, _ := item["type"].(string); it == "agentMessage" {
				if text, _ := item["text"].(string); text != "" {
					rt.appendEvent(ManagedEventAssistant, boundUTF8(text, managedEventTextMax))
				}
			}
		case codexApprovalMethod:
			// SP1-P1: strict structured NON-ACTIONABLE observation. The raw
			// line (not the float64 map) carries the lossless top-level id.
			rt.observeApprovalRequest(raw)
		case codexResolvedMethod:
			// SP1-P1: provider-side resolution drops the pending observation
			// only — no commit, no success, no provider write.
			rt.observeApprovalResolved(raw)
		}
	}
	rt.turnMu.Lock()
	rt.turnActive = false
	rt.currentTurn = ""
	rt.turnClosed = true
	rt.pendingApprovals = make(map[int64]pendingProviderRequest)
	rt.turnMu.Unlock()
	// SP1-P2A: child exit deterministically fails every armed delivery waiter
	// and rejects all future arming — a late resolved can never commit.
	rt.closeResponseWaiters()
	rt.reg.MarkExited(rt.sessionID, rt.epoch)
	// SP1-P1: child exit invalidates the session's (non-actionable) approval
	// records — a dead runtime leaves no pending approval display behind.
	if rt.approvals != nil {
		rt.approvals.InvalidateSession(rt.sessionID, "managed child exited")
	}
	rt.appendEvent(ManagedEventExited, "")
	close(rt.exited)
	_ = rt.proc.Wait() // reap
}

func (rt *codexManagedRuntime) appendEvent(kind ManagedEventKind, text string) {
	if rt.events != nil {
		rt.events.append(kind, text)
	}
}

// submitPrompt claims the single active turn and delivers one bounded prompt
// through the owned transport. Claim under lock, provider write after
// release, claim rollback on write failure. A concurrent prompt conflicts
// with ZERO provider write.
func (rt *codexManagedRuntime) submitPrompt(text string) error {
	rt.turnMu.Lock()
	if rt.turnClosed {
		rt.turnMu.Unlock()
		return fmt.Errorf("managed session closed")
	}
	if rt.turnActive {
		rt.turnMu.Unlock()
		return fmt.Errorf("turn already active")
	}
	rt.turnActive = true
	rt.currentTurn = "" // bound below from the provider's turn/start response
	rt.nextID++
	id := rt.nextID
	rt.pendingReq = id
	rt.turnMu.Unlock()

	err := rt.send(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "turn/start",
		"params": map[string]any{
			"threadId":       rt.threadID,
			"input":          []map[string]any{{"type": "text", "text": text}},
			"approvalPolicy": "untrusted",
		},
	})
	if err != nil {
		rt.turnMu.Lock()
		rt.turnActive = false
		rt.pendingReq = 0
		rt.turnMu.Unlock()
		return fmt.Errorf("prompt delivery: %w", err)
	}
	return nil
}

// stop kills and reaps the owned child. Safe to call on any failure path.
func (rt *codexManagedRuntime) stop() {
	_ = rt.proc.Kill()
	_ = rt.proc.Wait()
}

// inflightCreate is a service-owned lease for one CreateDetached call. It is
// registered BEFORE verify/spawn and released on every create exit path, so
// Shutdown can (a) cancel the lease's child even before publication and
// (b) wait until every in-flight create has rolled back or published.
type inflightCreate struct {
	mu        sync.Mutex
	proc      ManagedProcess // nil until spawned
	cancelled bool
}

// setProc hands the spawned child to the lease. Returns false when the lease
// was already cancelled by Shutdown — the caller then owns the rollback.
func (c *inflightCreate) setProc(p ManagedProcess) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancelled {
		return false
	}
	c.proc = p
	return true
}

func (c *inflightCreate) isCancelled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cancelled
}

// cancel marks the lease cancelled and kills its child if one was spawned.
// Reaping stays with the create's own rollback path (or with Shutdown's
// bounded wait for published children).
func (c *inflightCreate) cancel() {
	c.mu.Lock()
	p := c.proc
	c.cancelled = true
	c.mu.Unlock()
	if p != nil {
		_ = p.Kill()
	}
}

// ── Service (composition-root owned) ──

// ManagedCodexService owns the launcher, the owned-session registry, the
// launch-generation counter, and every managed runtime. Constructed by the
// production composition root behind Config.EnableManagedCodex.
type ManagedCodexService struct {
	cfg              CodexAppServerEntryConfig
	launcher         ManagedLauncher
	verify           func() error // fail-closed identity check before every spawn
	handshakeTimeout time.Duration
	reg              *ManagedSessionRegistry

	mu        sync.Mutex
	closing   bool // set once by Shutdown; new/in-flight creates fail closed
	leases    map[*inflightCreate]struct{}
	leaseCond *sync.Cond // broadcast on every lease release (guards: mu)
	gen       int64
	runtimes  map[string]*codexManagedRuntime
	// approvals is the SP1-P1 non-actionable ingest sink, wired once by the
	// composition root before any create (nil ⇒ observation only).
	approvals *AuthoritativeApprovalStore
	// actionable is the SP1-P2B activation state: set ONLY by the single
	// InstallApprovalExecution transition (before the first runtime), copied
	// per-runtime at create, never toggled mid-life.
	actionable bool

	// pumpObserver is a NARROW test seam (nil in production) copied onto each
	// runtime before its pump starts.
	pumpObserver func(sessionID, method string)
	// createBarrier is a NARROW test seam (nil in production) invoked at
	// named points inside CreateDetached so deterministic tests can pause a
	// create and race it against Shutdown.
	createBarrier func(stage string)
}

func (s *ManagedCodexService) barrier(stage string) {
	if s.createBarrier != nil {
		s.createBarrier(stage)
	}
}

// NewManagedCodexService creates the service. launcher nil means the
// production execLauncher. Identity verification runs per create, not here,
// so daemon boot never executes the provider.
func NewManagedCodexService(cfg CodexAppServerEntryConfig, launcher ManagedLauncher) *ManagedCodexService {
	if launcher == nil {
		launcher = execLauncher{}
	}
	s := &ManagedCodexService{
		cfg:              cfg,
		launcher:         launcher,
		verify:           cfg.Verify,
		handshakeTimeout: managedHandshakeTimeout,
		reg:              NewManagedSessionRegistry(maxManagedSessions),
		leases:           make(map[*inflightCreate]struct{}),
		runtimes:         make(map[string]*codexManagedRuntime),
	}
	s.leaseCond = sync.NewCond(&s.mu)
	return s
}

// beginLease registers an in-flight create BEFORE verify/spawn. Fails closed
// once shutdown has begun.
func (s *ManagedCodexService) beginLease() (*inflightCreate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return nil, fmt.Errorf("managed codex service is shutting down")
	}
	lease := &inflightCreate{}
	s.leases[lease] = struct{}{}
	return lease, nil
}

// endLease releases an in-flight create on every exit path and wakes a
// waiting Shutdown.
func (s *ManagedCodexService) endLease(lease *inflightCreate) {
	s.mu.Lock()
	delete(s.leases, lease)
	s.mu.Unlock()
	s.leaseCond.Broadcast()
}

// NewManagedCodexServiceForTest builds a service with an injected launcher
// and verifier override — the deterministic-test seam for production-route
// tests that must not execute the pinned provider binary. Never called by the
// composition root (which always uses NewManagedCodexService + the fail-closed
// pinned Verify).
func NewManagedCodexServiceForTest(launcher ManagedLauncher, verify func() error) *ManagedCodexService {
	s := NewManagedCodexService(CodexAppServerEntryConfig{
		Bin:              "/pinned/test/node_modules/.bin/codex",
		Version:          "codex-cli 0.144.1",
		AuthorityVersion: certifiedCodexAuthorityVersion,
	}, launcher)
	if verify != nil {
		s.verify = verify
	}
	return s
}

// Registry exposes the owned-session registry for the read-only REST surface.
func (s *ManagedCodexService) Registry() *ManagedSessionRegistry { return s.reg }

// AuthorityVersion returns the certified authority version this service was
// configured with. Used by the catalog to cross-validate RuntimeOf results.
func (s *ManagedCodexService) AuthorityVersion() string { return s.cfg.AuthorityVersion }

// SetApprovalStore configures the authoritative approval store as the SP1-P1
// NON-ACTIONABLE observation sink. The store is IMMUTABLE once configured
// (P2B-R1): the ONE canonical store is fixed by the first successful
// configuration or installation, before any runtime exists. Replacement is
// refused after a store is configured, after activation, after the first
// runtime/generation, and after shutdown began — so the handler store, the
// runtime ingest store, and the installed delivery authority can never
// diverge. Re-configuring the SAME store is an idempotent no-op. It grants no
// delivery capacity, no options, and no CTA by itself.
func (s *ManagedCodexService) SetApprovalStore(store *AuthoritativeApprovalStore) error {
	if store == nil {
		return fmt.Errorf("approval store configure: nil store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return fmt.Errorf("approval store configure: service is shutting down")
	}
	if s.approvals == store {
		return nil // idempotent
	}
	if s.actionable {
		return fmt.Errorf("approval store configure: approval execution is installed; the store is immutable")
	}
	if s.gen != 0 || len(s.runtimes) != 0 {
		return fmt.Errorf("approval store configure: a managed runtime already exists; the store is immutable")
	}
	if s.approvals != nil {
		return fmt.Errorf("approval store configure: a different approval store is already configured")
	}
	s.approvals = store
	return nil
}

// ApprovalExecutionInstalled reports whether the atomic activation transition
// has completed. The composition root uses it to distinguish a provably
// observation-only fallback from a pre-activated service it does not own.
func (s *ManagedCodexService) ApprovalExecutionInstalled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.actionable
}

// SubmitPrompt is the ONLY prompt entry (local IPC and mobile REST). It
// validates bounds, binds exact SessionID + current epoch against the live
// runtime and the owned registry, enforces one-active-turn, and delivers the
// prompt only through the owned app-server transport. Every failure is
// fail-closed with ZERO provider write.
func (s *ManagedCodexService) SubmitPrompt(sessionID string, epoch int64, text string) error {
	if err := validateManagedPrompt(text); err != nil {
		return err
	}
	s.mu.Lock()
	rt := s.runtimes[sessionID]
	s.mu.Unlock()
	if rt == nil {
		return fmt.Errorf("managed session not found")
	}
	if rt.epoch != epoch {
		return fmt.Errorf("stale session epoch")
	}
	rec, ok := s.reg.Get(sessionID)
	if !ok || rec.Exited {
		return fmt.Errorf("managed session closed")
	}
	return rt.submitPrompt(text)
}

// eventStoreFor resolves a session's projection store and current epoch.
func (s *ManagedCodexService) eventStoreFor(sessionID string) (*managedEventStore, int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt := s.runtimes[sessionID]
	if rt == nil || rt.events == nil {
		return nil, 0, false
	}
	return rt.events, rt.epoch, true
}

const managedStopGraceful = 2 * time.Second

// lifecycleRuntime resolves and epoch-binds a runtime for a lifecycle op.
func (s *ManagedCodexService) lifecycleRuntime(sessionID string, epoch int64) (*codexManagedRuntime, error) {
	s.mu.Lock()
	rt := s.runtimes[sessionID]
	s.mu.Unlock()
	if rt == nil {
		return nil, fmt.Errorf("managed session not found")
	}
	if rt.epoch != epoch {
		return nil, fmt.Errorf("stale session epoch")
	}
	return rt, nil
}

// closeInput permanently rejects new prompts (Stop/Kill first phase).
func (rt *codexManagedRuntime) closeInput() {
	rt.turnMu.Lock()
	rt.turnClosed = true
	rt.turnMu.Unlock()
}

// awaitExit waits for the pump to observe child exit, bounded.
func (rt *codexManagedRuntime) awaitExit(d time.Duration) bool {
	select {
	case <-rt.exited:
		return true
	case <-time.After(d):
		return false
	}
}

// Stop gracefully terminates a managed session: input closed, group TERM,
// bounded wait, then KILL; reaped; the record is non-current (exited) when
// Stop returns. Idempotent: stopping an already-terminal session succeeds
// without touching the process again.
func (s *ManagedCodexService) Stop(sessionID string, epoch int64) error {
	rt, err := s.lifecycleRuntime(sessionID, epoch)
	if err != nil {
		return err
	}
	rec, ok := s.reg.Get(sessionID)
	if !ok {
		return fmt.Errorf("managed session not found")
	}
	if rec.Exited {
		return nil // already terminal — idempotent success
	}
	rt.closeInput()
	_ = rt.proc.Term()
	if !rt.awaitExit(managedStopGraceful) {
		_ = rt.proc.Kill()
		if !rt.awaitExit(managedStopGraceful) {
			// The pump did not observe exit in the bound — mark non-current
			// directly so a stopped session can never look live (honest
			// escalation; the pump's late MarkExited is then a no-op).
			s.reg.MarkExited(sessionID, epoch)
			return fmt.Errorf("managed session stop: child did not exit within the bound")
		}
	}
	_ = rt.proc.Wait()
	return nil
}

// Kill force-terminates a managed session's process group and reaps it. The
// record is non-current when Kill returns. Idempotent on terminal sessions.
func (s *ManagedCodexService) Kill(sessionID string, epoch int64) error {
	rt, err := s.lifecycleRuntime(sessionID, epoch)
	if err != nil {
		return err
	}
	rec, ok := s.reg.Get(sessionID)
	if !ok {
		return fmt.Errorf("managed session not found")
	}
	if rec.Exited {
		return nil
	}
	rt.closeInput()
	_ = rt.proc.Kill()
	if !rt.awaitExit(managedStopGraceful) {
		s.reg.MarkExited(sessionID, epoch)
		return fmt.Errorf("managed session kill: child did not exit within the bound")
	}
	_ = rt.proc.Wait()
	return nil
}

// Delete removes a TERMINAL managed session: the registry record and the
// bounded output/status data are dropped and the event store is closed —
// later native events and prompts are inert. Deleting a non-terminal session
// fails closed; deleting a deleted session reports not-found without side
// effects.
func (s *ManagedCodexService) Delete(sessionID string, epoch int64) error {
	rec, ok := s.reg.Get(sessionID)
	if !ok {
		return fmt.Errorf("managed session not found")
	}
	if rec.Epoch != epoch {
		return fmt.Errorf("stale session epoch")
	}
	if !rec.Exited {
		return fmt.Errorf("managed session is not terminal: stop or kill it first")
	}
	s.mu.Lock()
	rt := s.runtimes[sessionID]
	delete(s.runtimes, sessionID)
	approvals := s.approvals
	s.mu.Unlock()
	if rt != nil && rt.events != nil {
		rt.events.close()
	}
	s.reg.Remove(sessionID)
	// SP1-P1: the session's approval records are dropped with the session.
	if approvals != nil {
		approvals.Clear(sessionID)
	}
	return nil
}

// CreateDetached is the production managed launch: verify pinned identity →
// direct spawn with the exact app-server argv → bounded handshake → register →
// start the single server-derived certification turn → start the event pump.
// Every failure before the pump kills and reaps the child and leaves no
// visible session.
//
// Linearization with Shutdown: a service-owned in-flight lease is registered
// BEFORE verify/spawn, and s.mu guarding {closing, leases, runtimes} is the
// single transition point. Shutdown cancels every lease (killing a spawned
// child even before publication) and waits — bounded by the caller's ctx —
// until every in-flight create has rolled back or published. No lock is held
// across external I/O.
func (s *ManagedCodexService) CreateDetached(cwd string) (string, error) {
	return s.create(cwd, true)
}

// CreateAttached creates a managed session with NO automatic turn (SP0.5
// interactive path): prompts arrive one-at-a-time through SubmitPrompt.
func (s *ManagedCodexService) CreateAttached(cwd string) (string, error) {
	return s.create(cwd, false)
}

func (s *ManagedCodexService) create(cwd string, certification bool) (string, error) {
	if err := validateCWD(cwd); err != nil {
		return "", err
	}
	// In-flight lease before any external I/O; fail closed once shutdown began.
	lease, err := s.beginLease()
	if err != nil {
		return "", err
	}
	defer s.endLease(lease)

	if err := s.verify(); err != nil {
		return "", fmt.Errorf("managed codex verify: %w", err)
	}
	// Shutdown cancellation received during verify: never spawn.
	if lease.isCancelled() {
		return "", fmt.Errorf("managed codex service is shutting down")
	}
	proc, err := s.launcher.Launch(s.cfg.Bin, []string{"app-server", "--stdio"})
	if err != nil {
		return "", fmt.Errorf("managed codex launch: %w", err)
	}
	// Hand the child to the lease: from here Shutdown can kill it directly.
	if !lease.setProc(proc) {
		// Shutdown cancelled the lease while we were spawning — we own the
		// rollback of this never-visible child.
		_ = proc.Kill()
		_ = proc.Wait()
		return "", fmt.Errorf("managed codex service is shutting down")
	}
	s.barrier("post-spawn")

	id := fmt.Sprintf("%s:%s", codexAppServerAdapter, genLocalID("codex-app"))
	s.mu.Lock()
	if s.closing {
		// Shutdown won the race while we were spawning: it may not have seen
		// the child in the runtime map, but our open lease keeps Shutdown
		// waiting until this rollback completes.
		s.mu.Unlock()
		_ = proc.Kill()
		_ = proc.Wait()
		return "", fmt.Errorf("managed codex service is shutting down")
	}
	s.gen++
	epoch := s.gen
	rt := newCodexManagedRuntime(proc, epoch, s.reg)
	rt.sessionID = id
	rt.events = newManagedEventStore(id, epoch)
	// SP1-P1: copy the observation sink + pinned authority version onto the
	// runtime before its pump can start. SP1-P2B: the activation state is
	// bound to the session/epoch at birth and never toggled mid-life.
	rt.approvals = s.approvals
	rt.authorityVersion = s.cfg.AuthorityVersion
	rt.actionableActive = s.actionable
	s.runtimes[id] = rt // published: from here Shutdown always finds the child
	s.mu.Unlock()

	// fail rolls back a published create: unpublish, drop any registered
	// record, close the event store, kill + reap. Safe against a concurrent
	// Shutdown (delete/Remove are no-ops on already-cleared state; Kill/Wait
	// are idempotent).
	fail := func(stage string, ferr error, registered bool) (string, error) {
		s.mu.Lock()
		delete(s.runtimes, id)
		s.mu.Unlock()
		if registered {
			s.reg.Remove(id)
		}
		rt.events.close()
		rt.stop()
		return "", fmt.Errorf("managed codex %s: %w", stage, ferr)
	}

	if err := rt.handshake(cwd, s.handshakeTimeout); err != nil {
		return fail("handshake", err, false)
	}

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
		return fail("register", err, false)
	}
	s.barrier("post-register")

	if certification {
		if err := rt.startCertificationTurn(); err != nil {
			return fail("turn start", err, true)
		}
	}

	if s.pumpObserver != nil {
		obs := s.pumpObserver
		rt.observer = func(method string) { obs(id, method) }
	}
	go rt.pump()
	return id, nil
}

// Shutdown performs the single closing transition: under s.mu it sets closing
// (new creates fail closed at the lease boundary), snapshots every published
// child, and collects the open in-flight leases. It then closes the registry
// (late pump events and late registrations are rejected), cancels every lease
// — killing a spawned child even before publication — kills every published
// child, and waits, bounded by ctx, until (a) every published child is reaped
// AND (b) every in-flight create has rolled back or failed closed (lease
// count zero). On a nil return: no POKIT-spawned child survives, the runtime
// map and lease set are empty, and the registry is closed. The composition
// root stops IPC accepting BEFORE calling this; a handler goroutine already
// past accept is drained by the lease wait.
func (s *ManagedCodexService) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	leases := make([]*inflightCreate, 0, len(s.leases))
	for l := range s.leases {
		leases = append(leases, l)
	}
	rts := make([]*codexManagedRuntime, 0, len(s.runtimes))
	for _, rt := range s.runtimes {
		rts = append(rts, rt)
	}
	s.runtimes = make(map[string]*codexManagedRuntime)
	s.mu.Unlock()

	s.reg.Close()

	for _, l := range leases {
		l.cancel() // kills spawned-but-unpublished children; marks the rest
	}
	for _, rt := range rts {
		rt.events.close()
		_ = rt.proc.Kill()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, rt := range rts {
			_ = rt.proc.Wait() // reap published children
		}
		// Drain in-flight creates: each rolls back (kill+reap its own child)
		// or fails closed, then releases its lease.
		s.mu.Lock()
		for len(s.leases) > 0 {
			s.leaseCond.Wait()
		}
		s.mu.Unlock()
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("managed codex shutdown drain: %w", ctx.Err())
	}
}
