// Package term — C1D: structured detached launch of a POKIT-owned Claude Code
// child. The daemon directly spawns the pinned certified executable with
// session-isolated hook settings and a daemon-owned private hook bridge. The
// runtime owns stdin, stdout, child wait/reap, and the hook bridge lifecycle.
// C1D is observation-only: zero actionable options, zero ClaimForExecution,
// zero mobile CTA.
package term

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// claudeHeadlessAdapter is the canonical-ID adapter segment for managed Claude
// sessions. It is NEVER registered in mux.Registry, so the identity space is
// disjoint from tmux/cmux/localpty/controlled_pty/codex_app_server and
// unreachable through adapter discovery.
const claudeHeadlessAdapter = "claude_headless"

// claudeCertificationPrompt is the fixed server-derived input of the single
// bounded C1D certification turn.
const claudeCertificationPrompt = "Reply with exactly the single word READY. Do not run any commands or use any tools."

const (
	claudeHandshakeTimeout = 30 * time.Second
	maxClaudeSessions      = 4
	// maxPendingClaudeObservations bounds the private per-runtime pending
	// observation state.
	maxPendingClaudeObservations = 4
	// claudeObservationTimeout bounds how long a pending observation waits for
	// a matching tool_deferred result before being cleared.
	claudeObservationTimeout = 120 * time.Second
)

// ── Claude-specific managed runtime (one per session) ──

// claudePendingObservation is the bounded PRIVATE runtime-owned record of one
// observed PreToolUse event. It is owned by the pump and never enters any DTO,
// log line, or public projection.
type claudePendingObservation struct {
	toolUseID    string
	toolName     string
	sessionID    string // Claude session ID from the hook
	inputDigest  string // SHA-256 hex
	observedAt   time.Time
	joinedAt     time.Time // zero until joined with tool_deferred
}

// claudeManagedRuntime owns exactly one Claude child: its stdio, hook bridge,
// event pump, and pending observations. Nothing else may touch the child's
// stdio or hook settings.
type claudeManagedRuntime struct {
	sessionID string
	epoch     int64
	proc      ManagedProcess
	reg       *ManagedSessionRegistry
	scanner   *bufio.Scanner
	exited    chan struct{}

	// bridge is the private hook bridge for this runtime.
	bridge *claudeHookBridge
	// hookDir is the temporary directory holding isolated hook settings.
	hookDir string

	// turnMu guards pending observations and turn state.
	turnMu             sync.Mutex
	turnClosed         bool
	pendingObservations map[string]*claudePendingObservation // keyed by tool_use_id
	rejects            int // internal diagnostics counter

	// approvals is the non-actionable ingest sink (nil ⇒ observation only).
	approvals       *AuthoritativeApprovalStore
	authorityVersion string

	// observer is a NARROW test seam (nil in production).
	observer func(stage string)
}

func newClaudeManagedRuntime(proc ManagedProcess, epoch int64, reg *ManagedSessionRegistry, bridge *claudeHookBridge, hookDir string) *claudeManagedRuntime {
	sc := bufio.NewScanner(proc.Stdout())
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	rt := &claudeManagedRuntime{
		proc:                proc,
		epoch:               epoch,
		reg:                 reg,
		scanner:             sc,
		exited:              make(chan struct{}),
		bridge:              bridge,
		hookDir:             hookDir,
		pendingObservations: make(map[string]*claudePendingObservation),
	}
	// Wire the bridge back to this runtime.
	bridge.rt = rt
	return rt
}

// observePreToolUse is called by the hook bridge when a valid PreToolUse event
// is received. It stores a pending observation under turnMu. Only the first
// observation for a given tool_use_id is stored; duplicates are rejected.
func (rt *claudeManagedRuntime) observePreToolUse(toolUseID, toolName, claudeSessionID, inputDigest string) {
	rt.turnMu.Lock()
	defer rt.turnMu.Unlock()

	if rt.turnClosed {
		rt.rejects++
		return
	}
	if _, dup := rt.pendingObservations[toolUseID]; dup {
		rt.rejects++
		return
	}
	if len(rt.pendingObservations) >= maxPendingClaudeObservations {
		rt.rejects++
		return
	}

	rt.pendingObservations[toolUseID] = &claudePendingObservation{
		toolUseID:   toolUseID,
		toolName:    toolName,
		sessionID:   claudeSessionID,
		inputDigest: inputDigest,
		observedAt:  time.Now(),
	}

	if rt.observer != nil {
		rt.observer("pre_tool_use")
	}
}

