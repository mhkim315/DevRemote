package term

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
)

// SessionTelemetry holds the calculated state of a session.
type SessionTelemetry struct {
	ID              string              `json:"id"`
	DisplayID       string              `json:"displayId,omitempty"` // local ID without adapter prefix
	State           string              `json:"state"`
	Load            int                 `json:"load"`
	Runner          string              `json:"runner"`
	RunnerColor     string              `json:"runnerColor"`
	Adapter         string              `json:"adapter"`
	Capabilities    []string            `json:"capabilities,omitempty"` // e.g. ["live_stream","screen","history"]
	Events          []models.AgentEvent `json:"events"`
	AgentKind       string              `json:"agentKind,omitempty"`       // detected agent (Phase A5+)
	AgentStatus     string              `json:"agentStatus,omitempty"`     // agent activity status (Phase A5+)
	AgentConfidence float64             `json:"agentConfidence,omitempty"` // detection confidence 0.0-1.0 (Phase A5+)
	// Agent events flow through the existing Events field via
	// TelemetryService.processSession → EventStore → Snapshot.
	Stale         bool      `json:"stale,omitempty"`
	LastSuccessAt time.Time `json:"lastSuccessAt,omitempty"`
	LastError     string    `json:"lastError,omitempty"`
}

// sessionCapabilities returns the list of optional capabilities a session supports.
func sessionCapabilities(s mux.Session) []string {
	var caps []string
	if _, ok := s.(mux.StreamOpener); ok {
		caps = append(caps, "live_stream")
	}
	if _, ok := s.(mux.ScreenReader); ok {
		caps = append(caps, "screen")
	}
	if _, ok := s.(mux.HistoryReader); ok {
		caps = append(caps, "history")
	}
	if _, ok := s.(mux.ProcessProvider); ok {
		caps = append(caps, "process")
	}
	return caps
}

type sessionStateData struct {
	LastOutput       []byte
	LastActivity     time.Time
	State            string
	Load             int
	Runner           string
	RunnerColor      string
	Cursor           *LogCursor
	Parser           AgentLogParser
	SamplingFailures int
}

func sortTelemetry(items []SessionTelemetry) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
}

func isApprovalPrompt(line string) bool {
	return strings.Contains(line, "Do you want") ||
		strings.Contains(line, "proceed?") ||
		strings.Contains(line, "(y/n)") ||
		strings.Contains(line, "(y/N)") ||
		(strings.Contains(line, "1. Yes") && strings.Contains(line, "No"))
}

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

