package term

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent"
	claude "devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202"
	codex "devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// TelemetryService owns the telemetry state machine and background sampling loop.
// It replaces the telemetryCache package global.
type TelemetryService struct {
	reg         *mux.Registry
	events      EventStore
	notifier    Notifier
	detector    AgentDetector                            // Phase A5: optional agent detector (nil if not wired)
	approvals   *AuthoritativeApprovalStore              // A1: generation-bound approval store
	activity    *ActivityBuffer                          // E8f2: activity capture
	transcript  *transcript.Service                      // T3: AgentEvent → Transcript projection
	logResolver func(models.ProcessInfo) (LogRef, error) // Phase A5b: injectable resolver (nil = production ResolveAgentLog)
	interval    time.Duration
	// statusStore is the S1 session-owned agent-activity store. It is fed from the
	// SAME accepted-adapter read path as Transcript (no second reader) and holds
	// the advisory activity result separately from lifecycle/health.
	statusStore *AgentStatusStore

	// A1 R3-C: the per-session runtime delivery gate. Telemetry drives its
	// activation/deactivation from the real lifecycle so approval delivery
	// acceptance is linearized against launch/stream replacement, correlation loss,
	// and delete/unlink/termination. nil in tests that do not exercise delivery.
	deliveryGate *RuntimeDeliveryGate

	mu       sync.Mutex
	sessions map[string]*sessionStateData
	done     chan struct{}
}

// NewTelemetryService creates a TelemetryService. Call Run() to start sampling.
func NewTelemetryService(reg *mux.Registry, events EventStore, notifier Notifier, detector AgentDetector, approvals *AuthoritativeApprovalStore, activity *ActivityBuffer, transcriptSvc *transcript.Service) *TelemetryService {
	if notifier == nil {
		notifier = NoopNotifier{}
	}
	if approvals == nil {
		approvals = NewAuthoritativeApprovalStore()
	}
	return &TelemetryService{
		reg:         reg,
		events:      events,
		notifier:    notifier,
		detector:    detector,
		approvals:   approvals,
		activity:    activity,
		transcript:  transcriptSvc,
		interval:    2 * time.Second,
		statusStore: NewAgentStatusStore(),
		sessions:    make(map[string]*sessionStateData),
		done:        make(chan struct{}),
	}
}

// Run starts the background telemetry sampling loop. Blocks until ctx is cancelled.
// The caller should call Done() to observe completion.
func (s *TelemetryService) Run(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		sessions := s.reg.Sessions(ctx)
		processSnapshots, batchAdapters, failedAdapters := collectProcessSnapshots(ctx, s.reg.Adapters())

		s.reconcileSessions(sessions)

		for _, sess := range sessions {
			s.processSession(ctx, sess, processSnapshots, batchAdapters, failedAdapters)
		}
	}
}

// reconcileSessions adds newly-seen sessions and prunes ones that have left the
// Registry. A pruned session's cached telemetry AND its S1 agent-activity record
// are cleared (bounded state; no inheritance if the id is later reused). This is
// the production owner of "Registry disappearance" cleanup.
func (s *TelemetryService) reconcileSessions(sessions []mux.Session) {
	s.mu.Lock()
	activeSet := make(map[string]bool, len(sessions))
	for _, sess := range sessions {
		id := sess.AdapterName() + ":" + sess.ID()
		activeSet[id] = true
		if s.sessions[id] == nil {
			s.sessions[id] = &sessionStateData{
				LastActivity: time.Now(),
				State:        "idle",
				Load:         0,
			}
		}
	}
	// Collect the pruned (registry-disappeared) IDs under s.mu, then perform the
	// external store/gate cleanup AFTER releasing s.mu so no other lock (status store,
	// approval store, delivery gate) is ever nested under the telemetry mutex.
	var pruned []string
	for id := range s.sessions {
		if !activeSet[id] {
			delete(s.sessions, id)
			pruned = append(pruned, id)
		}
	}
	s.mu.Unlock()

	for _, id := range pruned {
		if s.statusStore != nil {
			s.statusStore.Clear(id)
		}
		// A1-C: a session that disappeared holds no live approval authority.
		if s.approvals != nil {
			s.approvals.Clear(id)
		}
		// A1 R4-B: registry disappearance is a distinct production cleanup owner —
		// deactivate the exact generation-owned delivery endpoint so a terminated
		// generation accepts no further bytes.
		if s.deliveryGate != nil {
			s.deliveryGate.Deactivate(id)
		}
	}
}

