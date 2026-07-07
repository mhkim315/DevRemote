package term

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
)

// TelemetryService owns the telemetry state machine and background sampling loop.
// It replaces the telemetryCache package global.
type TelemetryService struct {
	reg      *mux.Registry
	events   EventStore
	links    LinkStore
	notifier Notifier
	detector AgentDetector // Phase A5: optional agent detector (nil if not wired)
	interval time.Duration

	mu       sync.Mutex
	sessions map[string]*sessionStateData
	done     chan struct{}
}

// NewTelemetryService creates a TelemetryService. Call Run() to start sampling.
func NewTelemetryService(reg *mux.Registry, events EventStore, links LinkStore, notifier Notifier, detector AgentDetector) *TelemetryService {
	if notifier == nil {
		notifier = NoopNotifier{}
	}
	return &TelemetryService{
		reg:      reg,
		events:   events,
		links:    links,
		notifier: notifier,
		detector: detector,
		interval: 2 * time.Second,
		sessions: make(map[string]*sessionStateData),
		done:     make(chan struct{}),
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
			logRef, logErr = ResolveAgentLog(ctx, info)
		} else if !batchAdapters[sess.AdapterName()] {
			if pp, ok := sess.(mux.ProcessProvider); ok {
				if pinfo, err := pp.ProcessInfo(ctx); err == nil {
					logRef, logErr = ResolveAgentLog(ctx, pinfo)
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
			kind, status, confidence := s.detector.DetectAgent(st.ID, ref.Adapter, ref.LocalID, evidence)
			st.AgentKind = kind
			st.AgentStatus = status
			st.AgentConfidence = confidence
			// Parse agent events from logs.
			if events := s.detector.ParseEvents(st.ID, evidence); len(events) > 0 {
				st.AgentEvents = events
			}
		}
	}
	return res
}

// Clear removes all cached telemetry state for a session.
func (s *TelemetryService) Clear(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}
