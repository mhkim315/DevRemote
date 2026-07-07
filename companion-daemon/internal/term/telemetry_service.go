package term

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
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
	logResolver func(models.ProcessInfo) (LogRef, error) // Phase A5b: injectable resolver (nil = production ResolveAgentLog)
	interval    time.Duration

	mu       sync.Mutex
	sessions map[string]*sessionStateData
	done     chan struct{}
}

// NewTelemetryService creates a TelemetryService. Call Run() to start sampling.
func NewTelemetryService(reg *mux.Registry, events EventStore, links LinkStore, notifier Notifier, detector AgentDetector, approvals ApprovalStore) *TelemetryService {
	if notifier == nil {
		notifier = NoopNotifier{}
	}
	if approvals == nil {
		approvals = NewApprovalStore()
	}
	return &TelemetryService{
		reg:       reg,
		events:    events,
		links:     links,
		notifier:  notifier,
		detector:  detector,
		approvals: approvals,
		interval:  2 * time.Second,
		sessions:  make(map[string]*sessionStateData),
		done:      make(chan struct{}),
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
				newEvents, readErr := ReadNewEvents(cursor, parser, 500)
				if readErr == nil && len(newEvents) > 0 {
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
								ID:        fmt.Sprintf("%s-%s", id, e.ID),
								SessionID: id,
								AgentKind: logRef.Agent,
								Kind:      "approval",
								Status:    "pending",
								Prompt:    firstNonEmpty(e.Detail, e.Summary, "Interaction requested"),
								Options:   options,
								Default:   "reject",
								Source:    "jsonl",
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
				Capabilities: sessionCapabilities(sess),
				Events:       events, Stale: isStale, LastSuccessAt: snap.LastSuccessAt, LastError: errStr,
			})
		} else {
			res = append(res, SessionTelemetry{
				ID: compoundID, DisplayID: sess.ID(), State: data.State, Load: data.Load,
				Runner: data.Runner, RunnerColor: data.RunnerColor, Adapter: sess.AdapterName(),
				Capabilities: sessionCapabilities(sess),
				Events:       events, Stale: isStale, LastSuccessAt: snap.LastSuccessAt, LastError: errStr,
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

// Clear removes all cached telemetry state for a session.
func (s *TelemetryService) Clear(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
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
				Input: &agent.InputSchema{Required: true, Placeholder: "Enter text to send"}},
			agent.InteractionOption{ID: "send_key", Label: "Send Key", Kind: "neutral",
				Input: &agent.InputSchema{Required: true, Placeholder: "Key sequence"}},
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