func (s *TelemetryService) processSession(ctx context.Context, sess mux.Session, processSnapshots map[string]models.ProcessInfo, batchAdapters, failedAdapters map[string]bool) {
	id := sess.AdapterName() + ":" + sess.ID()

	// E8f2: ensure recorder exists for session (lifecycle-first, not WebSocket-born).
	if opener, ok := sess.(mux.StreamOpener); ok {
		EnsureRecorder(id, opener, s.activity)
	}

	var logRef LogRef
	var logErr error = fmt.Errorf("no log")

	// 1. ProcessProvider (link-based resolver removed in PA2a)
	if logErr != nil {
		if info, ok := processSnapshots[id]; ok {
			logRef, logErr = s.resolveLog(ctx, info)
		} else if !batchAdapters[sess.AdapterName()] {
			if pp, ok := sess.(mux.ProcessProvider); ok {
				if pinfo, err := pp.ProcessInfo(ctx); err == nil {
					logRef, logErr = s.resolveLog(ctx, pinfo)
				}
			}
		}
	}

	var parsedNewEvents bool
	var lastEvent models.AgentEvent

	if logErr == nil {
		s.mu.Lock()
		stateData := s.sessions[id]
		if stateData != nil {
			if stateData.Cursor == nil || stateData.Cursor.Path != logRef.Path {
				stateData.Cursor = &LogCursor{Path: logRef.Path, Offset: 0, Inode: 0}
				switch logRef.Agent {
				case "claude":
					stateData.Parser = &ClaudeParser{Session: id}
				case "codex":
					stateData.Parser = &CodexParser{Session: id}
				case "gemini":
					stateData.Parser = &GeminiParser{Session: id}
				case "antigravity":
					stateData.Parser = &AntigravityParser{Session: id}
				}
			}
			cursor := stateData.Cursor
			parser := stateData.Parser
			s.mu.Unlock()

			if parser != nil {
				rr := readRawLines(cursor, 500)
				rawLines := rr.Lines

				// ── B1: Accepted adapter path — INDEPENDENT of legacy parser ──
				// Must run from rawLines directly. Records the legacy parser
				// ignores must still reach the accepted adapter, advance the
				// cursor, and produce semantic Transcript segments.
				if s.transcript != nil && isAcceptedAdapter(logRef.Agent) {
					if stateData.Adapter == nil {
						stateData.Adapter = newAdapterState()
					}
					a := stateData.Adapter
					// S1.1-B: the launch-instance generation gates every status write
					// this poll so a delayed write bound to a replaced launch is rejected.
					// Looked up once; the binding is re-read for correlation below.
					launchBinding := transcript.LookupLaunch(id)
					launchGen := int64(0)
					if launchBinding != nil {
						launchGen = launchBinding.Generation
					}
					prevPath := a.path
					if rr.GenerationChanged || a.path != logRef.Path {
						a.resetForGeneration(logRef.Path)
						// E1/B3: a stream-generation CHANGE invalidates any prior positive
						// status IMMEDIATELY. This is NOT a full Clear (reserved for lifecycle
						// delete): it stores a non-current unknown+degraded record AT the new
						// generation, raising the generation high-water mark so a delayed
						// OLDER-generation update/revoke cannot resurrect the prior status.
						// The INITIAL generation (prevPath == "") is not a change, so a
						// never-seen session gets no phantom record here.
						if s.statusStore != nil && prevPath != "" {
							s.statusStore.Invalidate(id, launchGen, a.streamGen, "stream generation changed")
						}
						// A1 R2-C: a stream-generation change supersedes prior pending AND
						// executing approval authority and advances the runtime high-water,
						// so an in-flight claim from the old stream generation cannot commit.
						if s.approvals != nil && prevPath != "" {
							s.approvals.SupersedeRuntime(id, launchGen, a.streamGen, "stream generation changed")
						}
						if s.deliveryGate != nil && prevPath != "" {
							s.deliveryGate.Deactivate(id)
						}
					}
					a.appendRecords(rawLines)
					records, acursor, overflowed := a.buildAdapterInput()
					if overflowed {
						// B3: at most one degraded marker per generation.
						if a.shouldEmitOverflowMarker() {
							s.transcript.EmitDegraded(id, "semantic ingestion overflowed", now())
						}
						// S1-C: ingestion overflow revokes activity authority for this
						// generation → safe unknown+degraded (no fabricated status).
						if s.statusStore != nil {
							s.statusStore.Revoke(id, launchGen, a.streamGen, a.version, "semantic ingestion overflowed")
						}
					} else {
						acceptedEvents, adapterVersion, nextCursor, degraded := callAcceptedAdapter(logRef.Agent, records, id, acursor)
						a.setCursor(nextCursor)
						// B2: adapter degraded/version-conflict → revoke authority.
						// Per-record EventUnknown may set degraded=true without a
						// version problem. Only revoke when version is not validated.
						if adapterVersion != "" {
							a.updateVersion(adapterVersion, logRef.Agent)
						}
						if degraded && !a.versionValid {
							a.markVersionConflict()
						}
						pid, start := getProcessIdentity(ctx, sess, id, processSnapshots, batchAdapters)
						// R2: correlate against the runtime's ACTUAL terminal adapter, not
						// a constant — a binding recorded for a different adapter fails closed.
						corr := a.launchCorrelation(launchBinding, sess.AdapterName(), logRef.Agent, pid, start)
						s.transcript.SetCorrelation(id, transcript.CorrelationState{
							SessionID:   id,
							Correlation: corr,
							Provider:    logRef.Agent,
						})
						if corr == contract.CorrelationManagedLaunch {
							if len(acceptedEvents) > 0 {
								s.transcript.ProjectAgentEvents(id, acceptedEvents)
							}
						}
						// S1-C: feed the SAME accepted, version/correlation-gated batch
						// to the session-owned agent-activity store. Positive status only
						// from correlated new events; a version conflict revokes authority;
						// a previously-valid session that LOSES correlation is revoked to
						// unknown+degraded (a never-correlated session stays absent).
						// S1.1-B: every write carries the launch-instance generation so a
						// delayed write bound to a replaced launch is rejected.
						if s.statusStore != nil {
							switch {
							case a.versionConflict:
								s.statusStore.Revoke(id, launchGen, a.streamGen, a.version, "accepted version conflict")
							case corr == contract.CorrelationManagedLaunch:
								if len(acceptedEvents) > 0 {
									s.statusStore.Update(AgentStatusUpdate{
										SessionID: id, LaunchGen: launchGen, Generation: a.streamGen, Version: a.version,
										Events: acceptedEvents, Adapter: acceptedAdapterFor(logRef.Agent),
									})
								}
							default:
								// correlation unavailable (not a version conflict): revoke a
								// previously-valid status; leave never-correlated sessions absent.
								s.statusStore.RevokeIfPresent(id, launchGen, a.streamGen, a.version, "correlation unavailable")
							}
						}

						// A1-C: approval authority follows the SAME accepted, version/
						// correlation-gated batch as status. Requests are established ONLY
						// by the accepted adapter's DetectApproval (capability-gated) under
						// a managed launch; a version conflict or a lost correlation
						// invalidates any prior pending authority (fail closed).
						if s.approvals != nil {
							switch {
							case a.versionConflict:
								s.approvals.InvalidateSession(id, "accepted version conflict")
							case corr == contract.CorrelationManagedLaunch:
								s.ingestApprovals(id, launchGen, a.streamGen, logRef.Agent, a.version, acceptedEvents)
							default:
								s.approvals.InvalidateSession(id, "correlation unavailable")
							}
						}
						// A1 R3-C: keep the delivery gate's current runtime generation in
						// sync with the correlated managed launch; a version conflict or
						// lost correlation deactivates it. The production gate has no sink
						// (no provider channel), so this establishes the linearized call
						// graph without accepting any delivery.
						if s.deliveryGate != nil {
							if !a.versionConflict && corr == contract.CorrelationManagedLaunch {
								s.deliveryGate.Activate(id, RuntimeRef{Adapter: logRef.Agent, Version: a.version, LaunchGen: launchGen, StreamGen: a.streamGen}, 0)
							} else {
								s.deliveryGate.Deactivate(id)
							}
						}
					}
				}

				// ── Legacy parser path — unchanged ──
				var newEvents []models.AgentEvent
				for _, line := range rawLines {
					parsed, _ := parser.Parse(line)
					newEvents = append(newEvents, parsed...)
				}
				if len(newEvents) > 0 {
					s.events.Append(id, newEvents)
					parsedNewEvents = true
					lastEvent = newEvents[len(newEvents)-1]

					// A1-C: the legacy parser NO LONGER creates approval authority.
					// Its approval_requested lines can be spoofed and carry no
					// generation/provenance/action binding. Approvals are established
					// only by the accepted adapter's DetectApproval on the accepted
					// batch above. Legacy events remain diagnostic history in the
					// event store only, never an actionable approval.
				}
			}
		} else {
			s.mu.Unlock()
		}
	}

	var out []byte
	var screenErr error
	adapterFailed := failedAdapters[sess.AdapterName()]
	if !adapterFailed {
		if sr, ok := sess.(mux.ScreenReader); ok {
			out, screenErr = sr.ReadScreen(ctx)
		}
	}

	runner := "agent"
	if logRef.Agent != "" {
		runner = logRef.Agent
	}
	runnerColor := "#58a6ff"

	s.mu.Lock()
	stateData := s.sessions[id]
	if stateData == nil {
		s.mu.Unlock()
		return
	}

	stateData.Runner = runner
	stateData.RunnerColor = runnerColor

	if preserveTransientSamplingFailure(stateData, logErr, screenErr, adapterFailed, parsedNewEvents) {
		s.mu.Unlock()
		return
	}

	diffSize := len(out) - len(stateData.LastOutput)
	if diffSize < 0 {
		diffSize = -diffSize
	}

	isWaiting := false
	lines := strings.Split(string(out), "\n")
	tailLines := lines
	if len(lines) > 5 {
		tailLines = lines[len(lines)-5:]
	}
	for _, line := range tailLines {
		if isApprovalPrompt(line) {
			isWaiting = true
			break
		}
	}

	isThinkingFallback := false
	if logErr != nil {
		for _, line := range tailLines {
			if strings.Contains(line, "Thinking...") || strings.Contains(line, "Querying") {
				isThinkingFallback = true
				break
			}
		}
	}

	evaluateState(stateData, parsedNewEvents, lastEvent, logErr, isWaiting, isThinkingFallback, diffSize)

	if isWaiting && s.notifier != nil {
		_ = s.notifier.ApprovalRequired(ctx, id, string(out))
	}

	stateData.LastOutput = out
	s.mu.Unlock()
}