func evaluateState(stateData *sessionStateData, parsedNewEvents bool, lastEvent models.AgentEvent, logErr error, isWaiting bool, isThinkingFallback bool, diffSize int) {
	if isWaiting {
		stateData.State = "waiting"
		stateData.Load = 0
	} else if parsedNewEvents {
		stateData.LastActivity = time.Now()
		switch lastEvent.Type {
		case "user":
			stateData.State = "thinking"
			stateData.Load = 50
		case "tool_use":
			stateData.State = "working"
			stateData.Load = 100
		case "tool_result":
			stateData.State = "thinking"
			stateData.Load = 50
		case "message":
			if strings.Contains(lastEvent.Summary, "Thinking") || strings.Contains(lastEvent.Summary, "Reasoning") {
				stateData.State = "thinking"
				stateData.Load = 50
			} else {
				stateData.State = "idle"
				stateData.Load = 0
			}
		default:
			stateData.State = "working"
			stateData.Load = 50
		}
	} else if logErr == nil {
		if time.Since(stateData.LastActivity) > 10*time.Second {
			switch stateData.State {
			case "working":
				stateData.State = "thinking"
				stateData.Load = 50
				stateData.LastActivity = time.Now()
			case "thinking", "waiting":
				stateData.State = "idle"
				stateData.Load = 0
			}
		} else if stateData.State == "waiting" && !isWaiting {
			stateData.State = "idle"
			stateData.Load = 0
		}
	} else {
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

func preserveTransientSamplingFailure(stateData *sessionStateData, logErr error, screenErr error, adapterFailed bool, parsedNewEvents bool) bool {
	if parsedNewEvents || logErr == nil {
		stateData.SamplingFailures = 0
		return false
	}
	if screenErr == nil && !adapterFailed {
		stateData.SamplingFailures = 0
		return false
	}
	stateData.SamplingFailures++
	return stateData.SamplingFailures <= 2
}

// HandleSessionsV2 returns rich JSON metadata for all sessions.
func (h *Handlers) HandleSessionsV2(w http.ResponseWriter, r *http.Request) {
	reg := h.Registry
	if historyID := r.URL.Query().Get("history"); historyID != "" {
		events := h.Events.List(historyID)
		w.Header().Set("Content-Type", "application/json")
		if len(events) > 0 {
			json.NewEncoder(w).Encode(events)
			return
		}
		var out []byte
		var err error
		sess, err := reg.FindSession(r.Context(), mux.MigrateLegacyID(historyID))
		if err == nil {
			if hr, ok := sess.(mux.HistoryReader); ok {
				out, err = hr.ReadHistory(r.Context(), 10000)
			} else if sr, ok := sess.(mux.ScreenReader); ok {
				out, err = sr.ReadScreen(r.Context())
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
	if h.Telemetry != nil {
		json.NewEncoder(w).Encode(h.Telemetry.Snapshot(reg))
		return
	}
	// Fallback without telemetry service (e.g. tests).
	res := buildSimpleSnapshotWithDetector(reg, h.Events, h.AgentDetector)
	json.NewEncoder(w).Encode(res)
}

// normalizeEventType maps legacy parser types to common AgentEventType names.
func normalizeEventType(t string) string {
	switch t {
	case "user":
		return "user_message"
	case "tool_use":
		return "tool_call_started"
	case "tool_result":
		return "tool_call_finished"
	case "message":
		return "assistant_message"
	default:
		return t
	}
}

func normalizeEvents(evts []models.AgentEvent) []models.AgentEvent {
	for i := range evts {
		evts[i].Type = normalizeEventType(evts[i].Type)
	}
	return evts
}

func buildSimpleSnapshot(reg *mux.Registry, events EventStore) []SessionTelemetry {
	sessions := reg.Sessions(context.Background())
	res := make([]SessionTelemetry, 0)
	for _, s := range sessions {
		compoundID := s.AdapterName() + ":" + s.ID()
		snap, _ := reg.Snapshot(s.AdapterName())
		var errStr string
		if snap.LastError != nil {
			errStr = snap.LastError.Error()
		}
		isStale := snap.LastError != nil
		evts := normalizeEvents(events.List(compoundID))
		res = append(res, SessionTelemetry{
			ID: compoundID, DisplayID: s.ID(), State: "idle", Load: 0,
			Runner: "cat", RunnerColor: "#58a6ff", Adapter: s.AdapterName(),
			Capabilities: sessionCapabilities(s),
			Events:       evts, Stale: isStale, LastSuccessAt: snap.LastSuccessAt, LastError: errStr,
		})
	}
	sortTelemetry(res)
	return res
}

func buildSimpleSnapshotWithDetector(reg *mux.Registry, events EventStore, detector AgentDetector) []SessionTelemetry {
	result := buildSimpleSnapshot(reg, events)
	if detector == nil {
		return result
	}
	for i := range result {
		st := &result[i]
		ref := mux.ParseSessionID(st.ID)
		kind, status, confidence := detector.DetectAgent(st.ID, ref.Adapter, ref.LocalID, ProdDetectionEvidence{
			TermAdapter: ref.Adapter,
		})
		st.AgentKind = kind
		st.AgentStatus = status
		st.AgentConfidence = confidence
	}
	return result
}
