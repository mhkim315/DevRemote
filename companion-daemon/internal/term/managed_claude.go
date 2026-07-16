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
	"sync/atomic"
	"syscall"
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
	maxActiveApprovals           = maxPendingClaudeObservations
	claudeObservationTimeout     = 120 * time.Second
)

// ── Entropy and clock (injectable for tests) ──

var entropyReader io.Reader = crand.Reader

// clockNow returns the current time. Tests may replace it.
var clockNow = time.Now

// ── Pending observation ──

type claudePendingObservation struct {
	toolUseID   string
	toolName    string
	sessionID   string
	inputDigest string
	observedAt  time.Time
}

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
	exitOnce  sync.Once
	cwd       string // original validated cwd, bound for resume

	bridge  *claudeHookBridge
	hookDir string

	turnMu              sync.Mutex
	turnClosed          bool
	terminated          bool        // set by terminate()
	ingestGen           int64       // bumped by terminate()
	atomicTerminated    atomic.Bool // lock-free read for pre-ingest check
	deferredExit        bool        // B4: set before terminate() to preserve coordinator identity
	joinedDeferred      bool        // R5-B: true only after exact tool_deferred join+ingest
	pendingObservations map[string]*claudePendingObservation
	activeApprovals     []activeApproval
	rejects             int

	approvals        *AuthoritativeApprovalStore
	authorityVersion string

	// C2D-B: private one-shot resume coordinator. Set from the parent
	// ManagedClaudeService; nil when the service is not configured.
	coordinator *claudeResumeCoordinator

	observer       func(stage string)
	preIngestHook  func() // test seam: before Store ingest
	postIngestHook func() // test seam: after Store ingest, before active append
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
		toolUseID:   toolUseID,
		toolName:    toolName,
		sessionID:   claudeSessionID,
		inputDigest: inputDigest,
		observedAt:  clockNow(),
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

type streamDenial struct {
	Type              string `json:"type"`
	StopReason        string `json:"stop_reason"`
	SessionID         string `json:"session_id"`
	PermissionDenials []struct {
		ToolName    string `json:"tool_name"`
		ToolUseID   string `json:"tool_use_id"`
		InputSha256 string `json:"input_sha256"`
	} `json:"permission_denials"`
}

