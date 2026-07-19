package term

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent"
	claude "devremote/companion-daemon/internal/agent/adapters/claude/v2_1_202"
	codex "devremote/companion-daemon/internal/agent/adapters/codex/v0_144_1"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/sessionid"
	"devremote/companion-daemon/internal/transcript"
)

// TelemetryService owns the telemetry state machine and background sampling loop.
type TelemetryService struct {
	reg        *mux.Registry
	events     EventStore
	notifier   Notifier
	detector   AgentDetector                            // Phase A5: optional agent detector (nil if not wired)
	approvals  *AuthoritativeApprovalStore              // A1: generation-bound approval store
	activity   *ActivityBuffer                          // E8f2: activity capture
	transcript *transcript.Service                      // T3: AgentEvent → Transcript projection
	interval   time.Duration
	// statusStore is the S1 session-owned agent-activity store.
	statusStore *AgentStatusStore

	// A1 R3-C: the per-session runtime delivery gate.
	deliveryGate *RuntimeDeliveryGate

	mu            sync.Mutex
	adapterStates map[string]*adapterState                     // PA3: retained accepted-adapter ingestion state
	logResolver   func(models.ProcessInfo) (LogRef, error)     // test seam (nil = production ResolveAgentLog)
	done          chan struct{}
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
		reg:           reg,
		events:        events,
		notifier:      notifier,
		detector:      detector,
		approvals:     approvals,
		activity:      activity,
		transcript:    transcriptSvc,
		interval:      2 * time.Second,
		statusStore:   NewAgentStatusStore(),
		adapterStates: make(map[string]*adapterState),
		done:          make(chan struct{}),
	}
}

// Run starts the background telemetry sampling loop. Blocks until ctx is cancelled.
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

// reconcileSessions prunes adapter state for sessions that have left the Registry.
func (s *TelemetryService) reconcileSessions(sessions []mux.Session) {
	activeSet := make(map[string]bool, len(sessions))
	for _, sess := range sessions {
		id := sess.AdapterName() + ":" + sess.ID()
		activeSet[id] = true
	}

	s.mu.Lock()
	var pruned []string
	for id := range s.adapterStates {
		if !activeSet[id] {
			delete(s.adapterStates, id)
			pruned = append(pruned, id)
		}
	}
	s.mu.Unlock()

	for _, id := range pruned {
		if s.statusStore != nil {
			s.statusStore.Clear(id)
		}
		if s.approvals != nil {
			s.approvals.Clear(id)
		}
		if s.deliveryGate != nil {
			s.deliveryGate.Deactivate(id)
		}
	}
}