// joinDeferred attempts to match a tool_deferred result from Claude's stdout
// against a pending observation. On match, a non-actionable record is ingested
// into the ApprovalStore and the pending observation is cleared.
func (rt *claudeManagedRuntime) joinDeferred(toolUseID, sessionID string) {
	rt.turnMu.Lock()
	pending, ok := rt.pendingObservations[toolUseID]
	if !ok || pending.sessionID != sessionID {
		rt.turnMu.Unlock()
		return
	}
	delete(rt.pendingObservations, toolUseID)
	approvals := rt.approvals
	rt.turnMu.Unlock()

	if approvals == nil {
		return
	}

	// Derive a canonical public approval ID from epoch + tool_use_id.
	// The provider tool_use_id is never used as the ApprovalID.
	approvalID := fmt.Sprintf("claude-%d-%s", rt.epoch, toolUseID)

	// Non-actionable: zero options, zero delivery material, zero required perm.
	approvals.IngestObserved(ApprovalIngest{
		SessionID: rt.sessionID,
		LaunchGen: rt.epoch,
		StreamGen: 0,
		Provider:  claudeHeadlessAdapter,
		Version:   rt.authorityVersion,
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID:         approvalID,
				SessionID:  rt.sessionID,
				AgentKind:  claudeHeadlessAdapter,
				Kind:       "approval",
				Status:     "pending",
				Options:    nil, // ZERO options — non-actionable
				Source:     agent.SourceJSONL,
				Confidence: 1,
			},
			// Provenance is versioned provider protocol (C0D-certified lifecycle).
			Provenance:       contract.Provenance("claude.pretooluse.defer.v1"),
			Actionable:       false,
			RequiredPerm:     "",
			DeliveryMaterial: nil,
		}},
	})

	if rt.observer != nil {
		rt.observer("deferred_joined")
	}
}

