package term

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
)

// SessionTelemetry holds the calculated state of a tmux session.
type SessionTelemetry struct {
	ID          string       `json:"id"`
	State       string       `json:"state"` // "idle", "thinking", "working", "waiting"
	Load        int          `json:"load"`  // 0-100 (animation speed)
	Runner      string       `json:"runner"`
	RunnerColor string       `json:"runnerColor"`
	Adapter     string       `json:"adapter"`
	Events      []models.AgentEvent `json:"events"`
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

// StartTelemetryLoop runs in the background and periodically takes snapshots of
// all active tmux sessions to calculate their speed and state.

func isApprovalPrompt(line string) bool {
	return strings.Contains(line, "Do you want") ||
		strings.Contains(line, "proceed?") ||
		strings.Contains(line, "(y/n)") ||
		strings.Contains(line, "(y/N)") ||
		(strings.Contains(line, "1. Yes") && strings.Contains(line, "No"))
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
			var sessions []string
			for _, s := range mux.GetAllSessionsCached() {
				// Use the compound key so cache works for multi-adapters
				sessions = append(sessions, s.AdapterName()+":"+s.ID())
			}

			telemetryMu.Lock()
			// Clean up old sessions
			activeSet := make(map[string]bool)
			for _, s := range sessions {
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
			for _, s := range sessions {
				parts := strings.SplitN(s, ":", 2)
				if len(parts) != 2 || parts[0] != "tmux" {
					// We only know how to capture-pane for tmux right now
					continue
				}
				rawID := parts[1]

				cmdCwd := exec.Command("tmux", "display-message", "-p", "-t", rawID, "#{pane_current_path}")
				outCwd, _ := cmdCwd.Output()
				paneCwd := strings.TrimSpace(string(outCwd))

				logRef, logErr := FindAgentLogRef(ctx, rawID, paneCwd)
				
				var parsedNewEvents bool
				var lastEvent models.AgentEvent
				
				if logErr == nil {
					telemetryMu.Lock()
					stateData := telemetryCache[s]
					if stateData != nil {
						if stateData.Cursor == nil || stateData.Cursor.Path != logRef.Path {
							stateData.Cursor = &LogCursor{Path: logRef.Path, Offset: 0, Inode: 0}
							if logRef.Agent == "claude" {
								stateData.Parser = &ClaudeParser{Session: rawID}
							} else if logRef.Agent == "codex" {
								stateData.Parser = &CodexParser{Session: rawID}
							} else if logRef.Agent == "gemini" {
								stateData.Parser = &GeminiParser{Session: rawID}
							}
						}

						cursor := stateData.Cursor
						parser := stateData.Parser
						telemetryMu.Unlock()

						if parser != nil {
							newEvents, readErr := ReadNewEvents(cursor, parser, 500)
							if readErr == nil && len(newEvents) > 0 {
								models.AppendEvents(rawID, newEvents)
								parsedNewEvents = true
								lastEvent = newEvents[len(newEvents)-1]
							}
						}
					} else {
						telemetryMu.Unlock()
					}
				}

				// Always fetch capture-pane as fallback for approvals and missing logs
				cmd := exec.Command("tmux", "capture-pane", "-t", rawID, "-p")
				out, _ := cmd.Output()

				envCmd := exec.Command("tmux", "show-environment", "-t", rawID)
				envOut, _ := envCmd.Output()
				
				runner := "cat"
				runnerColor := "#58a6ff"
				envLines := strings.Split(string(envOut), "\n")
				for _, el := range envLines {
					if strings.HasPrefix(el, "POKIT_RUNNER=") {
						runner = strings.TrimPrefix(el, "POKIT_RUNNER=")
					} else if strings.HasPrefix(el, "POKIT_RUNNER_COLOR=") {
						runnerColor = strings.TrimPrefix(el, "POKIT_RUNNER_COLOR=")
					}
				}

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
		ref := mux.ParseSessionID(historyID)
		if ref.Adapter != "" && ref.Adapter != "tmux" {
			http.Error(w, "History not implemented for adapter", http.StatusNotImplemented)
			return
		}

		events := models.GetEvents(ref.RawID)
		w.Header().Set("Content-Type", "application/json")
		if len(events) > 0 {
			json.NewEncoder(w).Encode(events)
			return
		}

		// Fallback to tmux capture-pane
		cmd := exec.Command("tmux", "capture-pane", "-e", "-t", ref.RawID, "-p", "-S", "-10000")
		out, err := cmd.Output()
		if err != nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		
		fallbackEvents := []models.AgentEvent{{
			ID: "fallback-0", Session: historyID, Type: "message", Summary: "Legacy Tmux History", Detail: string(out), Timestamp: time.Now().Format(time.RFC3339),
		}}
		json.NewEncoder(w).Encode(fallbackEvents)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	var res []SessionTelemetry

	telemetryMu.Lock()
	for _, s := range mux.GetAllSessionsCached() {
		compoundID := s.AdapterName() + ":" + s.ID()
		data := telemetryCache[compoundID]
		if data == nil {
			res = append(res, SessionTelemetry{ID: compoundID, State: "idle", Load: 0, Runner: "cat", RunnerColor: "#58a6ff", Adapter: s.AdapterName(), Events: models.GetEvents(s.ID())}) // Use s.ID() (RawID) for GetEvents because watcher uses RawID
		} else {
			res = append(res, SessionTelemetry{ID: compoundID, State: data.State, Load: data.Load, Runner: data.Runner, RunnerColor: data.RunnerColor, Adapter: s.AdapterName(), Events: models.GetEvents(s.ID())}) // Use s.ID() (RawID)
		}
	}
	telemetryMu.Unlock()

	if res == nil {
		res = []SessionTelemetry{}
	}
	
	jsonBytes, _ := json.Marshal(res)
	w.Write(jsonBytes)
}

