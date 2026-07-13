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
	links       LinkStore
	notifier    Notifier
	detector    AgentDetector                            // Phase A5: optional agent detector (nil if not wired)
	approvals   ApprovalStore                            // Phase A9: approval tracking
	activity    *ActivityBuffer                          // E8f2: activity capture
	transcript  *transcript.Service                      // T3: AgentEvent → Transcript projection
	logResolver func(models.ProcessInfo) (LogRef, error) // Phase A5b: injectable resolver (nil = production ResolveAgentLog)
	interval    time.Duration
	// statusStore is the S1 session-owned agent-activity store. It is fed from the
	// SAME accepted-adapter read path as Transcript (no second reader) and holds
	// the advisory activity result separately from lifecycle/health.
	statusStore *AgentStatusStore

	mu       sync.Mutex
	sessions map[string]*sessionStateData
	done     chan struct{}
}

// NewTelemetryService creates a TelemetryService. Call Run() to start sampling.
func NewTelemetryService(reg *mux.Registry, events EventStore, links LinkStore, notifier Notifier, detector AgentDetector, approvals ApprovalStore, activity *ActivityBuffer, transcriptSvc *transcript.Service) *TelemetryService {
	if notifier == nil {
		notifier = NoopNotifier{}
	}
	if approvals == nil {
		approvals = NewApprovalStore()
	}
	return &TelemetryService{
		reg:         reg,
		events:      events,
		links:       links,
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

		s.mu.Lock()
		activeSet := make(map[string]bool)
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
		for id := range s.sessions {
			if !activeSet[id] {
				delete(s.sessions, id)
				// S1-C: a session that left the registry loses its cached activity
				// record too (bounded state; no inheritance on later reuse).
				if s.statusStore != nil {
					s.statusStore.Clear(id)
				}
			}
		}
		s.mu.Unlock()

		for _, sess := range sessions {
			s.processSession(ctx, sess, processSnapshots, batchAdapters, failedAdapters)
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

	// 1. LinkedLogResolver
	if s.links != nil {
		if link, ok := GetLink(id, s.reg, s.links); ok && !link.Stale {
			if link.Provider == "gemini-antigravity" {
				res := &AntigravityResolver{}
				logRef, logErr = res.ResolveLink(ctx, link.ExternalSessionID)
			}
		}
	}

	// 2. ProcessProvider
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
					if rr.GenerationChanged || a.path != logRef.Path {
						a.resetForGeneration(logRef.Path)
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
							s.statusStore.Revoke(id, a.streamGen, a.version, "semantic ingestion overflowed")
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
						binding := transcript.LookupLaunch(id)
						corr := a.launchCorrelation(binding, logRef.Agent, getProcessPID(id, processSnapshots))
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
						// from correlated new events; a version conflict revokes authority
						// to unknown+degraded. Polls with no status evidence leave the
						// prior record untouched (Update is a no-op on empty evidence).
						if s.statusStore != nil {
							switch {
							case a.versionConflict:
								s.statusStore.Revoke(id, a.streamGen, a.version, "accepted version conflict")
							case corr == contract.CorrelationManagedLaunch && len(acceptedEvents) > 0:
								s.statusStore.Update(AgentStatusUpdate{
									SessionID: id, Generation: a.streamGen, Version: a.version,
									Events: acceptedEvents, Adapter: acceptedAdapterFor(logRef.Agent),
								})
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

					// Phase A9: detect approval events and track in ApprovalStore.
					// Phase A9: capability-aware approval options.
					var newApprovals []agent.AgentApproval
					for _, e := range newEvents {
						if e.Type == "approval_requested" {
							options := buildInteractionOptions(sess)

							newApprovals = append(newApprovals, agent.AgentApproval{
								ID:         fmt.Sprintf("%s-%s", id, e.ID),
								SessionID:  id,
								AgentKind:  logRef.Agent,
								Kind:       "approval",
								Status:     "pending",
								Prompt:     firstNonEmpty(e.Detail, e.Summary, "Interaction requested"),
								Options:    options,
								Default:    "reject",
								Source:     "jsonl",
								Confidence: 0.9,
							})
						}
					}
					if len(newApprovals) > 0 {
						s.approvals.Upsert(id, newApprovals)
					}
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
		st.Approvals = s.approvals.List(st.ID)
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
}

// buildInteractionOptions returns capability-aware interaction options for a session.
// Each option carries a semantic Kind for mobile styling; mobile never infers meaning from ID.
func buildInteractionOptions(sess mux.Session) []agent.InteractionOption {
	_, hasInput := sess.(mux.InputWriter)
	_, hasStream := sess.(mux.StreamOpener)

	var options []agent.InteractionOption
	if hasStream {
		options = append(options, agent.InteractionOption{ID: "open_terminal", Label: "Open Terminal", Kind: "open"})
	}
	if hasInput {
		options = append(options,
			agent.InteractionOption{ID: "approve", Label: "Approve", Kind: "approve"},
			agent.InteractionOption{ID: "reject", Label: "Reject", Kind: "reject"},
			agent.InteractionOption{ID: "send_text", Label: "Send Text", Kind: "neutral",
				Input: &agent.InputSchema{Required: true, Placeholder: "Enter text to send", Placement: "as_payload"}},
			agent.InteractionOption{ID: "send_key", Label: "Send Key", Kind: "neutral",
				Input: &agent.InputSchema{Required: true, Placeholder: "Key sequence", Placement: "as_payload"}},
		)
	}
	// Observe-only: return empty options (no view_only fake action).
	// Mobile renders "No remote actions available" for empty options.
	return options
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

// getProcessPID extracts the PID for a session from process snapshots.
func getProcessPID(id string, snapshots map[string]models.ProcessInfo) int {
	if info, ok := snapshots[id]; ok {
		return info.PID
	}
	return 0
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
