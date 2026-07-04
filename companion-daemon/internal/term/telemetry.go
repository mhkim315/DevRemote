package term

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// SessionTelemetry holds the calculated state of a tmux session.
type SessionTelemetry struct {
	ID          string       `json:"id"`
	State       string       `json:"state"` // "idle", "thinking", "working", "waiting"
	Load        int          `json:"load"`  // 0-100 (animation speed)
	Runner      string       `json:"runner"`
	RunnerColor string       `json:"runnerColor"`
	Events      []AgentEvent `json:"events"`
}

type sessionStateData struct {
	LastOutput   []byte
	LastActivity time.Time
	State        string
	Load         int
	Runner       string
	RunnerColor  string
}

var (
	telemetryCache = make(map[string]*sessionStateData)
	telemetryMu    sync.Mutex
)

// StartTelemetryLoop runs in the background and periodically takes snapshots of
// all active tmux sessions to calculate their speed and state.
func StartTelemetryLoop() {
	go func() {
		for {
			time.Sleep(2 * time.Second)
			sessions, _ := DefaultMux.ListSessions()

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
				cmd := exec.Command("tmux", "capture-pane", "-t", s, "-p")
				out, err := cmd.Output()
				if err != nil {
					continue
				}

				envCmd := exec.Command("tmux", "show-environment", "-t", s)
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
				isThinking := false
				lines := strings.Split(string(out), "\n")
				
				tailLines := lines
				if len(lines) > 5 {
					tailLines = lines[len(lines)-5:]
				}

				for _, line := range tailLines {
					if strings.Contains(line, "Do you want") ||
						strings.Contains(line, "proceed?") ||
						strings.Contains(line, "(y/n)") ||
						strings.Contains(line, "(y/N)") ||
						(strings.Contains(line, "1. Yes") && strings.Contains(line, "No")) {
						isWaiting = true
						break
					}
				}

				if !isWaiting {
					for _, line := range tailLines {
						if strings.Contains(line, "Thinking...") || strings.Contains(line, "Querying") {
							isThinking = true
							break
						}
					}
				}

				if isWaiting {
					stateData.State = "waiting"
					stateData.Load = 0
				} else if diffSize > 50 {
					stateData.State = "working"
					stateData.Load = 100
					stateData.LastActivity = time.Now()
				} else if diffSize > 0 || isThinking {
					stateData.State = "thinking"
					stateData.Load = 50
					stateData.LastActivity = time.Now()
				} else {
					if time.Since(stateData.LastActivity) > 5*time.Second {
						stateData.State = "idle"
						stateData.Load = 0
					} else {
						// Grace period: slow down to thinking before falling asleep
						stateData.State = "thinking"
						stateData.Load = 20
					}
				}

				stateData.LastOutput = out
				telemetryMu.Unlock()
			}
		}
	}()
}

// HandleSessionsV2 replaces the old HandleSessions API and returns rich JSON metadata
func HandleSessionsV2(w http.ResponseWriter, r *http.Request) {
	if historyID := r.URL.Query().Get("history"); historyID != "" {
		cmd := exec.Command("tmux", "capture-pane", "-e", "-t", historyID, "-p", "-S", "-10000")
		out, err := cmd.Output()
		if err != nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write(out)
		return
	}

	sessions, err := DefaultMux.ListSessions()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	var res []SessionTelemetry

	telemetryMu.Lock()
	for _, s := range sessions {
		data := telemetryCache[s]
		if data == nil {
			res = append(res, SessionTelemetry{ID: s, State: "idle", Load: 0, Runner: "cat", RunnerColor: "#58a6ff", Events: GetEvents(s)})
		} else {
			res = append(res, SessionTelemetry{ID: s, State: data.State, Load: data.Load, Runner: data.Runner, RunnerColor: data.RunnerColor, Events: GetEvents(s)})
		}
	}
	telemetryMu.Unlock()

	if res == nil {
		res = []SessionTelemetry{}
	}
	
	jsonBytes, _ := json.Marshal(res)
	w.Write(jsonBytes)
}
