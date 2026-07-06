package term

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
)

// SessionTelemetry holds the calculated state of a tmux session.
type SessionTelemetry struct {
	ID            string              `json:"id"`
	State         string              `json:"state"` // "idle", "thinking", "working", "waiting"
	Load          int                 `json:"load"`  // 0-100 (animation speed)
	Runner        string              `json:"runner"`
	RunnerColor   string              `json:"runnerColor"`
	Adapter       string              `json:"adapter"`
	Events        []models.AgentEvent `json:"events"`
	Stale         bool                `json:"stale,omitempty"`
	LastSuccessAt time.Time           `json:"lastSuccessAt,omitempty"`
	LastError     string              `json:"lastError,omitempty"`
}

type sessionStateData struct {
	LastOutput   []byte
	LastActivity time.Time
	State        string
	Load         int
	Runner       string
	RunnerColor  string
	Cursor       *LogCursor
	Parser       AgentLogParser
}

var (
	telemetryCache = make(map[string]*sessionStateData)
	telemetryMu    sync.Mutex
)

// ClearTelemetryCache completely resets the state and cursor for a given session.
func ClearTelemetryCache(sessionID string) {
	telemetryMu.Lock()
	defer telemetryMu.Unlock()
	delete(telemetryCache, sessionID)
}

// StartTelemetryLoop runs in the background and periodically takes snapshots of
// all active tmux sessions to calculate their speed and state.

func isApprovalPrompt(line string) bool {
	return strings.Contains(line, "Do you want") ||
		strings.Contains(line, "proceed?") ||
		strings.Contains(line, "(y/n)") ||
		strings.Contains(line, "(y/N)") ||
		(strings.Contains(line, "1. Yes") && strings.Contains(line, "No"))
}

// collectProcessSnapshots performs at most one process discovery command per
// batch-capable adapter in a telemetry cycle. A missing surface in a successful
// batch is not retried through ProcessInfo because that fallback would execute
// the same expensive adapter-wide command once per session.
func collectProcessSnapshots(ctx context.Context, adapters []mux.Adapter) (map[string]models.ProcessInfo, map[string]bool, map[string]bool) {
	snapshots := make(map[string]models.ProcessInfo)
	batchAdapters := make(map[string]bool)
	failedAdapters := make(map[string]bool)

	for _, adapter := range adapters {
		name := adapter.Name()
		provider, ok := adapter.(mux.ProcessSnapshotProvider)
		if !ok {
			continue
		}

		batchAdapters[name] = true
		snapshot, err := provider.ProcessSnapshot(ctx)
		if err != nil {
			failedAdapters[name] = true
			continue
		}
		for sessionID, info := range snapshot {
			snapshots[name+":"+sessionID] = info
		}
	}

	return snapshots, batchAdapters, failedAdapters
}

func StartTelemetryLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			// 1. Gather sessions and build process snapshots
			sessions := Sessions(context.Background())
			processSnapshots, batchAdapters, failedAdapters := collectProcessSnapshots(ctx, R.Registry.Adapters())

			telemetryMu.Lock()
			// Clean up old sessions
			activeSet := make(map[string]bool)
			for _, sess := range sessions {
				s := sess.AdapterName() + ":" + sess.ID()
				activeSet[s] = true
				if telemetryCache[s] == nil {
					telemetryCache[s] = &sessionStateData{
						LastActivity: time.Now(),
						State:        "idle",
						Load:         0,
					}
				}
			}
			for s := range telemetryCache {
				if !activeSet[s] {
					delete(telemetryCache, s)
				}
			}
			telemetryMu.Unlock()

			// Check each active session
			for _, sess := range sessions {
				s := sess.AdapterName() + ":" + sess.ID()
				var logRef LogRef
				var logErr error = fmt.Errorf("no log")

				// 1. LinkedLogResolver (Explicit Links)
				if link, ok := GetLink(s); ok && !link.Stale {
					if link.Provider == "gemini-antigravity" {
						res := &AntigravityResolver{}
						logRef, logErr = res.ResolveLink(ctx, link.ExternalSessionID)
					}
				}

				// 2. ProcessProvider (System Process - Batch or Fallback)
				if logErr != nil {
					if info, ok := processSnapshots[s]; ok {
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
					telemetryMu.Lock()
					stateData := telemetryCache[s]
					if stateData != nil {
						if stateData.Cursor == nil || stateData.Cursor.Path != logRef.Path {
							stateData.Cursor = &LogCursor{Path: logRef.Path, Offset: 0, Inode: 0}
							if logRef.Agent == "claude" {
								stateData.Parser = &ClaudeParser{Session: s}
							} else if logRef.Agent == "codex" {
								stateData.Parser = &CodexParser{Session: s}
							} else if logRef.Agent == "gemini" {
								stateData.Parser = &GeminiParser{Session: s}
							}
						}

						cursor := stateData.Cursor
						parser := stateData.Parser
						telemetryMu.Unlock()

						if parser != nil {
							newEvents, readErr := ReadNewEvents(cursor, parser, 500)
							if readErr == nil && len(newEvents) > 0 {
								models.AppendEvents(s, newEvents)
								parsedNewEvents = true
								lastEvent = newEvents[len(newEvents)-1]
							}
						}
					} else {
						telemetryMu.Unlock()
					}
				}

				// Always fetch capture-pane as fallback for approvals and missing logs
				var out []byte
				if !failedAdapters[sess.AdapterName()] {
					if sr, ok := sess.(mux.ScreenReader); ok {
						out, _ = sr.ReadScreen(ctx)
					}
				}

				// POKIT_RUNNER is no longer extracted here directly from tmux,
				// as it's typically set globally or via the Linker/ProcessResolver.
				// We'll default to the agent type we found, or the logRef.Agent.
				runner := "agent"
				if logRef.Agent != "" {
					runner = logRef.Agent
				}
				runnerColor := "#58a6ff"

				telemetryMu.Lock()
				stateData := telemetryCache[s]
				if stateData == nil {
					telemetryMu.Unlock()
					continue
				}

				stateData.Runner = runner
				stateData.RunnerColor = runnerColor

				diffSize := len(out) - len(stateData.LastOutput)
				if diffSize < 0 {
					diffSize = -diffSize
				}

				// Extract the last 5 lines for keyword analysis
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

				stateData.LastOutput = out
				telemetryMu.Unlock()
			}
		}
	}()
}

// evaluateState contains the core telemetry state machine logic for unit testing
func evaluateState(stateData *sessionStateData, parsedNewEvents bool, lastEvent models.AgentEvent, logErr error, isWaiting bool, isThinkingFallback bool, diffSize int) {
	if isWaiting {
		stateData.State = "waiting"
		stateData.Load = 0
	} else if parsedNewEvents {
		stateData.LastActivity = time.Now()
		if lastEvent.Type == "user" {
			stateData.State = "thinking"
			stateData.Load = 50
		} else if lastEvent.Type == "tool_use" {
			stateData.State = "working"
			stateData.Load = 100
		} else if lastEvent.Type == "tool_result" {
			stateData.State = "thinking"
			stateData.Load = 50
		} else if lastEvent.Type == "message" {
			if strings.Contains(lastEvent.Summary, "Thinking") || strings.Contains(lastEvent.Summary, "Reasoning") {
				stateData.State = "thinking"
				stateData.Load = 50
			} else {
				stateData.State = "idle"
				stateData.Load = 0
			}
		} else {
			stateData.State = "working"
			stateData.Load = 50
		}
	} else if logErr == nil {
		// JSONL log found but NO new events parsed this tick. Apply idle timeout to prevent permanent lock.
		if time.Since(stateData.LastActivity) > 10*time.Second {
			if stateData.State == "working" {
				stateData.State = "thinking"
				stateData.Load = 50
				stateData.LastActivity = time.Now() // reset so it waits before dropping to idle
			} else if stateData.State == "thinking" || stateData.State == "waiting" {
				stateData.State = "idle"
				stateData.Load = 0
			}
		} else if stateData.State == "waiting" && !isWaiting {
			// Prompt disappeared but no events parsed yet
			stateData.State = "idle"
			stateData.Load = 0
		}
	} else {
		// Fallback to capture-pane heuristics only if we failed to find an agent log
		if diffSize > 50 {
			stateData.State = "working"
			stateData.Load = 100
			stateData.LastActivity = time.Now()
		} else if diffSize > 0 || isThinkingFallback {
			stateData.State = "thinking"
			stateData.Load = 50
			stateData.LastActivity = time.Now()
		} else {
			if time.Since(stateData.LastActivity) > 5*time.Second {
				stateData.State = "idle"
				stateData.Load = 0
			} else {
				stateData.State = "thinking"
				stateData.Load = 20
			}
		}
	}
}