func strictDenialDecode(raw []byte) (*streamDenial, bool) {
	if len(raw) == 0 || len(raw) > 65536 {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var d streamDenial
	if err := dec.Decode(&d); err != nil {
		return nil, false
	}
	var trailing json.RawMessage
	if dec.Decode(&trailing) != io.EOF {
		return nil, false
	}
	if d.Type != "result" || d.SessionID == "" || len(d.SessionID) > 1024 {
		return nil, false
	}
	if len(d.PermissionDenials) == 0 || len(d.PermissionDenials) > 16 {
		return nil, false
	}
	for _, pd := range d.PermissionDenials {
		if pd.ToolName == "" || len(pd.ToolName) > 256 || pd.ToolUseID == "" || len(pd.ToolUseID) > 256 {
			return nil, false
		}
		if pd.InputSha256 != "" && !validCoordinatorDigest(pd.InputSha256) {
			return nil, false
		}
	}
	return &d, true
}

// joinDeferred matches a tool_deferred result against a pending observation.
// On match, ingests a non-actionable record. Capacity is checked against
// maxActiveApprovals; store admission failure rolls back the active entry.
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

	// Capacity check: active approvals are bounded.
	if len(rt.activeApprovals) >= maxActiveApprovals {
		rt.turnMu.Unlock()
		return
	}

	approvalToken, err := rt.genApprovalToken()
	if err != nil {
		rt.turnMu.Unlock()
		return
	}
	approvalID := "claude-" + approvalToken

	// C2D-B: preserve private identity before Store admission.
	// Roll back on admission failure so the capacity is not leaked.
	identityCreated := false
	if rt.coordinator != nil {
		pokitRT := RuntimeRef{Adapter: claudeHeadlessAdapter, Version: rt.authorityVersion, LaunchGen: rt.epoch, StreamGen: 0}
		if !rt.coordinator.ReserveIdentity(approvalID, sessionID, toolUseID, toolName, inputDigest, rt.sessionID, pokitRT) {
			rt.turnMu.Unlock()
			return // capacity exhausted or duplicate
		}
		identityCreated = true
	}

	delete(rt.pendingObservations, toolUseID)
	rt.turnMu.Unlock()

	// Test seam: inject Stop/terminate before Store ingest.
	if rt.preIngestHook != nil {
		rt.preIngestHook()
	}

	// Lock-free pre-ingest check: if terminate() ran (possibly from the
	// hook above), drop immediately. Covers the forward race where the
	// Store has no prior session.
	if rt.atomicTerminated.Load() {
		if identityCreated {
			rt.coordinator.RemoveIdentity(approvalID)
		}
		return
	}

	approvals := rt.approvals
	if approvals == nil {
		if identityCreated {
			rt.coordinator.RemoveIdentity(approvalID)
		}
		return
	}

	admitted := approvals.IngestObserved(ApprovalIngest{
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

	if !admitted {
		if identityCreated {
			rt.coordinator.RemoveIdentity(approvalID)
		}
		return
	}

	// R5-B: mark joined only after successful Store admission.
	rt.turnMu.Lock()
	rt.joinedDeferred = true
	rt.turnMu.Unlock()

	// Test seam: inject terminate BETWEEN Store admission and active append.
	if rt.postIngestHook != nil {
		rt.postIngestHook()
	}

	// Post-ingest check: if terminate() ran, clean up the just-ingested
	// record. This handles the reverse race.
	rt.turnMu.Lock()
	term2 := rt.terminated
	if !term2 {
		rt.activeApprovals = append(rt.activeApprovals, activeApproval{
			approvalID: approvalID,
			expiresAt:  clockNow().Add(claudeObservationTimeout),
		})
	}
	rt.turnMu.Unlock()

	if term2 {
		if approvals != nil {
			approvals.InvalidateRecord(rt.sessionID, approvalID)
		}
		// B2 fix: remove orphaned identity that was created before Store
		// admission but never added to activeApprovals.
		if identityCreated {
			rt.coordinator.RemoveIdentity(approvalID)
		}
	}

	if rt.observer != nil {
		rt.observer("deferred_joined")
	}
}

func (rt *claudeManagedRuntime) genApprovalToken() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(entropyReader, b); err != nil {
		return "", fmt.Errorf("approval token entropy: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// pump reads Claude's stream-json stdout.
func (rt *claudeManagedRuntime) pump() {
	// B4: expected deferred exit must preserve the coordinator identity.
	// Set deferredExit before terminate() so the coordinator is NOT cleared.
	defer func() {
		rt.turnMu.Lock()
		deferred := rt.joinedDeferred
		rt.turnMu.Unlock()
		if deferred {
			rt.deferredExit = true
		}
		rt.terminate()
	}()

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
	if err := json.Unmarshal(line, &event); err == nil && event.Type == "result" && event.StopReason == "tool_deferred" {
		rt.joinDeferred(&event)
		return
	}
	if d, ok := strictDenialDecode(line); ok {
		rt.routeDenial(d)
	}
}

func (rt *claudeManagedRuntime) routeDenial(d *streamDenial) {
	if rt.coordinator == nil || rt.bridge == nil {
		return
	}
	// R6-A3: read the immutable resume context under the bridge mutex.
	rt.bridge.mu.Lock()
	ctx := rt.bridge.resumeCtx
	rt.bridge.mu.Unlock()
	if ctx == nil {
		return
	}
	for _, pd := range d.PermissionDenials {
		if pd.ToolName == "" || pd.ToolUseID == "" || d.SessionID == "" {
			continue
		}
		if d.SessionID != ctx.claudeSessionID || pd.ToolUseID != ctx.toolUseID || pd.ToolName != ctx.toolName {
			continue
		}
		// R6-A4: verify the denial event's input_sha256 matches the
		// coordinator-stored digest. If the event carries a digest,
		// require it. If absent (older Claude versions), use stored.
		witnessDigest := pd.InputSha256
		if witnessDigest == "" {
			witnessDigest = ctx.inputDigest
		} else if witnessDigest != ctx.inputDigest {
			continue // digest mismatch — denial is for wrong input
		}
		rt.coordinator.MarkWitnessed(ctx.claimToken, WitnessPermissionDenials,
			d.SessionID, pd.ToolUseID, pd.ToolName, witnessDigest, ctx.originalRuntime)
	}
}

func (rt *claudeManagedRuntime) clearStaleObservations() {
	// C2D-A lock-order: collect targets under turnMu, then call Store and
	// coordinator OUTSIDE the lock. The coordinator mutex is independent;
	// holding turnMu across both violates the contract.
	now := clockNow()
	cutoff := now.Add(-claudeObservationTimeout)

	rt.turnMu.Lock()
	for id, obs := range rt.pendingObservations {
		if obs.observedAt.Before(cutoff) {
			delete(rt.pendingObservations, id)
		}
	}
	remaining := rt.activeApprovals[:0]
	var expiredIDs []string
	for _, aa := range rt.activeApprovals {
		if now.After(aa.expiresAt) {
			expiredIDs = append(expiredIDs, aa.approvalID)
		} else {
			remaining = append(remaining, aa)
		}
	}
	rt.activeApprovals = remaining
	staleCoord := rt.coordinator
	staleStore := rt.approvals
	sessionID := rt.sessionID
	rt.turnMu.Unlock()

	for _, aid := range expiredIDs {
		if staleStore != nil {
			staleStore.InvalidateRecord(sessionID, aid)
		}
		// B2: use ClearForApproval so any reserved entry is also cancelled.
		if staleCoord != nil {
			staleCoord.ClearForApproval(aid)
		}
	}
	if staleCoord != nil {
		staleCoord.clearStaleEntries(now)
	}
}

func (rt *claudeManagedRuntime) terminate() {
	rt.exitOnce.Do(func() {
		// B4: set the lock-free barrier FIRST, before any I/O, so
		// in-flight joinDeferred calls see it. Then invalidate the
		// coordinator atomically BEFORE bridge close and process
		// Kill/Wait — the gap between Kill and Wait is where
		// ReserveEntry could otherwise succeed against a dead process.
		rt.atomicTerminated.Store(true)

		// Invalidate coordinator BEFORE external I/O so reservation
		// and termination are linearized under the coordinator mutex.
		if rt.coordinator != nil && !rt.deferredExit {
			rt.coordinator.ClearRuntime(rt.sessionID, rt.epoch)
		}

		if rt.bridge != nil {
			rt.bridge.close()
		}
		_ = rt.proc.Kill()
		_ = rt.proc.Wait()
		if rt.hookDir != "" {
			_ = os.RemoveAll(rt.hookDir)
		}

		rt.turnMu.Lock()
		rt.terminated = true
		rt.ingestGen++
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
			// R11-F1: install metadata-only Store high-water at
			// StreamGen=1 so a late IngestObserved(StreamGen=0)
			// is rejected. InstallRuntimeGeneration creates NO
			// Approval record — it is the single Store-owned
			// metadata transition for termination.
			_ = approvals.InstallRuntimeGeneration(rt.sessionID, rt.epoch, 1, "terminated")
		}
		rt.reg.MarkExited(rt.sessionID, rt.epoch)
		close(rt.exited)
	})
}

func (rt *claudeManagedRuntime) stop() { rt.terminate() }

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

	// C2D-B: private one-shot resume coordinator. Identities survive
	// individual Claude process exit. Built eagerly; no delivery path
	// is active until C2D-C wires it.
	coordinator *claudeResumeCoordinator

	createBarrier func(stage string)
}

func (s *ManagedClaudeService) barrier(stage string) {
	if s.createBarrier != nil {
		s.createBarrier(stage)
	}
}

func NewManagedClaudeService(cfg ClaudeEntryConfig, launcher ManagedLauncher, attestor ClaudeAttestor) *ManagedClaudeService {
	if launcher == nil {
		launcher = &execLauncherWithDir{}
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
		coordinator:      NewClaudeResumeCoordinator(),
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
		PinnedDigest:     "0000000000000000000000000000000000000000000000000000000000000000",
	}, launcher, attestor)
}

func (s *ManagedClaudeService) Registry() *ManagedSessionRegistry { return s.reg }

// Coordinator returns the C2D-B resume coordinator. Exported for composition tests.
func (s *ManagedClaudeService) Coordinator() *claudeResumeCoordinator { return s.coordinator }

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

// CreateDetached launches a Claude managed session. The child runs in the
// requested cwd directory.
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

	// Reserve session ID and Store slot BEFORE child launch.
	// Observation must not become reachable before authority is secured.
	id := fmt.Sprintf("%s:%s", claudeHeadlessAdapter, genLocalID("claude"))
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		bridge.close()
		os.RemoveAll(hookDir)
		return "", fmt.Errorf("managed claude service is shutting down")
	}
	s.gen++
	epoch := s.gen
	s.mu.Unlock()

	// Reserve Store authority first — if this fails, never spawn.
	if s.approvals != nil {
		if err := s.approvals.InstallRuntimeGeneration(id, epoch, 0, "reserved"); err != nil {
			bridge.close()
			os.RemoveAll(hookDir)
			return "", fmt.Errorf("managed claude reserve: %w", err)
		}
	}

	// Rollback helper: on any failure after reservation, clear the reserved
	// Store slot so capacity is not permanently leaked.
	rollback := func() {
		if s.approvals != nil {
			s.approvals.Clear(id)
		}
		bridge.close()
		os.RemoveAll(hookDir)
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
	proc, err := launchWithDir(s.launcher, exe, argv, cwd)
	if err != nil {
		rollback()
		return "", fmt.Errorf("managed claude launch: %w", err)
	}
	if !lease.setProc(proc) {
		_ = proc.Kill()
		_ = proc.Wait()
		rollback()
		return "", fmt.Errorf("managed claude service is shutting down")
	}
	s.barrier("post-spawn")

	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		_ = proc.Kill()
		_ = proc.Wait()
		rollback()
		return "", fmt.Errorf("managed claude service is shutting down")
	}
	rt := newClaudeManagedRuntime(proc, epoch, s.reg, bridge, hookDir)
	rt.sessionID = id
	rt.cwd = cwd
	rt.approvals = s.approvals
	rt.authorityVersion = s.cfg.AuthorityVersion
	rt.coordinator = s.coordinator
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
		// Roll back the reserved Store slot on failure after registration.
		if s.approvals != nil {
			s.approvals.Clear(id)
		}
		return "", fmt.Errorf("managed claude %s: %w", stage, ferr)
	}

	rec := ManagedSessionRecord{
		SessionID:       id,
		Provider:        "claude",
		Version:         s.cfg.Version,
		Epoch:           epoch,
		ProcessID:       proc.OpaqueID(),
		OS:              goruntime.GOOS,
		Arch:            goruntime.GOARCH,
		CreatedAt:       clockNow(),
		CertifiedDigest: s.cfg.PinnedDigest,
		PID:             proc.PID(),
		HookDir:         hookDir,
	}
	if err := s.reg.Register(rec); err != nil {
		return fail("register", err, false)
	}
	s.barrier("post-register")

	go rt.pump()
	return id, nil
}