func (s *TelemetryService) resolveLog(ctx context.Context, p models.ProcessInfo) (LogRef, error) {
	if s.logResolver != nil {
		return s.logResolver(p)
	}
	return ResolveAgentLog(ctx, p)
}

// SetLogResolver overrides the production ResolveAgentLog for testing.
func (s *TelemetryService) SetLogResolver(fn func(models.ProcessInfo) (LogRef, error)) {
	s.logResolver = fn
}

// Done returns a channel that closes when the sampling loop has exited.
func (s *TelemetryService) Done() <-chan struct{} { return s.done }

// Snapshot returns a copy of the current telemetry state for all sessions.
func (s *TelemetryService) Snapshot(reg *mux.Registry) []SessionTelemetry {
	sessions := reg.Sessions(context.Background())

	s.mu.Lock()
	stateCopies := make(map[string]*sessionStateData)
	for id, data := range s.sessions {
		copyData := *data
		stateCopies[id] = &copyData
	}
	s.mu.Unlock()

	res := make([]SessionTelemetry, 0)
	for _, sess := range sessions {
		compoundID := sess.AdapterName() + ":" + sess.ID()
		snap, _ := reg.Snapshot(sess.AdapterName())
		var errStr string
		if snap.LastError != nil {
			errStr = snap.LastError.Error()
		}
		isStale := snap.LastError != nil
		events := s.events.List(compoundID)

		data := stateCopies[compoundID]
		if data == nil {
			res = append(res, SessionTelemetry{
				ID: compoundID, DisplayID: sess.ID(), State: "idle", Load: 0,
				Runner: "cat", RunnerColor: "#58a6ff", Adapter: sess.AdapterName(),
				Capabilities:        sessionCapabilities(sess),
				AdapterCapabilities: adapterCapabilityStrings(reg, sess.AdapterName()),
				Events:              events, Stale: isStale, LastSuccessAt: snap.LastSuccessAt, LastError: errStr,
			})
		} else {
			res = append(res, SessionTelemetry{
				ID: compoundID, DisplayID: sess.ID(), State: data.State, Load: data.Load,
				Runner: data.Runner, RunnerColor: data.RunnerColor, Adapter: sess.AdapterName(),
				Capabilities:        sessionCapabilities(sess),
				AdapterCapabilities: adapterCapabilityStrings(reg, sess.AdapterName()),
				Events:              events, Stale: isStale, LastSuccessAt: snap.LastSuccessAt, LastError: errStr,
			})
		}
	}
	sortTelemetry(res)
	// Phase A5: populate agent fields from detector using real process evidence.
	if s.detector != nil {
		for i := range res {
			st := &res[i]
			ref := mux.ParseSessionID(st.ID)
			evidence := ProdDetectionEvidence{TermAdapter: ref.Adapter}
			// Extract real process evidence from the session.
			for _, sess := range sessions {
				if sess.AdapterName() == ref.Adapter && sess.ID() == ref.LocalID {
					if pp, ok := sess.(mux.ProcessProvider); ok {
						if info, err := pp.ProcessInfo(context.Background()); err == nil {
							evidence.ProcessName = info.Command
							evidence.CWD = info.CWD
						}
					}
					break
				}
			}
			kind, _, confidence := s.detector.DetectAgent(st.ID, ref.Adapter, ref.LocalID, evidence)
			st.AgentKind = kind
			st.AgentConfidence = confidence
			// AgentStatus from telemetry state machine (parser-derived).
			if sd, ok := stateCopies[st.ID]; ok && sd.State != "" {
				st.AgentStatus = mapLegacyState(sd.State)
			}
		}
	}
	// Phase A9: populate approvals from ApprovalStore.
	for i := range res {
		st := &res[i]
		st.Approvals = s.approvals.ListSafe(st.ID)
	}
	// S1-D: additive advisory agent-activity projection from the session-owned
	// store. Separate from lifecycle (LifecycleState) and poll health (Stale); a
	// stale record is flagged so the UI does not present it as current activity.
	if s.statusStore != nil {
		for i := range res {
			st := &res[i]
			rec, stale, ok := s.statusStore.Current(st.ID)
			if !ok {
				continue
			}
			st.AgentActivity = &AgentActivityDTO{
				ContractVersion: contract.ContractVersion, // frozen T0 version, not a new one
				Status:          string(rec.Status),
				Provenance:      string(rec.Provenance),
				Confidence:      rec.Confidence,
				Degraded:        rec.Degraded,
				ObservedAt:      rec.ObservedAt.UTC().Format(time.RFC3339),
				Stale:           stale,
			}
		}
	}
	return res
}