// processSession runs the accepted-adapter ingestion path for a session.
// PA3 Step 2: legacy parser, screen read, state machine removed.
func (s *TelemetryService) processSession(ctx context.Context, sess mux.Session, processSnapshots map[string]models.ProcessInfo, batchAdapters, failedAdapters map[string]bool) {
	id := sess.AdapterName() + ":" + sess.ID()

	// E8f2: ensure recorder exists for session.
	if opener, ok := sess.(mux.StreamOpener); ok {
		EnsureRecorder(id, func() (ptyStream, error) { return opener.OpenStream(ctx) }, s.activity)
	}

	// Resolve log for accepted-adapter ingestion.
	var logRef LogRef
	var logErr error = fmt.Errorf("no log")

	if info, ok := processSnapshots[id]; ok {
		logRef, logErr = s.resolveLog(ctx, info)
	} else if !batchAdapters[sess.AdapterName()] {
		if pp, ok := sess.(mux.ProcessProvider); ok {
			if pinfo, err := pp.ProcessInfo(ctx); err == nil {
				logRef, logErr = s.resolveLog(ctx, pinfo)
			}
		}
	}

	if logErr != nil {
		return // no log to ingest
	}

	s.mu.Lock()
	state := s.adapterStates[id]
	if state == nil {
		state = newAdapterState()
		s.adapterStates[id] = state
	}
	s.mu.Unlock()

	rawLines := readRawLinesForPath(state, logRef)
	if len(rawLines) == 0 {
		return
	}

	if s.transcript != nil && isAcceptedAdapter(logRef.Agent) {
		state.appendRecords(rawLines)
		records, acursor, overflowed := state.buildAdapterInput()
		if overflowed {
			if state.shouldEmitOverflowMarker() {
				s.transcript.EmitDegraded(id, "semantic ingestion overflowed", now())
			}
			if s.statusStore != nil {
				s.statusStore.Revoke(id, 0, state.streamGen, state.version, "semantic ingestion overflowed")
			}
		} else {
			acceptedEvents, adapterVersion, nextCursor, degraded := callAcceptedAdapter(logRef.Agent, records, id, acursor)
			state.setCursor(nextCursor)
			if adapterVersion != "" {
				state.updateVersion(adapterVersion, logRef.Agent)
			}
			if degraded && !state.versionValid {
				state.markVersionConflict()
			}
			pid, start := getProcessIdentity(ctx, sess, id, processSnapshots, batchAdapters)
			launchBinding := transcript.LookupLaunch(id)
			launchGen := int64(0)
			if launchBinding != nil {
				launchGen = launchBinding.Generation
			}
			corr := state.launchCorrelation(launchBinding, sess.AdapterName(), logRef.Agent, pid, start)
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
			// S1-C: agent-activity store update.
			if s.statusStore != nil {
				switch {
				case state.versionConflict:
					s.statusStore.Revoke(id, launchGen, state.streamGen, state.version, "accepted version conflict")
				case corr == contract.CorrelationManagedLaunch:
					if len(acceptedEvents) > 0 {
						s.statusStore.Update(AgentStatusUpdate{
							SessionID: id, LaunchGen: launchGen, Generation: state.streamGen, Version: state.version,
							Events: acceptedEvents, Adapter: acceptedAdapterFor(logRef.Agent),
						})
					}
				default:
					s.statusStore.RevokeIfPresent(id, launchGen, state.streamGen, state.version, "correlation unavailable")
				}
			}
			// A1-C: approval ingestion.
			if s.approvals != nil {
				switch {
				case state.versionConflict:
					s.approvals.InvalidateSession(id, "accepted version conflict")
				case corr == contract.CorrelationManagedLaunch:
					s.ingestApprovals(id, launchGen, state.streamGen, logRef.Agent, state.version, acceptedEvents)
				default:
					s.approvals.InvalidateSession(id, "correlation unavailable")
				}
			}
			// A1 R3-C: delivery gate sync.
			if s.deliveryGate != nil {
				if !state.versionConflict && corr == contract.CorrelationManagedLaunch {
					s.deliveryGate.Activate(id, RuntimeRef{Adapter: logRef.Agent, Version: state.version, LaunchGen: launchGen, StreamGen: state.streamGen}, 0)
				} else {
					s.deliveryGate.Deactivate(id)
				}
			}
		}
	}
}

