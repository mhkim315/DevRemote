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
	"devremote/companion-daemon/internal/devicetrust"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

const claudeHeadlessAdapter = "claude_headless"

// claudeCertificationPrompt elicits the ONE frozen catalog action
// (claude.bash.approval_probe.v1). C3D-C: the certification turn IS the
// live approval probe — CreateDetached has exactly one model turn, so this
// prompt is the only production path that can produce the exact catalog
// PreToolUse the accepted activation note §8 requires. The prompt shape is
// the one proven to elicit the exact command on pinned 2.1.209 (C1D live
// proof, R6-A7 probes). A deviated/refused command fails the classifier and
// stays non-actionable — never coerced, always visible.
const claudeCertificationPrompt = "Use your Bash tool to run exactly this command: echo pokitclaudeapprovalprobe"

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
	toolUseID       string
	toolName        string
	sessionID       string
	inputDigest     string
	catalogActionID string // P2A: empty for non-catalog observations
	observedAt      time.Time
}

type activeApproval struct {
	approvalID string
	expiresAt  time.Time
}

// ── Managed runtime ──

type claudeManagedRuntime struct {
	authorizer devicetrust.MutationAuthorizer
	sessionID  string
	runtimeID  string
	epoch      int64
	proc       ManagedProcess
	reg        *ManagedSessionRegistry
	scanner    *bufio.Scanner
	exited     chan struct{}
	exitOnce   sync.Once
	cwd        string // original validated cwd, bound for resume

	bridge  *claudeHookBridge
	hookDir string

	turnMu              sync.Mutex
	turnClosed          bool
	terminated          bool        // set by terminate()
	ingestGen           int64       // bumped by terminate()
	atomicTerminated    atomic.Bool // lock-free read for pre-ingest check
	deferredExit        atomic.Bool // B4: set before terminate() to preserve coordinator identity
	joinedDeferred      bool        // R5-B: true only after exact tool_deferred join+ingest
	pendingObservations map[string]*claudePendingObservation
	activeApprovals     []activeApproval
	rejects             int

	approvals        *AuthoritativeApprovalStore
	authorityVersion string

	// actionableActive (C3D-A) is the runtime's activation state, copied
	// from the service under s.mu at creation and never toggled mid-life.
	// Only an ACTIVE runtime may ingest a catalog-matched deferred request
	// as actionable; an inactive runtime keeps the frozen C1D
	// non-actionable zero-option observation.
	actionableActive bool

	// launchCert (C3D §12) is the immutable per-incarnation launch
	// certification tuple, set once before pump() starts.
	launchCert ClaudeLaunchCertification

	// C2D-B: private one-shot resume coordinator. Set from the parent
	// ManagedClaudeService; nil when the service is not configured.
	coordinator *claudeResumeCoordinator

	// R6-B: immutable resume-attempt context. Set exactly once in
	// ResumeForApproval BEFORE pump() starts (happens-before via the go
	// statement); nil for C1D observation runtimes, which therefore can
	// never produce a deny witness. The pump is the only reader.
	resumeCtx *resumeContext

	// A resume incarnation temporarily supersedes the same Pokit session.
	// These saved values permit restoration only when the original process
	// was still live; the normal joined-deferred exited original stays exited.
	resumePreviousRuntime *claudeManagedRuntime
	resumePreviousRecord  ManagedSessionRecord
	// terminalIntent is guarded by ManagedClaudeService.mu. Stop/Kill set it
	// before releasing the current-runtime lookup, and resume cleanup checks
	// it under the same lock before considering an older-generation restore.
	terminalIntent bool

	observer       func(stage string)
	preIngestHook  func() // test seam: before Store ingest
	postIngestHook func() // test seam: after Store ingest, before active append

	operationalMu  sync.RWMutex
	operational    OperationalEventSink
	operationalSeq atomic.Uint64
	finishOnce     sync.Once
}

func newClaudeManagedRuntime(authorizer devicetrust.MutationAuthorizer, proc ManagedProcess, epoch int64, reg *ManagedSessionRegistry, bridge *claudeHookBridge, hookDir string) *claudeManagedRuntime {
	sc := bufio.NewScanner(proc.Stdout())
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	rt := &claudeManagedRuntime{
		authorizer:          authorizer,
		proc:                proc,
		epoch:               epoch,
		reg:                 reg,
		scanner:             sc,
		exited:              make(chan struct{}),
		bridge:              bridge,
		hookDir:             hookDir,
		pendingObservations: make(map[string]*claudePendingObservation),
	}
	// NOTE: the runtime is NOT published to the bridge here. The caller
	// finishes assigning the runtime's identity/authority fields and then
	// publishes via bridge.publishRuntime — a hook that arrives before
	// publication sees nil and answers the safe defer (the accepted
	// earliest-hook semantics). Publishing a half-initialized runtime to
	// the concurrently-serving hook handlers is a data race (found by the
	// C3D-C live run under -race).
	return rt
}