// acceptedAdapterFor returns a fresh accepted version-specific adapter instance
// for the given agent kind, or nil if the kind is not an accepted adapter. The
// accepted adapters are stateless for GetStatus/ReadEvents (they delegate to the
// frozen contract), so a fresh instance per call is correct.
func acceptedAdapterFor(kind string) contract.AgentAdapter {
	switch kind {
	case "codex":
		return &codex.Adapter{}
	case "claude":
		return &claude.Adapter{}
	default:
		return nil
	}
}

// SetDeliveryGate wires the production runtime delivery gate (A1 R3-C) so lifecycle
// supersession drives delivery-acceptance linearization.
func (s *TelemetryService) SetDeliveryGate(g *RuntimeDeliveryGate) { s.deliveryGate = g }

// Clear removes all cached telemetry state for a session.
func (s *TelemetryService) Clear(sessionID string) {
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
	// S1-C: clear the session-owned agent-activity record so a recreated session
	// with the same id cannot inherit the old status.
	if s.statusStore != nil {
		s.statusStore.Clear(sessionID)
	}
	// A1-C: drop approval authority on delete so a recreated session with the same
	// canonical id cannot inherit a prior pending request.
	if s.approvals != nil {
		s.approvals.Clear(sessionID)
	}
	// A1 R3-C: session delete/unlink deactivates the delivery gate generation.
	if s.deliveryGate != nil {
		s.deliveryGate.Deactivate(sessionID)
	}
}