// HandleSessionsV2 replaces the old HandleSessions API and returns rich JSON metadata
func HandleSessionsV2(w http.ResponseWriter, r *http.Request) {
	if historyID := r.URL.Query().Get("history"); historyID != "" {
		events := models.GetEvents(historyID) // historyID is the canonical ID passed from the frontend
		w.Header().Set("Content-Type", "application/json")
		if len(events) > 0 {
			json.NewEncoder(w).Encode(events)
			return
		}

		// Fallback to adapter's capability for history reading
		var out []byte
		var err error
		sess, err := FindSession(MigrateLegacyID(historyID))
		if err == nil {
			if hr, ok := sess.(mux.HistoryReader); ok {
				out, err = hr.ReadHistory(context.Background(), 10000)
			} else if sr, ok := sess.(mux.ScreenReader); ok {
				out, err = sr.ReadScreen(context.Background())
			} else {
				err = fmt.Errorf("session does not support history or screen reading")
			}
		}

		if err != nil || len(out) == 0 {
			http.Error(w, "session not found or history unavailable", http.StatusNotFound)
			return
		}

		fallbackEvents := []models.AgentEvent{{
			ID: "fallback-0", Session: historyID, Type: "message", Summary: "Terminal History", Detail: string(out), Timestamp: time.Now().Format(time.RFC3339),
		}}
		json.NewEncoder(w).Encode(fallbackEvents)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	res := make([]SessionTelemetry, 0)

	sessions := Sessions(context.Background())

	// Collect telemetry data under lock
	telemetryMu.Lock()
	stateCopies := make(map[string]*sessionStateData)
	for id, data := range telemetryCache {
		// Shallow copy is enough for State, Load, Runner, RunnerColor
		copyData := *data
		stateCopies[id] = &copyData
	}
	telemetryMu.Unlock()

	for _, s := range sessions {
		compoundID := s.AdapterName() + ":" + s.ID()

		snap, _ := R.Registry.Snapshot(s.AdapterName())
		var errStr string
		if snap.LastError != nil {
			errStr = snap.LastError.Error()
		}
		isStale := snap.LastError != nil

		// Fetch events outside the lock
		events := models.GetEvents(compoundID)

		data := stateCopies[compoundID]
		if data == nil {
			res = append(res, SessionTelemetry{
				ID:            compoundID,
				State:         "idle",
				Load:          0,
				Runner:        "cat",
				RunnerColor:   "#58a6ff",
				Adapter:       s.AdapterName(),
				Events:        events,
				Stale:         isStale,
				LastSuccessAt: snap.LastSuccessAt,
				LastError:     errStr,
			})
		} else {
			res = append(res, SessionTelemetry{
				ID:            compoundID,
				State:         data.State,
				Load:          data.Load,
				Runner:        data.Runner,
				RunnerColor:   data.RunnerColor,
				Adapter:       s.AdapterName(),
				Events:        events,
				Stale:         isStale,
				LastSuccessAt: snap.LastSuccessAt,
				LastError:     errStr,
			})
		}
	}

	json.NewEncoder(w).Encode(res)
}
