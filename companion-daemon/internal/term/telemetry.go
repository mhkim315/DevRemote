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
	ID        string `json:"id"`
	DisplayID string `json:"displayId,omitempty"` // local ID without adapter prefix
	State     string `json:"state"`
	// LifecycleState is the daemon-AUTHORITATIVE managed-session lifecycle sourced
	// from the Session Catalog: starting|running|stopping|exited|killed|failed.
	// It is SEPARATE from `state`/`agentStatus` (agent activity: idle/thinking/
	// working/waiting). Empty for sessions that are not Pokit-managed (external
	// tmux/cmux/observe-only) — those have no managed lifecycle. Mobile gates
	// Stop/Kill/Delete on this field, never on list presence/absence.
	LifecycleState      string              `json:"lifecycleState,omitempty"`
	Load                int                 `json:"load"`
	Runner              string              `json:"runner"`
	RunnerColor         string              `json:"runnerColor"`
	Adapter             string              `json:"adapter"`
	Capabilities        []string            `json:"capabilities,omitempty"`        // session-level: e.g. ["live_stream","screen","history"]
	AdapterCapabilities []string            `json:"adapterCapabilities,omitempty"` // adapter-level: e.g. ["control","liveTerminal","reliableTranscript"]
	Events              []models.AgentEvent `json:"events"`
	AgentKind           string              `json:"agentKind,omitempty"`       // detected agent (Phase A5+)
	AgentStatus         string              `json:"agentStatus,omitempty"`     // agent activity status (Phase A5+)
	AgentConfidence     float64             `json:"agentConfidence,omitempty"` // detection confidence 0.0-1.0 (Phase A5+)
	Approvals           []SafeApprovalDTO   `json:"approvals,omitempty"`       // bounded, redacted safe approval DTOs (A1 B6)
	// AgentActivity is the S1 additive, authenticated ADVISORY agent-activity
	// projection sourced from the session-owned AgentStatusStore. It is kept
	// SEPARATE from the daemon-authoritative lifecycle (LifecycleState) and from
	// telemetry poll health (Stale). It never enables lifecycle/approval actions.
	AgentActivity *AgentActivityDTO `json:"agentActivity,omitempty"`
	// Agent events flow through the existing Events field via
	// TelemetryService.processSession → EventStore → Snapshot.
	Stale         bool      `json:"stale,omitempty"`
	LastSuccessAt time.Time `json:"lastSuccessAt,omitempty"`
	LastError     string    `json:"lastError,omitempty"`
}

// AgentActivityDTO is the S1 advisory agent-activity dimension exposed on a
// session row. It carries ONLY bounded, UI-needed values: status (frozen T0
// vocabulary), the winning provenance, confidence, degraded, the observation time,
// and whether the record is stale. It deliberately excludes raw errors, evidence,
// prompts, private paths, and adapter records. Consumers must render it as agent
// activity — never as session lifecycle, and a stale record must not be shown as
// the current live activity.
type AgentActivityDTO struct {
	ContractVersion string  `json:"contractVersion"` // frozen T0 contract.ContractVersion; mobile validates exactly
	Status          string  `json:"status"`          // agent.AgentStatus value
	Provenance      string  `json:"provenance"`      // contract.Provenance tier that won
	Confidence      float64 `json:"confidence"`      // 0.0–1.0
	Degraded        bool    `json:"degraded"`
	ObservedAt      string  `json:"observedAt"` // RFC3339 (UTC)
	Stale           bool    `json:"stale"`      // activity-record staleness (NOT poll health)
}

// mergeLifecycleState makes /api/sessions authoritative for managed-session
// lifecycle. It (1) annotates each LIVE row that has a catalog entry with the
// Catalog's authoritative LifecycleState, and (2) appends RETAINED terminal
// catalog rows (exited/killed/failed) whose runtime has already left the
// Registry, so an ended managed session stays listable for Activity/Transcript
// review and Delete History until it is explicitly deleted. Dedup is by canonical
// ID (a live row always wins over a catalog row for the same id). Non-managed
// sessions (no catalog entry) are untouched and carry no lifecycleState.
func mergeLifecycleState(snapshot []SessionTelemetry, lifecycle *LifecycleService, reg *mux.Registry) []SessionTelemetry {
	if lifecycle == nil {
		return snapshot
	}
	catalog := lifecycle.Catalog()
	live := make(map[string]int, len(snapshot))
	for i := range snapshot {
		live[snapshot[i].ID] = i
	}
	// (1) authoritative state for live managed rows.
	for i := range snapshot {
		if e, ok := catalog.Get(snapshot[i].ID); ok {
			snapshot[i].LifecycleState = string(e.State)
		}
	}
	// (2) retained terminal rows not represented live.
	for _, e := range catalog.List() {
		if _, ok := live[e.ID]; ok {
			continue // live row already present and annotated
		}
		if !e.State.Terminal() {
			// A managed row absent from the live Registry but not yet terminal is a
			// transient finalize race; skip rather than surface a phantom session.
			continue
		}
		snapshot = append(snapshot, SessionTelemetry{
			ID:             e.ID,
			DisplayID:      mux.ParseSessionID(e.ID).LocalID,
			State:          "idle", // agent-activity neutral; lifecycle is below
			LifecycleState: string(e.State),
			Runner:         e.Name,
			Adapter:        e.Adapter,
			// History/Activity is retained until Delete, so keep the read affordance.
			Capabilities:        []string{"history"},
			AdapterCapabilities: adapterCapabilityStrings(reg, e.Adapter),
			Events:              []models.AgentEvent{},
		})
	}
	sortTelemetry(snapshot)
	return snapshot
}