// RegisterOrReplaceLaunch is the single production-owned atomic launch
// replacement boundary (S1.1-R3). It delegates to the registry's serialized
// per-session transition, supplying a pre-publication invalidation callback. The
// registry guarantees the ordering under one held session gate:
//
//	allocate strictly-newer generation
//	→ (replacement only) invalidate old status + reset adapter ingestion AT that gen
//	→ publish the new binding with a strict monotonic check (no regression)
//
// so a concurrent telemetry poll can never observe the new launch generation
// before its non-current high-water exists, two concurrent replacements can never
// let a lower generation publish last, and two concurrent first registrations
// cannot both skip invalidation (the gate serializes them; the second sees the
// first's binding and takes the replacement path). This is distinct from lifecycle
// Clear (delete), which removes the record entirely.
func (s *TelemetryService) RegisterOrReplaceLaunch(spec transcript.LaunchSpec) (int64, bool) {
	gen, replaced, _ := transcript.RegisterOrReplaceLaunch(spec, func(gen int64) {
		s.invalidateForLaunch(spec.SessionID, gen)
	})
	// ok is always true here: the callback is non-nil, so a replacement can never
	// be refused for a missing invalidation.
	return gen, replaced
}

// invalidateForLaunch installs a non-current high-water at the new launch
// generation and resets the per-session adapter ingestion epoch. It is invoked by
// the registry transition BEFORE the replacement binding is published (and while
// the session's transition gate is held, but NO registry map lock is held — see
// the audited lock order in launch_binding.go).
func (s *TelemetryService) invalidateForLaunch(sessionID string, launchGen int64) {
	if s.statusStore == nil {
		return
	}
	// Reset the adapter ingestion epoch for the new launch so a stale cursor/
	// prefix from the prior launch cannot feed the new launch generation.
	s.mu.Lock()
	if sd := s.sessions[sessionID]; sd != nil && sd.Adapter != nil {
		sd.Adapter.clear()
	}
	s.mu.Unlock()
	// streamGen 0 at the new launch: the launch axis dominates the generation
	// rule, so this high-water rejects any prior-launch write regardless of its
	// (higher) streamGen.
	s.statusStore.Invalidate(sessionID, launchGen, 0, "launch binding replaced")
	// A1 R2-C: a launch replacement supersedes all prior pending AND executing
	// approval authority and advances the runtime high-water so an in-flight claim
	// from the replaced launch cannot commit; the new launch begins with none.
	if s.approvals != nil {
		s.approvals.SupersedeRuntime(sessionID, launchGen, 0, "launch binding replaced")
	}
	// A1 R3-C: a launch replacement deactivates the old delivery generation so the
	// old generation accepts no bytes.
	if s.deliveryGate != nil {
		s.deliveryGate.Deactivate(sessionID)
	}
}