func (rt *claudeManagedRuntime) observePreToolUse(toolUseID, toolName, claudeSessionID, inputDigest, catalogActionID string) {
	rt.turnMu.Lock()
	if rt.turnClosed {
		rt.rejects++
		rt.turnMu.Unlock()
		return
	}
	if _, dup := rt.pendingObservations[toolUseID]; dup {
		rt.rejects++
		rt.turnMu.Unlock()
		return
	}
	if len(rt.pendingObservations) >= maxPendingClaudeObservations {
		rt.rejects++
		rt.turnMu.Unlock()
		return
	}

	rt.pendingObservations[toolUseID] = &claudePendingObservation{
		toolUseID:       toolUseID,
		toolName:        toolName,
		sessionID:       claudeSessionID,
		inputDigest:     inputDigest,
		catalogActionID: catalogActionID,
		observedAt:      clockNow(),
	}
	rt.turnMu.Unlock()

	if rt.observer != nil {
		rt.observer("pre_tool_use")
	}
	rt.emitOperational(OperationalToolCallStarted, toolUseID, "PreToolUse", toolUseID)
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

// ── R6-B denial decoder ──
//
// The deny witness wire contract is frozen by the accepted R6-A7 evidence
// (docs/A1_2_C2D_C_R6_A7_EVIDENCE_REPORT.md §5, projections
// docs/a1_2_c2d_c_r6a7_projection_run_{a,b}.json): the denial-carrying
// result event has ~20 top-level fields of which only type, session_id and
// permission_denials are witness authority; each denial entry is exactly
// {tool_use_id, tool_name, tool_input} with RAW tool input and NO provider
// digest field. The digest is recomputed here from the event's own
// tool_input with the same canonicalizer used at PreToolUse observation
// (contract P1); the raw input never escapes the decoder (P4).

const (
	maxDenialLineBytes = 1 << 20 // scanner max token size
	maxDenialEntries   = 16
)

// streamDenialEntry is one strictly decoded permission_denials entry. The
// raw tool_input is reduced to its recomputed canonical digest inside the
// decoder and never leaves it.
type streamDenialEntry struct {
	ToolUseID   string
	ToolName    string
	InputDigest string // recomputed from the event's raw tool_input
}

type streamDenial struct {
	SessionID string
	Entries   []streamDenialEntry
}

// denialDecodeOutcome is the closed decode vocabulary. A detected denial
// that fails strict decoding is denialMalformed and MUST fail closed —
// never skipped (contract P6).
type denialDecodeOutcome int

const (
	denialNotDenial denialDecodeOutcome = iota
	denialOK
	denialMalformed
)

// detectDenialResult reports whether a line is denial-shaped: a JSON object
// with type=="result" carrying a permission_denials member.
func detectDenialResult(raw []byte) bool {
	if len(raw) == 0 || len(raw) > maxDenialLineBytes {
		return false
	}
	var probe struct {
		Type              string          `json:"type"`
		PermissionDenials json.RawMessage `json:"permission_denials"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.Type == "result" && len(probe.PermissionDenials) > 0
}

// decodeDenialResult strictly decodes a detected denial-shaped line.
//
// Authority fields (type, session_id, permission_denials) are token-walked
// and accepted exactly once each; a duplicate authority key is malformed.
// type must be present and equal "result". Unknown top-level fields are
// advisory per the frozen wire contract and are skipped as opaque JSON
// without interpretation.
func decodeDenialResult(raw []byte) (*streamDenial, denialDecodeOutcome) {
	if !detectDenialResult(raw) {
		return nil, denialNotDenial
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, denialMalformed
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, denialMalformed
	}
	var sessionID string
	var seenType, seenSession, seenDenials bool
	var entries []streamDenialEntry
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, denialMalformed
		}
		key, ok := kt.(string)
		if !ok {
			return nil, denialMalformed
		}
		switch key {
		case "type":
			if seenType {
				return nil, denialMalformed
			}
			seenType = true
			var v json.RawMessage
			if err := dec.Decode(&v); err != nil {
				return nil, denialMalformed
			}
			s, ok := strictBoundedString(v, 64)
			if !ok || s != "result" {
				return nil, denialMalformed
			}
		case "session_id":
			if seenSession {
				return nil, denialMalformed
			}
			seenSession = true
			var v json.RawMessage
			if err := dec.Decode(&v); err != nil {
				return nil, denialMalformed
			}
			s, ok := strictBoundedString(v, maxCoordinatorSessionID)
			if !ok || !validCoordinatorToken(s, maxCoordinatorSessionID) {
				return nil, denialMalformed
			}
			sessionID = s
		case "permission_denials":
			if seenDenials {
				return nil, denialMalformed
			}
			seenDenials = true
			es, ok := decodeDenialEntries(dec)
			if !ok {
				return nil, denialMalformed
			}
			entries = es
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, denialMalformed
			}
		}
	}
	if _, err := dec.Token(); err != nil { // consume '}'
		return nil, denialMalformed
	}
	if _, err := dec.Token(); err != io.EOF { // trailing content
		return nil, denialMalformed
	}
	if !seenType || !seenSession || !seenDenials {
		return nil, denialMalformed
	}
	return &streamDenial{SessionID: sessionID, Entries: entries}, denialOK
}

func decodeDenialEntries(dec *json.Decoder) ([]streamDenialEntry, bool) {
	tok, err := dec.Token()
	if err != nil {
		return nil, false
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return nil, false
	}
	var out []streamDenialEntry
	for dec.More() {
		if len(out) >= maxDenialEntries {
			return nil, false
		}
		e, ok := decodeDenialEntry(dec)
		if !ok {
			return nil, false
		}
		out = append(out, e)
	}
	if _, err := dec.Token(); err != nil { // consume ']'
		return nil, false
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// decodeDenialEntry decodes one entry against the CLOSED allowlist frozen
// by the R6-A7 evidence: exactly {tool_use_id, tool_name, tool_input}, all
// present, no unknown or duplicate fields. The raw tool_input is bounded,
// canonicalized and digested here; the bytes never escape (P4).
func decodeDenialEntry(dec *json.Decoder) (streamDenialEntry, bool) {
	tok, err := dec.Token()
	if err != nil {
		return streamDenialEntry{}, false
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return streamDenialEntry{}, false
	}
	var e streamDenialEntry
	var seenID, seenName, seenInput bool
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return streamDenialEntry{}, false
		}
		key, ok := kt.(string)
		if !ok {
			return streamDenialEntry{}, false
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return streamDenialEntry{}, false
		}
		switch key {
		case "tool_use_id":
			if seenID {
				return streamDenialEntry{}, false
			}
			seenID = true
			s, ok := strictBoundedString(v, maxCoordinatorToolID)
			if !ok || !validCoordinatorToken(s, maxCoordinatorToolID) {
				return streamDenialEntry{}, false
			}
			e.ToolUseID = s
		case "tool_name":
			if seenName {
				return streamDenialEntry{}, false
			}
			seenName = true
			s, ok := strictBoundedString(v, maxCoordinatorToolName)
			if !ok || !validCoordinatorToken(s, maxCoordinatorToolName) {
				return streamDenialEntry{}, false
			}
			e.ToolName = s
		case "tool_input":
			if seenInput {
				return streamDenialEntry{}, false
			}
			seenInput = true
			// The frozen wire contract requires tool_input to be a JSON
			// object. Reject non-object values (string, number, array,
			// null) before canonicalization.
			if len(v) == 0 || v[0] != '{' {
				return streamDenialEntry{}, false
			}
			canon, err := canonicalJSON(v)
			if err != nil || len(canon) > maxToolInputBytes {
				return streamDenialEntry{}, false
			}
			e.InputDigest = sha256Hex(canon)
		default:
			return streamDenialEntry{}, false // unknown entry field
		}
	}
	if _, err := dec.Token(); err != nil { // consume '}'
		return streamDenialEntry{}, false
	}
	if !seenID || !seenName || !seenInput {
		return streamDenialEntry{}, false
	}
	return e, true
}

// joinDeferred matches a tool_deferred result against a pending observation.
// On match, ingests a non-actionable record. Capacity is checked against
// maxActiveApprovals; store admission failure rolls back the active entry.
func (rt *claudeManagedRuntime) joinDeferred(d *streamDeferred) (approvalID string, admittedOperational bool) {
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
	approvalID = "claude-" + approvalToken

	// C2D-B: preserve private identity before Store admission.
	// Roll back on admission failure so the capacity is not leaked.
	identityCreated := false
	if rt.coordinator != nil {
		pokitRT := RuntimeRef{Adapter: claudeHeadlessAdapter, Version: rt.authorityVersion, LaunchGen: rt.epoch, StreamGen: 0}
		if !rt.coordinator.ReserveIdentity(approvalID, sessionID, toolUseID, toolName, inputDigest, pending.catalogActionID, rt.sessionID, pokitRT) {
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

	// C3D-A: an ACTIVE runtime ingests a catalog-matched deferred request as
	// ACTIONABLE with exactly the two certified options and daemon-generated
	// delivery material; the Store admission boundary independently
	// re-verifies the exact tuple. Everything else (inactive runtime,
	// non-catalog observation) keeps the frozen C1D non-actionable
	// zero-option ingest. Actionability is immutable in the store — no
	// record is ever upgraded later. actionableActive is set once before
	// pump() starts (happens-before via the go statement), like resumeCtx.
	actionable := rt.actionableActive && pending.catalogActionID != ""
	var options []agent.InteractionOption
	var material []ApprovalDeliveryMaterial
	requiredPerm := ""
	if actionable {
		options = claudeCertifiedOptions()
		material = claudeCertifiedDeliveryMaterial()
		requiredPerm = claudeRequiredPerm
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
				Options:    options,
				Source:     agent.SourceJSONL,
				Confidence: 1,
			},
			Provenance:       contract.ProvenanceProviderHook,
			Actionable:       actionable,
			RequiredPerm:     requiredPerm,
			CatalogActionID:  pending.catalogActionID,
			DeliveryMaterial: material,
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
	return approvalID, !term2
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
			rt.deferredExit.Store(true)
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
	var streamProbe struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &streamProbe) == nil &&
		(streamProbe.Type == "assistant" || streamProbe.Type == "stream_event") {
		rt.emitStreamObserved(streamProbe.Type)
	}
	var event streamDeferred
	if err := json.Unmarshal(line, &event); err == nil && event.Type == "result" && event.StopReason == "tool_deferred" {
		if approvalID, admitted := rt.joinDeferred(&event); admitted {
			rt.emitOperational(OperationalApprovalRequested, approvalID, "tool_deferred", approvalID)
		}
		return
	}
	// R4: denial-shaped events (result lines with permission_denials)
	// are ONLY consumption witnesses for deny decisions. An allow
	// decision uses PostToolUse as its witness. Skip denial processing
	// for allow — the empty permission_denials:[] in the normal resume
	// result line is not a denial witness and must never cancel the
	// entry.
	if rt.resumeCtx != nil && rt.resumeCtx.expectedDecision == "deny" {
		switch d, outcome := decodeDenialResult(line); outcome {
		case denialOK:
			rt.routeDenial(d)
		case denialMalformed:
			// R6-B P6: a denial-shaped line that fails strict
			// decoding is never skipped.
			rt.failClosedDenial()
		}
	}
}

// failClosedDenial cancels the active resume entry after a malformed or
// cross-bound denial-shaped event. A C1D observation runtime has no resume
// context and nothing to cancel.
func (rt *claudeManagedRuntime) failClosedDenial() {
	ctx := rt.resumeCtx
	if ctx == nil || ctx.coordinator == nil {
		return
	}
	ctx.coordinator.CancelEntry(ctx.claimToken)
}

// routeDenial validates one strictly decoded denial event against the
// runtime-owned immutable resume context and routes the witness.
//
// R6-B P1 + R4: routeDenial passes the strictly decoded denial event and
// the claim token to the coordinator's MarkDenialWitness. Under one
// coordinator lock, the operation verifies exactly one denial entry matches
// the bound ResumeAttemptIdentity, then performs the witness transition.
func (rt *claudeManagedRuntime) routeDenial(d *streamDenial) {
	ctx := rt.resumeCtx
	if ctx == nil || ctx.coordinator == nil {
		return // C1D observation runtime: never a witness source
	}
	// R4: pass all bounded denial entries + the claim token to the
	// coordinator. The coordinator selects exactly one matching entry
	// under one lock.
	result := ctx.coordinator.MarkDenialWitness(
		ctx.claimToken, d.SessionID, d.Entries, ctx.originalRuntime)
	// R4: only cancel the entry on identity mismatch or ambiguous
	// duplicate — real tampering evidence. Stale (entry already
	// consumed by PostToolUse) and Pending are harmless.
	if result.Outcome == WitnessMismatch || result.Outcome == WitnessDuplicate {
		rt.failClosedDenial()
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
		if rt.coordinator != nil && !rt.deferredExit.Load() {
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
			if rt.deferredExit.Load() {
				// C3D §6: the expected joined-deferred exit IS the claim
				// window of the C0D lifecycle (the initial process exits
				// after tool_deferred; the decision is delivered to a
				// --resume incarnation). The pending record and the
				// coordinator identity are preserved together; the window
				// is bounded by the store expiry, and explicit
				// Stop/Kill/Delete or the consumption witness revokes it.
				// The StreamGen=1 high-water is NOT installed here — a late
				// same-generation ingest is already impossible (turnClosed,
				// cleared observations, atomicTerminated).
			} else {
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
		}
		rt.reg.MarkExited(rt.sessionID, rt.epoch)
		rt.emitFinished()
		close(rt.exited)
	})
}

func (rt *claudeManagedRuntime) stop() { rt.terminate() }

func (rt *claudeManagedRuntime) emitOperational(kind OperationalEventKind, sourceID, sourcePosition, referenceID string) {
	submitOperationalAfterCommit(rt.operationalSink(), OperationalEvent{
		Kind: kind, Provider: "claude", SessionID: rt.sessionID,
		RuntimeID: rt.runtimeID, LaunchGeneration: rt.epoch,
		SourceID: sourceID, SourcePosition: sourcePosition,
		ReferenceID: referenceID, OccurredAt: clockNow().UTC(),
	})
}

func (rt *claudeManagedRuntime) setOperationalSink(sink OperationalEventSink) {
	rt.operationalMu.Lock()
	rt.operational = sink
	rt.operationalMu.Unlock()
}

func (rt *claudeManagedRuntime) operationalSink() OperationalEventSink {
	rt.operationalMu.RLock()
	defer rt.operationalMu.RUnlock()
	return rt.operational
}

func (rt *claudeManagedRuntime) emitStreamObserved(streamType string) {
	seq := rt.operationalSeq.Add(1)
	rt.emitOperational(OperationalStreamObserved, rt.runtimeID,
		fmt.Sprintf("%s/%d", streamType, seq), rt.runtimeID)
}

func (rt *claudeManagedRuntime) emitFinished() {
	rt.finishOnce.Do(func() {
		rt.emitOperational(OperationalProviderInvocationFinished, rt.runtimeID, "runtime/exited", rt.runtimeID)
	})
}

// LaunchCertification returns the immutable per-incarnation launch
// certification tuple (C3D §12). Exported for composition tests.
func (rt *claudeManagedRuntime) LaunchCertification() ClaudeLaunchCertification {
	return rt.launchCert
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

	approvals   *AuthoritativeApprovalStore
	operational OperationalEventSink

	// actionable is the C3D-A activation state: set ONLY by the single
	// InstallApprovalExecution transition, before the first epoch, never
	// toggled mid-life. Copied per-runtime at create time.
	actionable bool

	// C2D-B: private one-shot resume coordinator. Identities survive
	// individual Claude process exit. Built eagerly; no delivery path
	// is active until C2D-C wires it.
	coordinator *claudeResumeCoordinator

	createBarrier func(stage string)

	// 9.4-D: mandatory mutation authorizer. Nil not permitted.
	authorizer devicetrust.MutationAuthorizer
}

func (s *ManagedClaudeService) barrier(stage string) {
	if s.createBarrier != nil {
		s.createBarrier(stage)
	}
}

func NewManagedClaudeService(authorizer devicetrust.MutationAuthorizer, cfg ClaudeEntryConfig, launcher ManagedLauncher, attestor ClaudeAttestor) (*ManagedClaudeService, error) {
	if authorizer == nil {
		return nil, fmt.Errorf("managed claude mutation authorizer is required")
	}
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
		coordinator:      nil,
		authorizer:       authorizer,
	}
	coordinator, err := NewClaudeResumeCoordinator(authorizer)
	if err != nil {
		return nil, err
	}
	s.coordinator = coordinator
	s.leaseCond = sync.NewCond(&s.mu)
	return s, nil
}

func NewManagedClaudeServiceForTest(launcher ManagedLauncher, attestor ClaudeAttestor, authorizer devicetrust.MutationAuthorizer) *ManagedClaudeService {
	s, err := NewManagedClaudeService(authorizer, ClaudeEntryConfig{
		Bin:              "/pinned/test/claude",
		Version:          "2.1.209",
		AuthorityVersion: "2.1.209",
		PinnedPath:       "/pinned/test/claude",
		PinnedDigest:     "0000000000000000000000000000000000000000000000000000000000000000",
	}, launcher, attestor)
	if err != nil {
		panic(err)
	}
	return s
}

func (s *ManagedClaudeService) Registry() *ManagedSessionRegistry { return s.reg }

// AuthorityVersion returns the certified authority version this service was
// configured with. Used by the catalog to cross-validate RuntimeOf results.
func (s *ManagedClaudeService) AuthorityVersion() string { return s.cfg.AuthorityVersion }

// Coordinator returns the C2D-B resume coordinator. Exported for composition tests.
func (s *ManagedClaudeService) Coordinator() *claudeResumeCoordinator { return s.coordinator }

// WaitExited blocks until the runtime for the given session exits.
func (s *ManagedClaudeService) WaitExited(pokitSessionID string) {
	s.mu.Lock()
	rt, ok := s.runtimes[pokitSessionID]
	s.mu.Unlock()
	if !ok {
		return
	}
	<-rt.exited
}

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

// SetOperationalEventSink installs the optional neutral observer before the
// first Claude runtime generation exists.
func (s *ManagedClaudeService) SetOperationalEventSink(sink OperationalEventSink) error {
	if sink == nil {
		return fmt.Errorf("claude operational event sink configure: nil sink")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return fmt.Errorf("claude operational event sink configure: service is shutting down")
	}
	if s.gen != 0 || len(s.runtimes) != 0 {
		return fmt.Errorf("claude operational event sink configure: a managed runtime already exists")
	}
	if s.operational != nil {
		return fmt.Errorf("claude operational event sink configure: a different sink is already configured")
	}
	s.operational = sink
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
func (s *ManagedClaudeService) CreateDetached(cwd string, deviceID string, deviceEpoch uint64) (string, error) {
	if err := s.authorizer.AuthorizeCommit(deviceID, deviceEpoch, devicetrust.IntentSessionCreate); err != nil {
		return "", err
	}
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

	// C3D §12: build the per-incarnation launch-certification tuple for THIS
	// spawn and re-verify it through the single launch-binding validator. On
	// an installed (actionable) service a non-certified or incomplete
	// incarnation fails the create closed; an observation-only service
	// proceeds and the tuple records the honest result (RuntimeOf can never
	// resolve it).
	launchCert := buildClaudeLaunchCertification(s.cfg, proc, id, epoch, clockNow())
	s.mu.Lock()
	installed := s.actionable
	s.mu.Unlock()
	if installed && !validClaudeLaunchCertification(launchCert, nil, s.cfg, id, epoch) {
		_ = proc.Kill()
		_ = proc.Wait()
		rollback()
		reason := launchCert.Reason
		if reason == "" {
			reason = "launch binding invalid"
		}
		return "", fmt.Errorf("managed claude launch certification: %s", reason)
	}

	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		_ = proc.Kill()
		_ = proc.Wait()
		rollback()
		return "", fmt.Errorf("managed claude service is shutting down")
	}
	rt := newClaudeManagedRuntime(s.authorizer, proc, epoch, s.reg, bridge, hookDir)
	rt.sessionID = id
	rt.runtimeID = launchCert.ProcessID
	rt.cwd = cwd
	rt.approvals = s.approvals
	rt.authorityVersion = s.cfg.AuthorityVersion
	rt.coordinator = s.coordinator
	// C3D-A: activation state and launch certification are bound to the
	// session/epoch at birth and never toggled mid-life.
	rt.actionableActive = s.actionable
	rt.launchCert = launchCert
	s.runtimes[id] = rt
	// Publish to the bridge after every identity/authority field is set so
	// a hook handler that observes a non-nil rt through getRT() sees a
	// fully-initialized runtime. Hooks that arrive before publication
	// observe nil and answer the safe defer.
	bridge.publishRuntime(rt)
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
		SessionID: id,
		Provider:  "claude",
		Version:   s.cfg.Version,
		Epoch:     epoch,
		// C3D §12: every certification-bound identity field is copied from
		// the ONE immutable launch tuple, so the registry record and the
		// runtime-held tuple agree by construction and RuntimeOf's full
		// comparison (validClaudeLaunchCertification) can detect any later
		// record forgery or replacement.
		ProcessID:       launchCert.ProcessID,
		OS:              launchCert.OS,
		Arch:            launchCert.Arch,
		CreatedAt:       launchCert.SpawnedAt,
		CertifiedDigest: launchCert.ArtifactDigest,
		PID:             launchCert.PID,
		HookDir:         hookDir,
		AttestorKind:    launchCert.AttestorKind,
		CertResult:      launchCert.Result,
		CertReason:      launchCert.Reason,
	}
	if err := s.reg.Register(rec); err != nil {
		return fail("register", err, false)
	}
	// The registry is committed before composition issues the runtime's opaque
	// operational sender. Assign under s.mu so Shutdown cannot observe a
	// partially-installed runtime sender. The runtime never receives the
	// shared binder.
	s.mu.Lock()
	if s.closing || s.runtimes[id] != rt {
		s.mu.Unlock()
		return fail("operational bind", fmt.Errorf("managed claude service is shutting down"), true)
	}
	rt.setOperationalSink(bindOperationalRuntime(s.operational, OperationalRuntimeIdentity{
		Provider: "claude", SessionID: id, RuntimeID: rt.runtimeID, LaunchGeneration: epoch,
	}))
	s.mu.Unlock()
	s.barrier("post-register")

	rt.emitOperational(OperationalProviderInvocationStarted, rt.runtimeID, "runtime/registered", rt.runtimeID)
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

	// R4: coordinator-owned process pre-binding before spawn.
	// The coordinator MUST be available for a resume to proceed;
	// a nil coordinator means delivery was never wired.
	if ctx.coordinator == nil {
		bridge.close()
		os.RemoveAll(hookDir)
		return nil, fmt.Errorf("managed claude resume process: coordinator not available")
	}
	if !ctx.coordinator.BindResumeProcess(ctx.claimToken, ctx.resumeNonce, epoch) {
		bridge.close()
		os.RemoveAll(hookDir)
		return nil, fmt.Errorf("managed claude resume process: coordinator pre-bind failed")
	}
	// Place the same value in the immutable context for the bridge
	// to pass to ClaimWrite.
	ctx.resumeLaunchGen = epoch

	// R4: pre-bind cleanup helper. ANY post-bind failure must cancel the
	// entry so the delivery sees a terminal non-success.
	cancelEntry := func() {
		ctx.coordinator.CancelEntry(ctx.claimToken)
	}

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
		cancelEntry()
		return nil, fmt.Errorf("managed claude resume launch: %w", err)
	}
	if !lease.setProc(proc) {
		_ = proc.Kill()
		_ = proc.Wait()
		bridge.close()
		os.RemoveAll(hookDir)
		cancelEntry()
		return nil, fmt.Errorf("managed claude service is shutting down")
	}

	// C3D §12: every resume incarnation gets its OWN launch-certification
	// tuple (own epoch, own OpaqueID), re-verified through the single
	// launch-binding validator. A non-certified or incomplete resume fails
	// closed HERE — before the runtime is returned — so an uncertified
	// incarnation can never reach the repeated-hook or witness stage.
	// Witness authority still validates against the ORIGINAL RuntimeRef in
	// ctx.
	launchCert := buildClaudeLaunchCertification(s.cfg, proc, ctx.pokitSessionID, epoch, clockNow())
	if !validClaudeLaunchCertification(launchCert, nil, s.cfg, ctx.pokitSessionID, epoch) {
		_ = proc.Kill()
		_ = proc.Wait()
		bridge.close()
		os.RemoveAll(hookDir)
		cancelEntry()
		reason := launchCert.Reason
		if reason == "" {
			reason = "launch binding invalid"
		}
		return nil, fmt.Errorf("managed claude resume launch certification: %s", reason)
	}

	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		_ = proc.Kill()
		_ = proc.Wait()
		bridge.close()
		os.RemoveAll(hookDir)
		cancelEntry()
		return nil, fmt.Errorf("managed claude service is shutting down")
	}
	rt := newClaudeManagedRuntime(s.authorizer, proc, epoch, s.reg, bridge, hookDir)
	rt.sessionID = ctx.pokitSessionID
	rt.runtimeID = launchCert.ProcessID
	rt.cwd = cwd
	rt.coordinator = s.coordinator
	rt.authorityVersion = s.cfg.AuthorityVersion
	// C3D §12: the resume incarnation's certification is recorded with the
	// delivery attempt's runtime.
	rt.launchCert = launchCert
	// R6-B: the pump reads the runtime-owned immutable context, not the
	// bridge (closes the R6-A3 finding). Set before pump() starts.
	rt.resumeCtx = ctx
	rt.resumePreviousRuntime = s.runtimes[ctx.pokitSessionID]
	if previous, ok := s.reg.Get(ctx.pokitSessionID); ok {
		rt.resumePreviousRecord = previous
	}

	// Register and publish the resumed incarnation before its Started event.
	// Timeline verification reads this exact provider+ProcessID+epoch tuple;
	// leaving the original record current would reject every resume event.
	// Holding s.mu makes registry replacement and current-runtime publication
	// atomic relative to Shutdown and lifecycle lookups.
	rec := ManagedSessionRecord{
		SessionID:       ctx.pokitSessionID,
		Provider:        "claude",
		Version:         s.cfg.Version,
		Epoch:           epoch,
		ProcessID:       launchCert.ProcessID,
		OS:              launchCert.OS,
		Arch:            launchCert.Arch,
		CreatedAt:       launchCert.SpawnedAt,
		CertifiedDigest: launchCert.ArtifactDigest,
		PID:             launchCert.PID,
		HookDir:         hookDir,
		AttestorKind:    launchCert.AttestorKind,
		CertResult:      launchCert.Result,
		CertReason:      launchCert.Reason,
	}
	// Revoke before changing the registry current generation. An old submit
	// that acquired its sender before this point belongs to the still-current
	// N incarnation; every later submit is stale before N+1 becomes current.
	previous := rt.resumePreviousRuntime
	if previous != nil {
		revokeOperationalRuntime(previous.operationalSink())
	}
	s.barrier("post-resume-revoke")
	if err := s.reg.RegisterIncarnation(ctx.originalRuntime.LaunchGen, rec); err != nil {
		// Registration did not replace N, so a live original may receive a
		// fresh sender. Never resurrect one that terminated while the failed
		// resume was being prepared.
		if previous != nil {
			previous.rebindOperationalSinkIfLive(s.operational, OperationalRuntimeIdentity{
				Provider: "claude", SessionID: previous.sessionID,
				RuntimeID: previous.runtimeID, LaunchGeneration: previous.epoch,
			})
		}
		s.mu.Unlock()
		_ = proc.Kill()
		_ = proc.Wait()
		bridge.close()
		os.RemoveAll(hookDir)
		cancelEntry()
		return nil, fmt.Errorf("managed claude resume register: %w", err)
	}
	// RegisterIncarnation is the commit point for this replacement. Bind the
	// new opaque sender before publishing the runtime or emitting Started.
	rt.setOperationalSink(bindOperationalRuntime(s.operational, OperationalRuntimeIdentity{
		Provider: "claude", SessionID: ctx.pokitSessionID,
		RuntimeID: rt.runtimeID, LaunchGeneration: epoch,
	}))
	s.runtimes[ctx.pokitSessionID] = rt
	// Publish only after every field and registry identity is committed.
	bridge.publishRuntime(rt)
	s.mu.Unlock()

	s.barrier("post-resume-register")
	rt.emitOperational(OperationalProviderInvocationStarted, rt.runtimeID, "runtime/resumed", rt.runtimeID)
	go rt.pump()
	return rt, nil
}

// finishApprovalResume terminates the transient resume incarnation. If its
// original process was still genuinely live (a controlled test/compatibility
// path), restore that exact registry/runtime identity after the resume Finish
// event has revoked its capability. Normal joined-deferred originals are
// already terminal and are never restored. An explicit Stop/Kill intent on
// the resume generation also permanently suppresses restoration.
func (s *ManagedClaudeService) finishApprovalResume(rt *claudeManagedRuntime) {
	if rt == nil {
		return
	}
	rt.terminate()
	previous := rt.resumePreviousRuntime
	record := rt.resumePreviousRecord
	if previous == nil || record.SessionID == "" || record.Exited ||
		previous.atomicTerminated.Load() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous.atomicTerminated.Load() || s.closing || s.runtimes[rt.sessionID] != rt || rt.terminalIntent {
		return
	}
	if err := s.reg.RestoreIncarnation(rt.epoch, record); err != nil {
		return
	}
	s.barrier("post-resume-restore")
	// Restore reactivates a live original runtime whose prior sender was
	// revoked at replacement. It must receive a fresh, generation-bound sender
	// rather than reusing the stale capability.
	previousLive := previous.rebindOperationalSinkIfLive(s.operational, OperationalRuntimeIdentity{
		Provider: "claude", SessionID: record.SessionID,
		RuntimeID: previous.runtimeID, LaunchGeneration: previous.epoch,
	})
	s.runtimes[rt.sessionID] = previous
	if !previousLive {
		// The original exited after the outer pre-check. Keep its restored
		// registry record terminal and never leave a newly-bound sender active.
		s.reg.MarkExited(record.SessionID, record.Epoch)
	}
}

// rebindOperationalSinkIfLive checks original termination and installs a
// fresh sender under the same slot lock used by emitOperational. If terminate
// wins immediately after the check, its later Finished emission observes and
// revokes this new sender; if it won earlier, no sender is installed.
func (rt *claudeManagedRuntime) rebindOperationalSinkIfLive(
	sink OperationalEventSink,
	identity OperationalRuntimeIdentity,
) bool {
	rt.operationalMu.Lock()
	defer rt.operationalMu.Unlock()
	if rt.atomicTerminated.Load() {
		return false
	}
	rt.operational = bindOperationalRuntime(sink, identity)
	return true
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

func (s *ManagedClaudeService) Stop(sessionID string, epoch int64, deviceID string, deviceEpoch uint64) error {
	rt, coord, approvals, err := s.claimTerminalIntent(sessionID, epoch, deviceID, deviceEpoch, devicetrust.IntentSessionStop)
	if err != nil {
		return err
	}
	if rec, ok := s.reg.Get(sessionID); !ok {
		return fmt.Errorf("managed claude session not found")
	} else if rec.Exited {
		// Deferred exit preserves the claim window (coordinator identities
		// + pending record). Explicit Stop revokes both: identities are
		// cleared and the Store high-water advances to StreamGen=1, which
		// supersedes pending AND executing authority (a mid-delivery
		// commit becomes stale).
		if coord != nil {
			coord.ClearRuntime(sessionID, epoch)
		}
		if approvals != nil {
			_ = approvals.InstallRuntimeGeneration(sessionID, epoch, 1, "stopped")
		}
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
	rt.deferredExit.Store(true)
	rt.turnMu.Unlock()
	rt.terminate()
	return nil
}

func (s *ManagedClaudeService) Kill(sessionID string, epoch int64, deviceID string, deviceEpoch uint64) error {
	rt, coord, approvals, err := s.claimTerminalIntent(sessionID, epoch, deviceID, deviceEpoch, devicetrust.IntentSessionKill)
	if err != nil {
		return err
	}
	if rec, ok := s.reg.Get(sessionID); !ok {
		return fmt.Errorf("managed claude session not found")
	} else if rec.Exited {
		// Deferred exit preserves the claim window (coordinator identities
		// + pending record). Explicit Kill revokes both, same as Stop.
		if coord != nil {
			coord.ClearRuntime(sessionID, epoch)
		}
		if approvals != nil {
			_ = approvals.InstallRuntimeGeneration(sessionID, epoch, 1, "killed")
		}
		return nil
	}
	rt.terminate()
	return nil
}

// claimTerminalIntent resolves the exact current runtime and records that an
// external lifecycle command owns its terminal state. The write is performed
// under the same service lock used by finishApprovalResume's restore check, so
// Stop/Kill and an older-generation restore have one deterministic order.
func (s *ManagedClaudeService) claimTerminalIntent(
	sessionID string,
	epoch int64,
	deviceID string,
	deviceEpoch uint64,
	intent devicetrust.MutationIntent,
) (*claudeManagedRuntime, *claudeResumeCoordinator, *AuthoritativeApprovalStore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt := s.runtimes[sessionID]
	if rt == nil {
		return nil, nil, nil, fmt.Errorf("managed claude session not found")
	}
	if rt.epoch != epoch {
		return nil, nil, nil, fmt.Errorf("stale session epoch")
	}
	// 9.4-D: atomic device authorization under s.mu before state change.
	if err := s.authorizer.AuthorizeCommit(deviceID, deviceEpoch, intent); err != nil {
		return nil, nil, nil, err
	}
	rt.terminalIntent = true
	return rt, s.coordinator, s.approvals, nil
}

func (s *ManagedClaudeService) Delete(sessionID string, epoch int64, deviceID string, deviceEpoch uint64) error {
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
	if err := s.authorizer.AuthorizeCommit(deviceID, deviceEpoch, devicetrust.IntentSessionDelete); err != nil {
		s.mu.Unlock()
		return err
	}
	rt := s.runtimes[sessionID]
	delete(s.runtimes, sessionID)
	approvals := s.approvals
	coord := s.coordinator
	s.mu.Unlock()
	// Deferred exit preserves coordinator identities. Clean them up.
	if rt != nil && coord != nil {
		coord.ClearRuntime(sessionID, epoch)
	}
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
