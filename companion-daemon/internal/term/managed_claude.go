// Package term — C1D: structured detached launch of a POKIT-owned Claude Code
// child. The daemon directly spawns the pinned certified executable with
// session-isolated hook settings and a daemon-owned private hook bridge. The
// runtime owns stdin, stdout, child wait/reap, and the hook bridge lifecycle.
// C1D is observation-only: zero actionable options, zero ClaimForExecution,
// zero mobile CTA.
//
// Launch argv matches the C0D-certified path:
//
//	--settings <isolated> --setting-sources "" --output-format stream-json
//	--include-partial-messages -p <prompt>
//
// The pump reads stream-json lines from stdout. A tool_deferred result is
// joined against the hook bridge's pending observation by comparing the full
// identity tuple (session_id, tool_use_id, tool_name, input_sha256).
package term

import (
	"bufio"
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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

const claudeHeadlessAdapter = "claude_headless"

// claudeCertificationPrompt triggers a guaranteed Bash tool use so the
// PreToolUse hook fires and a defer observation can be joined. The command
// is harmless and its output is discarded.
const claudeCertificationPrompt = "Use your Bash tool to run exactly this command: echo c1d-probe-ok"

const (
	claudeHandshakeTimeout        = 30 * time.Second
	maxClaudeSessions             = 4
	maxPendingClaudeObservations  = 4
	claudeObservationTimeout      = 120 * time.Second
)

// ── Pending observation ──

type claudePendingObservation struct {
	toolUseID   string
	toolName    string
	sessionID   string // provider Claude session ID from the hook
	inputDigest string // SHA-256 hex of canonical tool_input
	observedAt  time.Time
}

// ── Managed runtime ──

type claudeManagedRuntime struct {
	sessionID string
	epoch     int64
	proc      ManagedProcess
	reg       *ManagedSessionRegistry
	scanner   *bufio.Scanner
	exited    chan struct{}

	bridge  *claudeHookBridge
	hookDir string

	turnMu             sync.Mutex
	turnClosed         bool
	pendingObservations map[string]*claudePendingObservation
	rejects            int

	approvals        *AuthoritativeApprovalStore
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
	bridge.rt = rt
	return rt
}

// observePreToolUse stores a pending observation from the hook bridge.
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

// streamDeferred is the C0D-certified structure of a tool_deferred result
// emitted by Claude Code in stream-json output mode.
type streamDeferred struct {
	Type             string `json:"type"`
	StopReason       string `json:"stop_reason"`
	SessionID        string `json:"session_id"`
	DeferredToolUse  *struct {
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"deferred_tool_use"`
}

// joinDeferred attempts to match a tool_deferred result from Claude's stdout
// against a pending observation. FULL identity comparison: session_id +
// tool_use_id + tool_name + input_sha256 must all match.
func (rt *claudeManagedRuntime) joinDeferred(d *streamDeferred) {
	if d.DeferredToolUse == nil || d.SessionID == "" {
		return
	}
	toolUseID := d.DeferredToolUse.ID
	toolName := d.DeferredToolUse.Name
	sessionID := d.SessionID

	// Compute input digest from the deferred result for comparison.
	inputCanon, err := canonicalJSON(d.DeferredToolUse.Input)
	if err != nil {
		return
	}
	inputDigest := sha256Hex(inputCanon)

	rt.turnMu.Lock()
	pending, ok := rt.pendingObservations[toolUseID]
	if !ok {
		rt.turnMu.Unlock()
		return
	}
	// Full identity comparison: all four fields must match.
	if pending.sessionID != sessionID || pending.toolName != toolName || pending.inputDigest != inputDigest {
		rt.turnMu.Unlock()
		return
	}
	delete(rt.pendingObservations, toolUseID)
	approvals := rt.approvals
	rt.turnMu.Unlock()

	if approvals == nil {
		return
	}

	// Opaque daemon-generated ApprovalID. The provider tool_use_id is NEVER
	// used as the ApprovalID — it is stored only in the private pending
	// observation for identity comparison.
	approvalID := fmt.Sprintf("claude-%s", genApprovalToken())

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
				Options:    nil,
				Source:     agent.SourceJSONL,
				Confidence: 1,
			},
			Provenance:       contract.ProvenanceProviderHook,
			Actionable:       false,
			RequiredPerm:     "",
			DeliveryMaterial: nil,
		}},
	})

	if rt.observer != nil {
		rt.observer("deferred_joined")
	}
}

// pump reads Claude's stream-json stdout. Each line is a JSON object. Lines
// with type="result" and stop_reason="tool_deferred" are joined against
// pending hook observations. On child EOF/error it marks the record exited.
func (rt *claudeManagedRuntime) pump() {
	// Stale-observation cleanup: the pump periodically clears observations
	// that have exceeded the timeout. This runs inline in the pump loop via
	// a time.Ticker checked in the scan loop, avoiding a separate goroutine.
	staleTicker := time.NewTicker(30 * time.Second)
	defer staleTicker.Stop()

	// Read loop with stale check on ticker channel. We use a select to
	// interleave scanning and cleanup without a separate goroutine.
	lines := make(chan []byte, 64)
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		for rt.scanner.Scan() {
			line := bytes.TrimSpace(rt.scanner.Bytes())
			if len(line) > 0 {
				lines <- line
			}
		}
	}()

loop:
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				break loop
			}
			rt.processLine(line)
		case <-staleTicker.C:
			rt.clearStaleObservations()
		}
	}
	// Drain any remaining lines after scanner stopped.
	for line := range lines {
		rt.processLine(line)
	}

	rt.turnMu.Lock()
	rt.turnClosed = true
	rt.pendingObservations = make(map[string]*claudePendingObservation)
	rt.turnMu.Unlock()

	if rt.approvals != nil {
		rt.approvals.InvalidateSession(rt.sessionID, "managed claude child exited")
	}
	rt.reg.MarkExited(rt.sessionID, rt.epoch)
	close(rt.exited)
	_ = rt.proc.Wait()
}

// processLine attempts to parse a stream-json line as a tool_deferred result.
func (rt *claudeManagedRuntime) processLine(line []byte) {
	var event streamDeferred
	if err := json.Unmarshal(line, &event); err != nil {
		return
	}
	if event.Type != "result" || event.StopReason != "tool_deferred" {
		return
	}
	rt.joinDeferred(&event)
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
				approvalID := fmt.Sprintf("claude-%s", genApprovalToken())
				_ = approvalID
				rt.approvals.InvalidateRecord(rt.sessionID, approvalID)
			}
		}
	}
}

func (rt *claudeManagedRuntime) stop() {
	if rt.bridge != nil {
		rt.bridge.close()
	}
	_ = rt.proc.Kill()
	_ = rt.proc.Wait()
	if rt.hookDir != "" {
		_ = os.RemoveAll(rt.hookDir)
	}
}

// ── Service ──

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

	approvals *AuthoritativeApprovalStore

	createBarrier func(stage string)
}

func (s *ManagedClaudeService) barrier(stage string) {
	if s.createBarrier != nil {
		s.createBarrier(stage)
	}
}

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

func NewManagedClaudeServiceForTest(launcher ManagedLauncher, attestor ClaudeAttestor) *ManagedClaudeService {
	return NewManagedClaudeService(ClaudeEntryConfig{
		Bin:              "/pinned/test/claude",
		Version:          "2.1.209",
		AuthorityVersion: "2.1.209",
	}, launcher, attestor)
}

func (s *ManagedClaudeService) Registry() *ManagedSessionRegistry { return s.reg }

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
		return nil
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

func (s *ManagedClaudeService) endLease(lease *inflightCreate) {
	s.mu.Lock()
	delete(s.leases, lease)
	s.mu.Unlock()
	s.leaseCond.Broadcast()
}

func (s *ManagedClaudeService) eventStoreFor(sessionID string) (*managedEventStore, int64, bool) {
	return nil, 0, false
}

// CreateDetached launches a Claude managed session with the C0D-certified
// argv, isolated hook settings, and a private hook bridge.
func (s *ManagedClaudeService) CreateDetached(cwd string) (string, error) {
	if err := validateCWD(cwd); err != nil {
		return "", err
	}
	lease, err := s.beginLease()
	if err != nil {
		return "", err
	}
	defer s.endLease(lease)

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

	hookDir, settingsPath, err := s.createHookSettings()
	if err != nil {
		return "", fmt.Errorf("managed claude hook settings: %w", err)
	}
	bridge, _, err := newClaudeHookBridge()
	if err != nil {
		os.RemoveAll(hookDir)
		return "", fmt.Errorf("managed claude hook bridge: %w", err)
	}
	<-bridge.started

	if err := s.writeHookScript(hookDir, bridge.endpoint()); err != nil {
		bridge.close()
		os.RemoveAll(hookDir)
		return "", fmt.Errorf("managed claude hook script: %w", err)
	}

	s.barrier("pre-spawn")

	// C0D-certified launch argv: session-isolated settings, no user/project
	// settings, structured stream-json output, partial messages included.
	argv := []string{
		"--settings", settingsPath,
		"--setting-sources", "",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"-p", claudeCertificationPrompt,
	}
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
// configures the PreToolUse hook to call the daemon's bridge.
func (s *ManagedClaudeService) createHookSettings() (hookDir string, settingsPath string, err error) {
	hookDir, err = os.MkdirTemp("", "pokit-claude-hooks-")
	if err != nil {
		return "", "", err
	}

	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []map[string]any{
				{
					"matcher": "",
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

// writeHookScript writes the shell script that curls the bridge. The script
// pipes stdin (PreToolUse JSON from Claude) to the bridge and returns the
// bridge's response (JSON) to Claude on stdout.
func (s *ManagedClaudeService) writeHookScript(hookDir, bridgeURL string) error {
	script := fmt.Sprintf("#!/bin/sh\ncurl -s -X POST -d @- '%s'\n", bridgeURL)
	return os.WriteFile(filepath.Join(hookDir, "hook.sh"), []byte(script), 0700)
}

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
	s.reg.MarkExited(sessionID, epoch)
	return nil
}

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

// ── Helpers ──

// sha256Hex returns the hex-encoded SHA-256 digest of data.
func sha256Hex(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// genApprovalToken generates an opaque random token for use in ApprovalIDs.
// The provider tool_use_id is never used as the ApprovalID.
func genApprovalToken() string {
	b := make([]byte, 16)
	_, _ = crand.Read(b)
	return hex.EncodeToString(b)
}

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