// firstNonEmpty returns the first non-empty string from the given candidates.
func firstNonEmpty(candidates ...string) string {
	for _, c := range candidates {
		if c != "" {
			return c
		}
	}
	return ""
}

// convertToAgentEvents converts legacy models.AgentEvent to agent.AgentEvent
// for T3 Transcript projection. The agent kind is supplied from log detection.
func convertToAgentEvents(legacy []models.AgentEvent, agentKind string) []agent.AgentEvent {
	out := make([]agent.AgentEvent, 0, len(legacy))
	for _, e := range legacy {
		ts, _ := time.Parse(time.RFC3339, e.Timestamp)
		out = append(out, agent.AgentEvent{
			ID:         e.ID,
			SessionID:  e.Session,
			AgentKind:  agentKind,
			Type:       mapLegacyType(e.Type),
			Text:       e.Detail,
			Confidence: 0.0,          // not authoritative; provenance governs
			Provenance: "native_log", // accepted provenance tier: needs correlation
			Timestamp:  ts,
		})
	}
	return out
}

// isAcceptedAdapter returns true only for agent kinds that have an accepted
// T0 contract.AgentAdapter in production (Codex v0.144.1, Claude v2.1.202).
func isAcceptedAdapter(kind string) bool {
	switch kind {
	case "codex", "claude":
		return true
	default:
		return false
	}
}