// logResolver is an injectable test seam. nil means use production ResolveAgentLog.
// SetLogResolver overrides it for testing.
func (s *TelemetryService) SetLogResolver(fn func(models.ProcessInfo) (LogRef, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logResolver = fn
}

// resolveLog resolves an agent log reference from process info.
func (s *TelemetryService) resolveLog(ctx context.Context, p models.ProcessInfo) (LogRef, error) {
	s.mu.Lock()
	fn := s.logResolver
	s.mu.Unlock()
	if fn != nil {
		return fn(p)
	}
	return ResolveAgentLog(ctx, p)
}



// Done returns a channel that closes when the sampling loop has exited.
func (s *TelemetryService) Done() <-chan struct{} { return s.done }

// Snapshot returns a copy of the current telemetry state for all sessions.
func (s *TelemetryService) Snapshot(reg *mux.Registry) []SessionTelemetry {
	sessions := reg.Sessions(context.Background())

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

		res = append(res, SessionTelemetry{
			ID: compoundID, DisplayID: sess.ID(), State: "idle", Load: 0,
			Runner: "agent", RunnerColor: "#58a6ff", Adapter: sess.AdapterName(),
			Capabilities:        sessionCapabilities(sess),
			AdapterCapabilities: adapterCapabilityStrings(reg, sess.AdapterName()),
			Events:              events, Stale: isStale, LastSuccessAt: snap.LastSuccessAt, LastError: errStr,
		})
	}
	sortTelemetry(res)

	// Phase A5: populate agent fields from detector.
	if s.detector != nil {
		for i := range res {
			st := &res[i]
			ref := sessionid.ParseSessionID(st.ID)
			evidence := ProdDetectionEvidence{TermAdapter: ref.Adapter}
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
		}
	}

	// Phase A9: populate approvals from ApprovalStore.
	for i := range res {
		st := &res[i]
		st.Approvals = s.approvals.ListSafe(st.ID)
	}

	// S1-D: advisory agent-activity projection.
	if s.statusStore != nil {
		for i := range res {
			st := &res[i]
			rec, stale, ok := s.statusStore.Current(st.ID)
			if !ok {
				continue
			}
			st.AgentActivity = &AgentActivityDTO{
				ContractVersion: contract.ContractVersion,
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

// acceptedAdapterFor returns a fresh accepted version-specific adapter instance.
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

// SetDeliveryGate wires the production runtime delivery gate.
func (s *TelemetryService) SetDeliveryGate(g *RuntimeDeliveryGate) { s.deliveryGate = g }

// Clear removes all cached state for a session.
func (s *TelemetryService) Clear(sessionID string) {
	s.mu.Lock()
	delete(s.adapterStates, sessionID)
	s.mu.Unlock()
	if s.statusStore != nil {
		s.statusStore.Clear(sessionID)
	}
	if s.approvals != nil {
		s.approvals.Clear(sessionID)
	}
	if s.deliveryGate != nil {
		s.deliveryGate.Deactivate(sessionID)
	}
}

// RegisterOrReplaceLaunch is the single production-owned atomic launch replacement boundary.
func (s *TelemetryService) RegisterOrReplaceLaunch(spec transcript.LaunchSpec) (int64, bool) {
	gen, replaced, _ := transcript.RegisterOrReplaceLaunch(spec, func(gen int64) {
		s.invalidateForLaunch(spec.SessionID, gen)
	})
	return gen, replaced
}

// invalidateForLaunch installs a non-current high-water at the new launch generation.
func (s *TelemetryService) invalidateForLaunch(sessionID string, launchGen int64) {
	if s.statusStore == nil {
		return
	}
	s.mu.Lock()
	if st := s.adapterStates[sessionID]; st != nil {
		st.clear()
	}
	s.mu.Unlock()
	s.statusStore.Invalidate(sessionID, launchGen, 0, "launch binding replaced")
	if s.approvals != nil {
		s.approvals.SupersedeRuntime(sessionID, launchGen, 0, "launch binding replaced")
	}
	if s.deliveryGate != nil {
		s.deliveryGate.Deactivate(sessionID)
	}
}

// isAcceptedAdapter returns true only for agent kinds that have an accepted
// T0 contract.AgentAdapter in production.
func isAcceptedAdapter(kind string) bool {
	switch kind {
	case "codex", "claude":
		return true
	default:
		return false
	}
}

// isAcceptedVersion checks that the detected provider version matches
// the accepted adapter version. Used by adapter_state.go.
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

// readRawLinesForPath reads raw JSONL lines for the accepted adapter path.
func readRawLinesForPath(state *adapterState, logRef LogRef) [][]byte {
	if state == nil || logRef.Path == "" {
		return nil
	}
	if state.path != logRef.Path {
		state.resetForGeneration(logRef.Path)
	}
	cursor := &LogCursor{Path: logRef.Path, Offset: 0, Inode: 0}
	if c := state.cursor(); c != "" {
		cursor = &LogCursor{Path: logRef.Path, Offset: 0, Inode: 0}
		_ = c
	}
	result, err := ReadRawLines(cursor, 500)
	if err != nil {
		return nil
	}
	return result.Lines
}

// getProcessIdentity extracts the PID and process start time for a session.
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

// callAcceptedAdapter passes records to the accepted version-specific adapter.
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
		return nil, "", nextCursor, true
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
func extractStreamVersion(kind string, records []contract.RawRecord) string {
	if len(records) == 0 || len(records[0].Bytes) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(records[0].Bytes, &m); err != nil {
		return ""
	}
	switch kind {
	case "codex":
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
		if ver, ok := m["version"]; ok {
			var v string
			if err := json.Unmarshal(ver, &v); err == nil && v != "" {
				return v
			}
		}
	}
	return ""
}

var now = time.Now
