// Package term — SP0-P3: authenticated read-only REST surface for managed
// native sessions. Rows and status come DIRECTLY from the owned
// ManagedSessionRegistry — never from mux.Registry discovery, telemetry
// snapshots, screen text, PTY bytes, or JSONL. DTOs are bounded: no prompts,
// command text, payloads, paths, tokens, raw protocol messages, or process
// details.
package term

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"devremote/companion-daemon/internal/models"
)

// ManagedNativeStatusDTO is the bounded managed-session read DTO. Field set
// is closed: identity + native semantic status only. The opaque ProcessID,
// OS/arch metadata, thread/turn identities, and all protocol payloads are
// deliberately excluded.
type ManagedNativeStatusDTO struct {
	ID              string `json:"id"`
	Provider        string `json:"provider"`
	Version         string `json:"version"`
	NativeStatus    string `json:"nativeStatus"`
	LaunchGen       int64  `json:"launchGen"`
	CreatedAt       string `json:"createdAt"`
	StatusChangedAt string `json:"statusChangedAt"`
	Exited          bool   `json:"exited"`
}

func managedNativeStatusDTO(rec ManagedSessionRecord) ManagedNativeStatusDTO {
	return ManagedNativeStatusDTO{
		ID:              rec.SessionID,
		Provider:        rec.Provider,
		Version:         rec.Version,
		NativeStatus:    string(rec.NativeStatus),
		LaunchGen:       rec.Epoch,
		CreatedAt:       rec.CreatedAt.UTC().Format(time.RFC3339),
		StatusChangedAt: rec.StatusChangedAt.UTC().Format(time.RFC3339),
		Exited:          rec.Exited,
	}
}

// HandleManagedNativeStatus serves GET /api/sessions/{id}/native-status from
// the owned registry ONLY. Auth is applied by the router (AuthMiddleware in
// insecure-local mode, device principal with sessions:read in remote mode).
func (h *Handlers) HandleManagedNativeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Managed == nil {
		http.Error(w, "managed sessions not enabled", http.StatusNotFound)
		return
	}
	rec, ok := h.Managed.Registry().Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "managed session not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(managedNativeStatusDTO(rec))
}

// HandleManagedSessions serves GET /api/managed-sessions ENTIRELY from the
// owned registry: it never touches mux.Registry, adapter discovery, or the
// telemetry snapshot, so a hanging or failing tmux/cmux refresh can never
// block or influence it. This is the SP0-certified managed list surface;
// managed rows appended to /api/sessions are a coexistence convenience that
// shares the legacy snapshot's availability.
func (h *Handlers) HandleManagedSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Managed == nil {
		http.Error(w, "managed sessions not enabled", http.StatusNotFound)
		return
	}
	recs := h.Managed.Registry().List()
	out := make([]ManagedNativeStatusDTO, 0, len(recs))
	for _, rec := range recs {
		out = append(out, managedNativeStatusDTO(rec))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// appendManagedRows appends managed-session rows built directly from the
// owned registry to the /api/sessions response. Managed identity is
// authoritative: any snapshot row that collides with a managed canonical ID
// (e.g. a spoofed discovery adapter) is dropped so contradictory observer
// evidence can never overwrite or shadow the managed status.
func appendManagedRows(snapshot []SessionTelemetry, managed *ManagedCodexService) []SessionTelemetry {
	if managed == nil {
		return snapshot
	}
	recs := managed.Registry().List()
	if len(recs) == 0 {
		return snapshot
	}
	managedIDs := make(map[string]struct{}, len(recs))
	for _, rec := range recs {
		managedIDs[rec.SessionID] = struct{}{}
	}
	// Fresh output slice — never mutate the caller's snapshot backing array.
	out := make([]SessionTelemetry, 0, len(snapshot)+len(recs))
	for _, row := range snapshot {
		if _, collides := managedIDs[row.ID]; collides {
			continue
		}
		out = append(out, row)
	}
	for _, rec := range recs {
		out = append(out, SessionTelemetry{
			ID:          rec.SessionID,
			DisplayID:   strings.TrimPrefix(rec.SessionID, codexAppServerAdapter+":"),
			State:       string(rec.NativeStatus),
			Adapter:     codexAppServerAdapter,
			Runner:      rec.Provider,
			RunnerColor: "#58a6ff",
			AgentKind:   rec.Provider,
			AgentStatus: string(rec.NativeStatus),
			Events:      []models.AgentEvent{},
		})
	}
	return out
}