// isAcceptedVersion checks that the detected provider version matches
// the accepted adapter version. Unsupported/newer versions must not
// produce semantic Transcript.
func isAcceptedVersion(kind, version string) bool {
	switch kind {
	case "codex":
		return version == "0.144.1"
	case "claude":
		return version == "2.1.202"
	default:
		return false
	}
}

// readRawLines reads raw JSONL lines from the cursor's log file.
// Returns up to maxLines raw byte slices.
func readRawLines(cursor *LogCursor, maxLines int) RawLinesResult {
	if cursor == nil || cursor.Path == "" {
		return RawLinesResult{}
	}
	result, err := ReadRawLines(cursor, maxLines)
	if err != nil {
		return RawLinesResult{}
	}
	return result
}

// getProcessIdentity extracts the PID and process start time for a session for
// launch correlation. It prefers the batch process snapshot (tmux/cmux
// ProcessSnapshotProvider); for a non-batch adapter (controlled_pty) it falls
// back to the session's own ProcessProvider — the SAME source the launch binding
// captured at create — so a managed launch can rediscover its exact runtime
// identity. A missing snapshot AND a session that cannot report identity yields
// (0, zero): fail closed, never a fabricated identity; LaunchCorrelation then
// treats a claimed-but-undiscovered identity as unavailable, never a wildcard.
func getProcessIdentity(ctx context.Context, sess mux.Session, id string, snapshots map[string]models.ProcessInfo, batchAdapters map[string]bool) (int, time.Time) {
	if info, ok := snapshots[id]; ok {
		return info.PID, info.StartedAt
	}
	if !batchAdapters[sess.AdapterName()] {
		if pp, ok := sess.(mux.ProcessProvider); ok {
			if info, err := pp.ProcessInfo(ctx); err == nil {
				return info.PID, info.StartedAt
			}
		}
	}
	return 0, time.Time{}
}