// pump is the production event pump. It reads Claude's stdout line by line,
// looking for structured JSON events. When a tool_deferred result is found,
// it joins against pending observations from the hook bridge. On child EOF
// or error, it marks the record exited and reaps the child.
func (rt *claudeManagedRuntime) pump() {
	// Start a background goroutine that cleans up stale observations after
	// the timeout.
	staleCheck := time.NewTicker(30 * time.Second)
	defer staleCheck.Stop()
	go func() {
		for range staleCheck.C {
			rt.clearStaleObservations()
		}
	}()

	for rt.scanner.Scan() {
		line := bytes.TrimSpace(rt.scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		// Claude may emit JSON events on stdout. Look for tool_deferred.
		// The exact format is provider-version-specific; we decode
		// structurally with a minimal allowlist.
		rt.processLine(line)
	}

	// Child exited: clear all pending state.
	rt.turnMu.Lock()
	rt.turnClosed = true
	rt.pendingObservations = make(map[string]*claudePendingObservation)
	rt.turnMu.Unlock()

	if rt.approvals != nil {
		rt.approvals.InvalidateSession(rt.sessionID, "managed claude child exited")
	}
	rt.reg.MarkExited(rt.sessionID, rt.epoch)
	close(rt.exited)
	_ = rt.proc.Wait() // reap
}

// processLine attempts to parse a stdout line as a JSON event and extract
// deferred tool information for join.
func (rt *claudeManagedRuntime) processLine(line []byte) {
	var event struct {
		Type       string `json:"type"`
		ToolUseID  string `json:"tool_use_id"`
		SessionID  string `json:"session_id"`
		ToolName   string `json:"tool_name"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		return
	}

	// Match tool_deferred events: type is "tool_deferred" or the event
	// carries a tool_use_id that matches a pending observation.
	if event.ToolUseID == "" {
		return
	}
	if event.SessionID == "" {
		return
	}

	// Valid deferred result: join with pending observation.
	if event.Type == "tool_deferred" || event.Type == "assistant" {
		rt.joinDeferred(event.ToolUseID, event.SessionID)
	}
}

// clearStaleObservations removes pending observations that have exceeded the
// timeout without a matching tool_deferred result.
func (rt *claudeManagedRuntime) clearStaleObservations() {
	rt.turnMu.Lock()
	defer rt.turnMu.Unlock()

	cutoff := time.Now().Add(-claudeObservationTimeout)
	for id, obs := range rt.pendingObservations {
		if obs.observedAt.Before(cutoff) {
			delete(rt.pendingObservations, id)
			if rt.approvals != nil {
				approvalID := fmt.Sprintf("claude-%d-%s", rt.epoch, obs.toolUseID)
				rt.approvals.InvalidateRecord(rt.sessionID, approvalID)
			}
		}
	}
}

// stop kills and reaps the owned child and closes the hook bridge.
func (rt *claudeManagedRuntime) stop() {
	if rt.bridge != nil {
		rt.bridge.close()
	}
	_ = rt.proc.Kill()
	_ = rt.proc.Wait()
	// Clean up the temporary hook directory.
	if rt.hookDir != "" {
		_ = os.RemoveAll(rt.hookDir)
	}
}

// ── Service (composition-root owned) ──

// ManagedClaudeService owns the launcher, the owned-session registry, the
// launch-generation counter, and every managed Claude runtime. Constructed
// by the production composition root behind Config.EnableManagedClaude.
type ManagedClaudeService struct {
	cfg              ClaudeEntryConfig
	launcher         ManagedLauncher
	attestor         ClaudeAttestor
	handshakeTimeout time.Duration
	reg              *ManagedSessionRegistry

	mu        sync.Mutex
	closing   bool
	leases    map[*inflightCreate]struct{}
	leaseCond *sync.Cond
	gen       int64
	runtimes  map[string]*claudeManagedRuntime

	// approvals is the C1D non-actionable ingest sink, wired once by the
	// composition root before any create.
	approvals *AuthoritativeApprovalStore

	// createBarrier is a NARROW test seam (nil in production).
	createBarrier func(stage string)
}

func (s *ManagedClaudeService) barrier(stage string) {
	if s.createBarrier != nil {
		s.createBarrier(stage)
	}
}

// NewManagedClaudeService creates the service. launcher nil means the
// production execLauncher. Identity verification runs per create, not here.
func NewManagedClaudeService(cfg ClaudeEntryConfig, launcher ManagedLauncher, attestor ClaudeAttestor) *ManagedClaudeService {
	if launcher == nil {
		launcher = execLauncher{}
	}
	if attestor == nil {
		attestor = NewClaudeAttestor(cfg)
	}
	s := &ManagedClaudeService{
		cfg:              cfg,
		launcher:         launcher,
		attestor:         attestor,
		handshakeTimeout: claudeHandshakeTimeout,
		reg:              NewManagedSessionRegistry(maxClaudeSessions),
		leases:           make(map[*inflightCreate]struct{}),
		runtimes:         make(map[string]*claudeManagedRuntime),
	}
	s.leaseCond = sync.NewCond(&s.mu)
	return s
}

// NewManagedClaudeServiceForTest builds a service with injected dependencies.
func NewManagedClaudeServiceForTest(launcher ManagedLauncher, attestor ClaudeAttestor) *ManagedClaudeService {
	return NewManagedClaudeService(ClaudeEntryConfig{
		Bin:              "/pinned/test/claude",
		Version:          "2.1.209",
		AuthorityVersion: "2.1.209",
	}, launcher, attestor)
}

// Registry exposes the owned-session registry for the read-only REST surface.
func (s *ManagedClaudeService) Registry() *ManagedSessionRegistry { return s.reg }

// SetApprovalStore configures the authoritative approval store as the C1D
// NON-ACTIONABLE observation sink. Mirrors the Codex contract: immutable after
// first runtime or shutdown.
func (s *ManagedClaudeService) SetApprovalStore(store *AuthoritativeApprovalStore) error {
	if store == nil {
		return fmt.Errorf("claude approval store configure: nil store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return fmt.Errorf("claude approval store configure: service is shutting down")
	}
	if s.approvals == store {
		return nil // idempotent
	}
	if s.gen != 0 || len(s.runtimes) != 0 {
		return fmt.Errorf("claude approval store configure: a managed runtime already exists; the store is immutable")
	}
	if s.approvals != nil {
		return fmt.Errorf("claude approval store configure: a different approval store is already configured")
	}
	s.approvals = store
	return nil
}

// beginLease registers an in-flight create BEFORE verify/spawn.
func (s *ManagedClaudeService) beginLease() (*inflightCreate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return nil, fmt.Errorf("managed claude service is shutting down")
	}
	lease := &inflightCreate{}
	s.leases[lease] = struct{}{}
	return lease, nil
}

// endLease releases an in-flight create on every exit path.
func (s *ManagedClaudeService) endLease(lease *inflightCreate) {
	s.mu.Lock()
	delete(s.leases, lease)
	s.mu.Unlock()
	s.leaseCond.Broadcast()
}

// eventStoreFor resolves a session's projection store and current epoch.
// C1D does not have an event store (no Codex-style managed events yet).
func (s *ManagedClaudeService) eventStoreFor(sessionID string) (*managedEventStore, int64, bool) {
	return nil, 0, false
}

// CreateDetached launches a Claude managed session: verify pinned identity →
// direct spawn with isolated hook settings → start hook bridge → register →
// start the certification turn → start the event pump.
func (s *ManagedClaudeService) CreateDetached(cwd string) (string, error) {
	if err := validateCWD(cwd); err != nil {
		return "", err
	}
	lease, err := s.beginLease()
	if err != nil {
		return "", err
	}
	defer s.endLease(lease)

	// Resolve the binary and certify.
	exe, err := exec.LookPath(s.cfg.Bin)
	if err != nil {
		return "", fmt.Errorf("managed claude: executable not found: %w", err)
	}
	if err := s.attestor.Certify(exe); err != nil {
		return "", fmt.Errorf("managed claude certify: %w", err)
	}
	if lease.isCancelled() {
		return "", fmt.Errorf("managed claude service is shutting down")
	}

	// Create isolated hook settings and hook bridge.
	hookDir, settingsPath, err := s.createHookSettings()
	if err != nil {
		return "", fmt.Errorf("managed claude hook settings: %w", err)
	}
	bridge, token, err := newClaudeHookBridge()
	if err != nil {
		os.RemoveAll(hookDir)
		return "", fmt.Errorf("managed claude hook bridge: %w", err)
	}
	<-bridge.started // wait for listener readiness

	// Write the hook script that curls the bridge.
	if err := s.writeHookScript(hookDir, bridge.endpoint()); err != nil {
		bridge.close()
		os.RemoveAll(hookDir)
		return "", fmt.Errorf("managed claude hook script: %w", err)
	}

	s.barrier("pre-spawn")

	// Launch Claude with isolated settings.
	// Args: --settings <path> -p <certification prompt>
	// No shell, no --permission-prompt-tool, no interactive flags.
	argv := []string{"--settings", settingsPath, "-p", claudeCertificationPrompt}
	proc, err := s.launcher.Launch(exe, argv)
	if err != nil {
		bridge.close()
		os.RemoveAll(hookDir)
		return "", fmt.Errorf("managed claude launch: %w", err)
	}
	if !lease.setProc(proc) {
		bridge.close()
		_ = proc.Kill()
		_ = proc.Wait()
		os.RemoveAll(hookDir)
		return "", fmt.Errorf("managed claude service is shutting down")
	}
	s.barrier("post-spawn")

	id := fmt.Sprintf("%s:%s", claudeHeadlessAdapter, genLocalID("claude"))
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		bridge.close()
		_ = proc.Kill()
		_ = proc.Wait()
		os.RemoveAll(hookDir)
		return "", fmt.Errorf("managed claude service is shutting down")
	}
	s.gen++
	epoch := s.gen
	rt := newClaudeManagedRuntime(proc, epoch, s.reg, bridge, hookDir)
	rt.sessionID = id
	rt.approvals = s.approvals
	rt.authorityVersion = s.cfg.AuthorityVersion
	s.runtimes[id] = rt
	s.mu.Unlock()

	fail := func(stage string, ferr error, registered bool) (string, error) {
		s.mu.Lock()
		delete(s.runtimes, id)
		s.mu.Unlock()
		if registered {
			s.reg.Remove(id)
		}
		rt.stop()
		return "", fmt.Errorf("managed claude %s: %w", stage, ferr)
	}

	_ = token // capability token referenced in hook script, bridge owns the actual validation

	rec := ManagedSessionRecord{
		SessionID: id,
		Provider:  "claude",
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

	go rt.pump()
	return id, nil
}

// createHookSettings writes an isolated Claude settings JSON file that
// configures the PreToolUse hook to call the daemon's bridge. Returns the
// hook directory path and the settings file path.
func (s *ManagedClaudeService) createHookSettings() (hookDir string, settingsPath string, err error) {
	hookDir, err = os.MkdirTemp("", "pokit-claude-hooks-")
	if err != nil {
		return "", "", err
	}

	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []map[string]any{
				{
					"matcher": "", // match all tools
					"command": filepath.Join(hookDir, "hook.sh"),
				},
			},
		},
	}
	settingsPath = filepath.Join(hookDir, "settings.json")
	f, err := os.Create(settingsPath)
	if err != nil {
		os.RemoveAll(hookDir)
		return "", "", err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(settings); err != nil {
		os.RemoveAll(hookDir)
		return "", "", err
	}
	return hookDir, settingsPath, nil
}

// writeHookScript writes the shell script that curls the bridge with the
// PreToolUse JSON from stdin. The script is minimal and contains no secrets.
func (s *ManagedClaudeService) writeHookScript(hookDir, bridgeURL string) error {
	// The script receives PreToolUse JSON on stdin and forwards it to the bridge.
	// The bridge's response (JSON) is written to stdout for Claude to consume.
	script := fmt.Sprintf("#!/bin/sh\ncurl -s -X POST -d @- '%s'\n", bridgeURL)
	scriptPath := filepath.Join(hookDir, "hook.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0700); err != nil {
		return err
	}
	return nil
}

// Shutdown performs the single closing transition. Mirrors ManagedCodexService.Shutdown.
func (s *ManagedClaudeService) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	leases := make([]*inflightCreate, 0, len(s.leases))
	for l := range s.leases {
		leases = append(leases, l)
	}
	rts := make([]*claudeManagedRuntime, 0, len(s.runtimes))
	for _, rt := range s.runtimes {
		rts = append(rts, rt)
	}
	s.runtimes = make(map[string]*claudeManagedRuntime)
	s.mu.Unlock()

	s.reg.Close()

	for _, l := range leases {
		l.cancel()
	}
	for _, rt := range rts {
		_ = rt.proc.Kill()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, rt := range rts {
			_ = rt.proc.Wait()
		}
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
		return fmt.Errorf("managed claude shutdown drain: %w", ctx.Err())
	}
}

// Stop gracefully terminates a managed Claude session.
func (s *ManagedClaudeService) Stop(sessionID string, epoch int64) error {
	s.mu.Lock()
	rt := s.runtimes[sessionID]
	s.mu.Unlock()
	if rt == nil {
		return fmt.Errorf("managed claude session not found")
	}
	if rt.epoch != epoch {
		return fmt.Errorf("stale session epoch")
	}
	rec, ok := s.reg.Get(sessionID)
	if !ok {
		return fmt.Errorf("managed claude session not found")
	}
	if rec.Exited {
		return nil
	}
	rt.stop()
	return nil
}

// Kill force-terminates a managed Claude session.
func (s *ManagedClaudeService) Kill(sessionID string, epoch int64) error {
	s.mu.Lock()
	rt := s.runtimes[sessionID]
	s.mu.Unlock()
	if rt == nil {
		return fmt.Errorf("managed claude session not found")
	}
	if rt.epoch != epoch {
		return fmt.Errorf("stale session epoch")
	}
	rec, ok := s.reg.Get(sessionID)
	if !ok {
		return fmt.Errorf("managed claude session not found")
	}
	if rec.Exited {
		return nil
	}
	rt.turnMu.Lock()
	rt.turnClosed = true
	rt.turnMu.Unlock()
	_ = rt.proc.Kill()
	_ = rt.proc.Wait()
	return nil
}

// Delete removes a TERMINAL managed Claude session.
func (s *ManagedClaudeService) Delete(sessionID string, epoch int64) error {
	rec, ok := s.reg.Get(sessionID)
	if !ok {
		return fmt.Errorf("managed claude session not found")
	}
	if rec.Epoch != epoch {
		return fmt.Errorf("stale session epoch")
	}
	if !rec.Exited {
		return fmt.Errorf("managed claude session is not terminal: stop or kill it first")
	}
	s.mu.Lock()
	rt := s.runtimes[sessionID]
	delete(s.runtimes, sessionID)
	approvals := s.approvals
	s.mu.Unlock()
	if rt != nil && rt.bridge != nil {
		rt.bridge.close()
	}
	if rt != nil && rt.hookDir != "" {
		_ = os.RemoveAll(rt.hookDir)
	}
	s.reg.Remove(sessionID)
	if approvals != nil {
		approvals.Clear(sessionID)
	}
	return nil
}