// adapterCapabilityStrings returns the adapter-level capabilities as JSON-safe strings.
func adapterCapabilityStrings(reg *mux.Registry, adapterName string) []string {
	if reg == nil {
		return nil
	}
	adapter, ok := reg.Adapter(adapterName)
	if !ok {
		return nil
	}
	caps := mux.AdapterCapabilities(adapter)
	out := make([]string, len(caps))
	for i, c := range caps {
		out[i] = string(c)
	}
	return out
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
	LastOutput   []byte
	LastActivity time.Time
	State        string
	Load         int
	Runner       string
	RunnerColor  string
	Cursor       *LogCursor
	Parser       AgentLogParser
	// T3: accepted adapter state
	// T3: accepted adapter positioned state (nil until used)
	Adapter *adapterState
	// SamplingFailures tracks consecutive telemetry errors.
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
		case "user", "user_message":
			stateData.State = "thinking"
			stateData.Load = 50
		case "tool_use", "tool_call_started":
			stateData.State = "working"
			stateData.Load = 100
		case "tool_result", "tool_call_finished":
			stateData.State = "thinking"
			stateData.Load = 50
		case "thinking":
			stateData.State = "thinking"
			stateData.Load = 50
		case "approval_requested":
			stateData.State = "waiting"
			stateData.Load = 0
		case "message", "assistant_message":
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

	// E8f: minimal read endpoint for captured terminal activity.
	if activityID := r.URL.Query().Get("activity"); activityID != "" {
		w.Header().Set("Content-Type", "application/json")
		if h.Activity != nil {
			events := h.Activity.List(activityID)
			if events == nil {
				events = []ActivityEvent{}
			}
			json.NewEncoder(w).Encode(events)
		} else {
			json.NewEncoder(w).Encode([]ActivityEvent{})
		}
		return
	}

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
		snapshot := mergeLifecycleState(h.Telemetry.Snapshot(reg), h.Lifecycle, reg)
		json.NewEncoder(w).Encode(appendManagedRows(snapshot, h.Managed))
		return
	}
	// Fallback without telemetry service (e.g. tests).
	res := mergeLifecycleState(buildSimpleSnapshotWithDetector(reg, h.Events, h.AgentDetector), h.Lifecycle, reg)
	json.NewEncoder(w).Encode(appendManagedRows(res, h.Managed))
}

// normalizeEventType maps legacy parser types + summary hints to common AgentEventType.
func normalizeEventType(e *models.AgentEvent) {
	switch e.Type {
	case "user":
		e.Type = "user_message"
	case "tool_use":
		e.Type = "tool_call_started"
	case "tool_result":
		e.Type = "tool_call_finished"
	case "message":
		// Term-layer parser uses Summary to distinguish thinking from message.
		if containsAny(e.Summary, "Thinking", "Reasoning") {
			e.Type = "thinking"
		} else {
			e.Type = "assistant_message"
		}
	}
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}

// mapLegacyState converts legacy state machine states to common AgentStatus.
func mapLegacyState(state string) string {
	switch state {
	case "waiting":
		return "waiting_approval"
	default:
		return state
	}
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
		evts := events.List(compoundID)
		res = append(res, SessionTelemetry{
			ID: compoundID, DisplayID: s.ID(), State: "idle", Load: 0,
			Runner: "cat", RunnerColor: "#58a6ff", Adapter: s.AdapterName(),
			Capabilities:        sessionCapabilities(s),
			AdapterCapabilities: adapterCapabilityStrings(reg, s.AdapterName()),
			Events:              evts, Stale: isStale, LastSuccessAt: snap.LastSuccessAt, LastError: errStr,
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