// callAcceptedAdapter passes the full prefix and opaque cursor to the accepted
// version-specific T1/T2 adapter. Returns events, stream version (for
// LaunchCorrelation), opaque NextCursor, and whether the adapter reported
// degraded (version conflict, cursor failure, malformed authority).
//
// The adapter receives the FULL prefix from position 0 — the caller must not
// slice or rebase the input. The cursor is passed through unchanged.
func callAcceptedAdapter(kind string, rawLines [][]byte, sessionID string, prevCursor string) ([]agent.AgentEvent, string, string, bool) {
	adapter := acceptedAdapterFor(kind)
	if adapter == nil {
		return nil, "", prevCursor, false
	}

	ctx := context.Background()
	records := make([]contract.RawRecord, 0, len(rawLines))
	for _, line := range rawLines {
		if len(line) == 0 {
			continue
		}
		records = append(records, contract.RawRecord{
			Bytes:      line,
			Source:     agent.SourceJSONL,
			Provenance: contract.ProvenanceNativeLog,
		})
	}
	if len(records) == 0 {
		return nil, "", prevCursor, false
	}

	result, err := adapter.ReadEvents(ctx, contract.ReadInput{
		Session:   contract.SessionContext{SessionID: sessionID},
		Records:   records,
		Cursor:    contract.Cursor(prevCursor),
		MaxEvents: 500,
	})
	nextCursor := prevCursor
	degraded := result.Degraded.Degraded
	if err == nil && string(result.NextCursor) != "" {
		nextCursor = string(result.NextCursor)
	}
	if err != nil {
		return nil, "", nextCursor, true // adapter error → degraded
	}
	if len(result.Events) == 0 && degraded {
		return nil, "", nextCursor, true
	}
	if len(result.Events) == 0 {
		return nil, "", nextCursor, false
	}

	discoveredVersion := extractStreamVersion(kind, records)

	return result.Events, discoveredVersion, string(result.NextCursor), degraded
}

// extractStreamVersion reads the version from the first structural record field.
// This is NOT validation — the adapter already validated the version internally.
// We only need the version string for LaunchCorrelation comparison.
func extractStreamVersion(kind string, records []contract.RawRecord) string {
	if len(records) == 0 {
		return ""
	}
	if len(records[0].Bytes) == 0 {
		return ""
	}
	// Minimal extraction from the first record — no structural validation.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(records[0].Bytes, &m); err != nil {
		return ""
	}
	switch kind {
	case "codex":
		// Codex: payload.cli_version
		payload, _ := m["payload"]
		if payload != nil {
			var p map[string]json.RawMessage
			if err := json.Unmarshal(payload, &p); err == nil {
				if cv, ok := p["cli_version"]; ok {
					var v string
					if err := json.Unmarshal(cv, &v); err == nil && v != "" {
						return v
					}
				}
			}
		}
	case "claude":
		// Claude: top-level version
		if ver, ok := m["version"]; ok {
			var v string
			if err := json.Unmarshal(ver, &v); err == nil && v != "" {
				return v
			}
		}
	}
	return ""
}

// now returns the current time. Extracted as a function so tests can override.
var now = time.Now

func mapLegacyType(t string) agent.AgentEventType {
	switch t {
	case "user", "user_message":
		return agent.EventUserMessage
	case "assistant", "assistant_message", "message":
		return agent.EventAssistantMessage
	case "thinking":
		return agent.EventThinking
	case "tool_use", "tool_call_started":
		return agent.EventToolCallStarted
	case "tool_result", "tool_call_finished":
		return agent.EventToolCallFinished
	case "approval_requested":
		return agent.EventApprovalRequested
	case "approval_resolved":
		return agent.EventApprovalResolved
	case "error", "failed":
		return agent.EventFailed
	case "done", "completed":
		return agent.EventCompleted
	case "interrupted":
		return agent.EventInterrupted
	default:
		return agent.EventUnknown
	}
}