// ResumeForApproval launches Claude with --resume for an approved decision.
// It creates a new bridge with /resume and /posttool endpoints, writes hook
// scripts embedding the claim token and nonce, and spawns the pinned Claude
// executable. The caller must already have reserved a coordinator entry.
//
// No lock is held across spawn or I/O. The caller is responsible for
// cleaning up the returned runtime via terminate().
func (s *ManagedClaudeService) ResumeForApproval(handle ResumeHandle, ctx *resumeContext) (*claudeManagedRuntime, error) {
	cwd := ctx.originalCWD
	if cwd == "" {
		cwd = "/tmp"
	}
	if err := validateCWD(cwd); err != nil {
		return nil, err
	}
	lease, err := s.beginLease()
	if err != nil {
		return nil, err
	}
	defer s.endLease(lease)

	exe, err := exec.LookPath(s.cfg.Bin)
	if err != nil {
		return nil, fmt.Errorf("managed claude resume: executable not found: %w", err)
	}
	if err := s.attestor.Certify(exe); err != nil {
		return nil, fmt.Errorf("managed claude resume certify: %w", err)
	}
	if lease.isCancelled() {
		return nil, fmt.Errorf("managed claude service is shutting down")
	}

	// Create isolated hook directory with resume + posttool endpoints.
	hookDir, settingsPath, err := s.createResumeHookSettings(ctx.claudeSessionID)
	if err != nil {
		return nil, fmt.Errorf("managed claude resume hook settings: %w", err)
	}
	bridge, _, err := newClaudeHookBridge()
	if err != nil {
		os.RemoveAll(hookDir)
		return nil, fmt.Errorf("managed claude resume bridge: %w", err)
	}
	<-bridge.started

	// Install the immutable resume context BEFORE process spawn.
	bridge.installResumeContext(ctx)

	resumeURL := bridge.resumeEndpoint(handle.ClaimToken, handle.ResumeNonce)
	posttoolURL := bridge.posttoolEndpoint(handle.ClaimToken)
	if err := s.writeResumeHookScripts(hookDir, resumeURL, posttoolURL); err != nil {
		bridge.close()
		os.RemoveAll(hookDir)
		return nil, fmt.Errorf("managed claude resume hook script: %w", err)
	}

	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		bridge.close()
		os.RemoveAll(hookDir)
		return nil, fmt.Errorf("managed claude service is shutting down")
	}
	s.gen++
	epoch := s.gen
	s.mu.Unlock()

	argv := []string{
		"--verbose",
		"--resume", ctx.claudeSessionID,
		"--settings", settingsPath,
		"--setting-sources", "",
		"--output-format", "stream-json",
		"--include-partial-messages",
	}
	proc, err := launchWithDir(s.launcher, exe, argv, cwd)
	if err != nil {
		bridge.close()
		os.RemoveAll(hookDir)
		return nil, fmt.Errorf("managed claude resume launch: %w", err)
	}
	if !lease.setProc(proc) {
		_ = proc.Kill()
		_ = proc.Wait()
		bridge.close()
		os.RemoveAll(hookDir)
		return nil, fmt.Errorf("managed claude service is shutting down")
	}

	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		_ = proc.Kill()
		_ = proc.Wait()
		bridge.close()
		os.RemoveAll(hookDir)
		return nil, fmt.Errorf("managed claude service is shutting down")
	}
	rt := newClaudeManagedRuntime(proc, epoch, s.reg, bridge, hookDir)
	rt.sessionID = ctx.pokitSessionID
	rt.cwd = cwd
	rt.coordinator = s.coordinator
	rt.authorityVersion = s.cfg.AuthorityVersion
	s.mu.Unlock()

	go rt.pump()
	return rt, nil
}

