package term

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/sessionid"
)

// SessionTelemetry holds the calculated state of a session.
type SessionTelemetry struct {
	ID        string `json:"id"`
	DisplayID string `json:"displayId,omitempty"` // local ID without adapter prefix
	// PA3 Step 2: State removed — use agentActivity.status + agentStatus instead.
	// LifecycleState is the daemon-AUTHORITATIVE managed-session lifecycle sourced
	// from the Session Catalog: starting|running|stopping|exited|killed|failed.
	// It is SEPARATE from `state`/`agentStatus` (agent activity: idle/thinking/
	// working/waiting). Empty for sessions that are not Pokit-managed (external
	// those have no managed lifecycle. Mobile gates
	// Stop/Kill/Delete on this field, never on list presence/absence.
	// PA3 Step 2: legacy fields retained as compatibility stubs for tests.
	// Zero values, not serialized (json:"-"). Removed in Step 4 (DTO update).
	State       string              `json:"-"`
	Load        int                 `json:"-"`
	Runner      string              `json:"-"`
	RunnerColor string              `json:"-"`
	Events      []models.AgentEvent `json:"-"`

	LifecycleState      string            `json:"lifecycleState,omitempty"`
	Adapter             string            `json:"adapter"`
	Capabilities        []string          `json:"capabilities,omitempty"`        // session-level: e.g. ["live_stream","screen","history"]
	AdapterCapabilities []string          `json:"adapterCapabilities,omitempty"` // adapter-level: e.g. ["control","liveTerminal","reliableTranscript"]
	AgentKind           string            `json:"agentKind,omitempty"`           // detected agent (Phase A5+)
	AgentStatus         string            `json:"agentStatus,omitempty"`         // agent activity status (Phase A5+)
	AgentConfidence     float64           `json:"agentConfidence,omitempty"`     // detection confidence 0.0-1.0 (Phase A5+)
	Approvals           []SafeApprovalDTO `json:"approvals,omitempty"`           // bounded, redacted safe approval DTOs (A1 B6)
	// AgentActivity is the S1 additive, authenticated ADVISORY agent-activity
	// projection sourced from the session-owned AgentStatusStore. It is kept
	// SEPARATE from the daemon-authoritative lifecycle (LifecycleState) and from
	// telemetry poll health (Stale). It never enables lifecycle/approval actions.
	AgentActivity *AgentActivityDTO `json:"agentActivity,omitempty"`
	// Agent events flow through the existing Events field via
	// PB.2b: Managed ingestion → Transcript → Snapshot.
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
func mergeLifecycleState(snapshot []SessionTelemetry, lifecycle *LifecycleService) []SessionTelemetry {
	if lifecycle == nil || lifecycle.OwnedPTY() == nil {
		return snapshot
	}
	// PA2c: controlled-PTY lifecycle rows come from the OwnedPTYRuntime store
	// (the former SessionCatalog controlled-PTY portion). Structured provider
	// rows come exclusively from ManagedRuntimeCatalog via appendCatalogRows —
	// SessionCatalog no longer exists as a second provider lifecycle owner.
	catalog := lifecycle.OwnedPTY()
	live := make(map[string]int, len(snapshot))
	for i := range snapshot {
		live[snapshot[i].ID] = i
	}
	// (1) authoritative state for live managed rows.
	for i := range snapshot {
		if e, ok := catalog.Get(snapshot[i].ID); ok {
			snapshot[i].LifecycleState = string(e.State)
			snapshot[i].AdapterCapabilities = []string{"liveTerminal", "live_stream", "history", "managedLifecycle", "input"}
		}
	}
	// (2) all controlled-PTY rows are owned by the runtime, including live
	// and retained terminal generations.
	for _, e := range catalog.List() {
		if _, ok := live[e.ID]; ok {
			continue // live row already present and annotated
		}
		snapshot = append(snapshot, SessionTelemetry{
			ID:                  e.ID,
			DisplayID:           sessionid.ParseSessionID(e.ID).LocalID,
			LifecycleState:      string(e.State),
			Adapter:             e.Adapter,
			Capabilities:        []string{"live_stream", "history"},
			AdapterCapabilities: []string{"liveTerminal", "live_stream", "history", "managedLifecycle", "input"},
		})
	}
	sortTelemetry(snapshot)
	return snapshot
}

func sortTelemetry(items []SessionTelemetry) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})
}

// applyDeviceInputCapability publishes terminal:input only to a device whose
// authenticated server principal holds that permission. It copies rows and
// capability slices so a principal-specific response cannot mutate telemetry's
// shared snapshot for another device.
func applyDeviceInputCapability(rows []SessionTelemetry, p *devicetrust.Principal) []SessionTelemetry {
	if p == nil || !hasTicketPerm(p, devicetrust.PermTerminalInput) {
		return rows
	}
	out := append([]SessionTelemetry(nil), rows...)
	for i := range out {
		caps := append([]string(nil), out[i].Capabilities...)
		found := false
		for _, cap := range caps {
			if cap == devicetrust.PermTerminalInput {
				found = true
				break
			}
		}
		if !found {
			caps = append(caps, devicetrust.PermTerminalInput)
		}
		out[i].Capabilities = caps
	}
	return out
}

// HandleSessionsV2 returns rich JSON metadata for all sessions.
func (h *Handlers) HandleSessionsV2(w http.ResponseWriter, r *http.Request) {
	// PA3 Step 3: legacy query-parameter endpoints removed.
	// ?activity= → 410 Gone (replaced by GET /api/sessions/{id}/transcript)
	// ?history=  → 410 Gone (replaced by GET /api/sessions/{id}/transcript)
	if q := r.URL.Query().Get("activity"); q != "" {
		http.Error(w, "gone — use GET /api/sessions/{id}/transcript", http.StatusGone)
		return
	}
	if q := r.URL.Query().Get("history"); q != "" {
		http.Error(w, "gone — use GET /api/sessions/{id}/transcript", http.StatusGone)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if h.Telemetry != nil {
		snapshot := mergeLifecycleState(h.Telemetry.Snapshot(), h.Lifecycle)
		snapshot = appendCatalogRows(snapshot, h.Catalog, h.Lifecycle, h.Approvals)
		json.NewEncoder(w).Encode(applyDeviceInputCapability(snapshot, devicetrust.PrincipalFromContext(r.Context())))
		return
	}
	// PA3 Step 6b: Fallback without telemetry service (e.g. tests).
	// Transcript is canonical for snapshot path.
	res := mergeLifecycleState(nil, h.Lifecycle)
	res = appendCatalogRows(res, h.Catalog, h.Lifecycle, h.Approvals)
	json.NewEncoder(w).Encode(applyDeviceInputCapability(res, devicetrust.PrincipalFromContext(r.Context())))
}
