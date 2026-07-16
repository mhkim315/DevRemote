// Package term — C1D: structured detached launch of a POKIT-owned Claude Code
// child. C1D is observation-only: zero actionable options, zero
// ClaimForExecution, zero mobile CTA.
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
	"io"
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

const claudeCertificationPrompt = "Use your Bash tool to run exactly this command: echo c1d-probe-ok"

const (
	claudeHandshakeTimeout       = 30 * time.Second
	maxClaudeSessions            = 4
	maxPendingClaudeObservations = 4
	claudeObservationTimeout     = 120 * time.Second
)

// ── Pending observation ──

type claudePendingObservation struct {
	toolUseID  string
	toolName   string
	sessionID  string
	inputDigest string
	observedAt time.Time
}

// activeApproval tracks a joined/ingested approval for timeout invalidation.
type activeApproval struct {
	approvalID string
	expiresAt  time.Time
}

// ── Managed runtime ──

type claudeManagedRuntime struct {
	sessionID string
	epoch     int64
	proc      ManagedProcess
	reg       *ManagedSessionRegistry
	scanner   *bufio.Scanner
	exited    chan struct{}
	exitOnce  sync.Once // guards close(rt.exited)

	bridge  *claudeHookBridge
	hookDir string

	turnMu             sync.Mutex
	turnClosed         bool
	pendingObservations map[string]*claudePendingObservation
	activeApprovals    []activeApproval
	rejects            int

	approvals        *AuthoritativeApprovalStore
	authorityVersion string

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
		toolUseID:  toolUseID,
		toolName:   toolName,
		sessionID:  claudeSessionID,
		inputDigest: inputDigest,
		observedAt: time.Now(),
	}

	if rt.observer != nil {
		rt.observer("pre_tool_use")
	}
}

type streamDeferred struct {
	Type            string `json:"type"`
	StopReason      string `json:"stop_reason"`
	SessionID       string `json:"session_id"`
	DeferredToolUse *struct {
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"deferred_tool_use"`
}

// joinDeferred matches a tool_deferred result against a pending observation.
// On match, ingests a non-actionable record and tracks the approvalID with an
// expiry so timeout invalidation can reach it.
func (rt *claudeManagedRuntime) joinDeferred(d *streamDeferred) {
	if d.DeferredToolUse == nil || d.SessionID == "" {
		return
	}
	toolUseID := d.DeferredToolUse.ID
	toolName := d.DeferredToolUse.Name
	sessionID := d.SessionID

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
	if pending.sessionID != sessionID || pending.toolName != toolName || pending.inputDigest != inputDigest {
		rt.turnMu.Unlock()
		return
	}

	approvalToken, err := rt.genApprovalToken()
	if err != nil {
		rt.turnMu.Unlock()
		return
	}
	approvalID := "claude-" + approvalToken

	delete(rt.pendingObservations, toolUseID)
	// Track the ingested approval for timeout invalidation.
	rt.activeApprovals = append(rt.activeApprovals, activeApproval{
		approvalID: approvalID,
		expiresAt:  time.Now().Add(claudeObservationTimeout),
	})
	approvals := rt.approvals
	rt.turnMu.Unlock()

	if approvals == nil {
		return
	}

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

// genApprovalToken delegates to the service's entropy reader.
func (rt *claudeManagedRuntime) genApprovalToken() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(entropyReader, b); err != nil {
		return "", fmt.Errorf("approval token entropy: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// pump reads Claude's stream-json stdout.
func (rt *claudeManagedRuntime) pump() {
	defer rt.terminate() // guaranteed cleanup on any exit path

	staleTicker := time.NewTicker(30 * time.Second)
	defer staleTicker.Stop()

	lines := make(chan []byte, 64)
	go func() {
		defer close(lines)
		for rt.scanner.Scan() {
			line := bytes.TrimSpace(rt.scanner.Bytes())
			if len(line) > 0 {
				lines <- line
			}
		}
	}()

	for {
		select {
		case line, ok := <-lines:
			if !ok {
				goto exit
			}
			rt.processLine(line)
		case <-staleTicker.C:
			rt.clearStaleObservations()
		}
	}
exit:
	for line := range lines {
		rt.processLine(line)
	}
}

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

// clearStaleObservations removes expired pending observations AND expired
// active approvals. Both are checked under turnMu.
func (rt *claudeManagedRuntime) clearStaleObservations() {
	rt.turnMu.Lock()
	defer rt.turnMu.Unlock()

	cutoff := time.Now().Add(-claudeObservationTimeout)
	for id, obs := range rt.pendingObservations {
		if obs.observedAt.Before(cutoff) {
			delete(rt.pendingObservations, id)
		}
	}
	// Expire joined approvals whose timeout has elapsed.
	remaining := rt.activeApprovals[:0]
	for _, aa := range rt.activeApprovals {
		if time.Now().After(aa.expiresAt) {
			if rt.approvals != nil {
				rt.approvals.InvalidateRecord(rt.sessionID, aa.approvalID)
			}
		} else {
			remaining = append(remaining, aa)
		}
	}
	rt.activeApprovals = remaining
}

// terminate is the SINGLE idempotent exit transition.
func (rt *claudeManagedRuntime) terminate() {
	rt.exitOnce.Do(func() {
		if rt.bridge != nil {
			rt.bridge.close()
		}
		_ = rt.proc.Kill()
		_ = rt.proc.Wait()
		if rt.hookDir != "" {
			_ = os.RemoveAll(rt.hookDir)
		}

		rt.turnMu.Lock()
		rt.turnClosed = true
		rt.pendingObservations = make(map[string]*claudePendingObservation)
		expired := rt.activeApprovals
		rt.activeApprovals = nil
		rt.turnMu.Unlock()

		approvals := rt.approvals
		if approvals != nil {
			for _, aa := range expired {
				approvals.InvalidateRecord(rt.sessionID, aa.approvalID)
			}
			approvals.InvalidateSession(rt.sessionID, "managed claude child exited")
		}
		rt.reg.MarkExited(rt.sessionID, rt.epoch)
		close(rt.exited)
	})
}

func (rt *claudeManagedRuntime) stop() {
	rt.terminate()
}

// ── Service ──

// entropyReader is the entropy source for token generation. In production it
// is crypto/rand.Reader. Tests may replace it with a failing reader.
var entropyReader io.Reader = crand.Reader

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
		PinnedPath:       "/pinned/test/claude",
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

// CreateDetached launches a Claude managed session.
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

	argv := []string{
		"--verbose",
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
		rt.terminate()
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

// createHookSettings writes an isolated Claude settings JSON file using the
// C0D-certified hook schema.
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
					"hooks": []map[string]any{
						{
							"type":    "command",
							"command": filepath.Join(hookDir, "hook.sh"),
						},
					},
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
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(settings); err != nil {
		os.RemoveAll(hookDir)
		return "", "", err
	}
	return hookDir, settingsPath, nil
}

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
		rt.terminate()
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

// Stop terminates the session deterministically. On return the bridge is
// closed, the process is reaped, pending state is cleared, approvals are
// invalidated, and the registry marks the session exited.
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
	rt.terminate()
	return nil
}

// Kill force-terminates deterministically. Same guarantees as Stop.
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
	rt.terminate()
	return nil
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
	delete(s.runtimes, sessionID)
	approvals := s.approvals
	s.mu.Unlock()
	s.reg.Remove(sessionID)
	if approvals != nil {
		approvals.Clear(sessionID)
	}
	return nil
}

// ── Shared helpers ──

func canonicalJSON(raw json.RawMessage) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func sha256Hex(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