// createResumeHookSettings creates settings.json with BOTH PreToolUse
// (matcher "") AND PostToolUse hooks for the resumed session.
func (s *ManagedClaudeService) createResumeHookSettings(_ string) (hookDir string, settingsPath string, err error) {
	hookDir, err = os.MkdirTemp("", "pokit-claude-resume-")
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
							"command": filepath.Join(hookDir, "hook_resume.sh"),
						},
					},
				},
			},
			"PostToolUse": []map[string]any{
				{
					"matcher": "",
					"hooks": []map[string]any{
						{
							"type":    "command",
							"command": filepath.Join(hookDir, "hook_posttool.sh"),
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

// writeResumeHookScripts writes the resume and posttool hook scripts.
func (s *ManagedClaudeService) writeResumeHookScripts(hookDir, resumeURL, posttoolURL string) error {
	resumeScript := fmt.Sprintf("#!/bin/sh\ncurl -s -X POST -d @- '%s'\n", resumeURL)
	if err := os.WriteFile(filepath.Join(hookDir, "hook_resume.sh"), []byte(resumeScript), 0700); err != nil {
		return err
	}
	posttoolScript := fmt.Sprintf("#!/bin/sh\ncurl -s -X POST -d @- '%s'\n", posttoolURL)
	return os.WriteFile(filepath.Join(hookDir, "hook_posttool.sh"), []byte(posttoolScript), 0700)
}

// launchWithDir spawns the executable in the given directory. It tries
// cwdLauncher first, falling back to plain Launch if the launcher doesn't
// support directory binding.
func launchWithDir(l ManagedLauncher, exe string, argv []string, cwd string) (ManagedProcess, error) {
	if cl, ok := l.(interface {
		LaunchInDir(exe string, argv []string, cwd string) (ManagedProcess, error)
	}); ok {
		return cl.LaunchInDir(exe, argv, cwd)
	}
	// Fallback: use the plain Launch (tests that don't need cwd).
	return l.Launch(exe, argv)
}

// execLauncherWithDir extends execLauncher with cwd support.
type execLauncherWithDir struct{}

func (execLauncherWithDir) Launch(exe string, argv []string) (ManagedProcess, error) {
	return execLauncher{}.Launch(exe, argv)
}

func (execLauncherWithDir) LaunchInDir(exe string, argv []string, cwd string) (ManagedProcess, error) {
	cmd := exec.Command(exe, argv...)
	cmd.Dir = cwd
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
	return &execProcess{cmd: cmd, stdin: stdin, stdout: stdout, started: clockNow()}, nil
}

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

	// C2D-B: close coordinator after all runtimes are terminated.
	if s.coordinator != nil {
		s.coordinator.Close()
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
	if rec, ok := s.reg.Get(sessionID); !ok {
		return fmt.Errorf("managed claude session not found")
	} else if rec.Exited {
		return nil
	}
	rt.terminate()
	return nil
}

// SimulateGracefulExit marks the runtime as having exited expectedly
// (after tool_deferred) and calls terminate. This preserves coordinator
// identities so the mobile claim/resume flow can use them. Exposed for
// composition tests; must not be used in production.
func (s *ManagedClaudeService) SimulateGracefulExit(sessionID string, epoch int64) error {
	s.mu.Lock()
	rt := s.runtimes[sessionID]
	s.mu.Unlock()
	if rt == nil {
		return fmt.Errorf("managed claude session not found")
	}
	if rt.epoch != epoch {
		return fmt.Errorf("stale session epoch")
	}
	rt.turnMu.Lock()
	rt.deferredExit = true
	rt.turnMu.Unlock()
	rt.terminate()
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
	if rec, ok := s.reg.Get(sessionID); !ok {
		return fmt.Errorf("managed claude session not found")
	} else if rec.Exited {
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

// CanonicalDigest computes the SHA-256 digest of canonical JSON for
// use in tests and composition. Exported for cmd/devremote tests.
func CanonicalDigest(raw []byte) string {
	canon, err := canonicalJSON(json.RawMessage(raw))
	if err != nil {
		return ""
	}
	return sha256Hex(canon)
}
