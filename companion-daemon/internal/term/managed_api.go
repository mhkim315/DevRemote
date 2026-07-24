// Package term — SP0-P3: authenticated read-only REST surface for managed
// native sessions. Rows and status come DIRECTLY from the owned
// ManagedSessionRegistry — never from legacy discovery, telemetry
// snapshots, screen text, PTY bytes, or JSONL. DTOs are bounded: no prompts,
// command text, payloads, paths, tokens, raw protocol messages, or process
// details.
package term

import (
	"encoding/json"

	"devremote/companion-daemon/internal/devicetrust"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"devremote/companion-daemon/internal/models"
)

// decodeStrictJSONBody decodes EXACTLY one bounded JSON object: unknown
// fields and any trailing content after the object fail closed.
func decodeStrictJSONBody(w http.ResponseWriter, r *http.Request, maxBytes int64, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing content after JSON body")
	}
	return nil
}

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
// the managed catalog ONLY. The catalog dispatches by canonical adapter prefix;
// it never probes legacy discovery or the legacy telemetry snapshot.
// Auth is applied by the router.
func (h *Handlers) HandleManagedNativeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	if h.Catalog != nil {
		if rec, ok := h.Catalog.Get(id); ok {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(managedNativeStatusDTO(rec))
			return
		}
	}
	http.Error(w, "managed session not found", http.StatusNotFound)
}

// HandleManagedSessions serves GET /api/managed-sessions from the managed
// catalog only: it never touches legacy adapter discovery or the
// telemetry snapshot. Returns combined results from both managed registries
// through the catalog's deterministic merge.
func (h *Handlers) HandleManagedSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var out []ManagedNativeStatusDTO
	if h.Catalog != nil {
		for _, rec := range h.Catalog.List() {
			out = append(out, managedNativeStatusDTO(rec))
		}
	}
	if out == nil {
		out = []ManagedNativeStatusDTO{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// ManagedEventsResponse is the SP0.5-B authenticated read DTO: a bounded
// snapshot of the session followed by monotonic events strictly after the
// presented cursor. Field set is closed.
type ManagedEventsResponse struct {
	ContractVersion string                 `json:"contractVersion"`
	Session         ManagedNativeStatusDTO `json:"session"`
	Events          []ManagedEvent         `json:"events"`
	NextCursor      uint64                 `json:"nextCursor"`
}

// HandleManagedSessionEvents serves
// GET /api/managed-sessions/{id}/events?cursor=N&epoch=E from the owned
// registry + event store ONLY. Wrong session, wrong epoch, cursor ahead of
// the newest event, closed store, or malformed parameters fail closed.
// Re-reading the same cursor is idempotent.
func (h *Handlers) HandleManagedSessionEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Managed == nil {
		http.Error(w, "managed sessions not enabled", http.StatusNotFound)
		return
	}
	id := r.PathValue("id")
	rec, ok := h.Managed.Registry().Get(id)
	if !ok {
		http.Error(w, "managed session not found", http.StatusNotFound)
		return
	}
	epoch, err := strconv.ParseInt(r.URL.Query().Get("epoch"), 10, 64)
	if err != nil {
		http.Error(w, "epoch is required", http.StatusBadRequest)
		return
	}
	if epoch != rec.Epoch {
		http.Error(w, "stale session epoch", http.StatusConflict)
		return
	}
	cursor := uint64(0)
	if cs := r.URL.Query().Get("cursor"); cs != "" {
		cursor, err = strconv.ParseUint(cs, 10, 64)
		if err != nil {
			http.Error(w, "malformed cursor", http.StatusBadRequest)
			return
		}
	}
	store, storeEpoch, ok := h.Managed.eventStoreFor(id)
	if !ok || storeEpoch != rec.Epoch {
		http.Error(w, "managed session not found", http.StatusNotFound)
		return
	}
	events, rerr := store.readAfter(cursor, 128)
	if rerr != nil {
		http.Error(w, rerr.Error(), http.StatusConflict)
		return
	}
	next := cursor
	for _, ev := range events {
		if ev.Seq > next {
			next = ev.Seq
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ManagedEventsResponse{
		ContractVersion: managedEventContractVersion,
		Session:         managedNativeStatusDTO(rec),
		Events:          events,
		NextCursor:      next,
	})
}

// managedPromptRequest is the SP0.5-B input body. Unknown fields fail closed;
// the server derives device/host/permission identity — the body carries only
// the bounded text and the exact epoch binding.
type managedPromptRequest struct {
	Epoch int64  `json:"epoch"`
	Text  string `json:"text"`
}

// HandleManagedSessionPrompt serves POST /api/managed-sessions/{id}/prompt.
// Auth is applied by the router (terminal input permission in remote mode).
// Delivery goes ONLY through ManagedCodexService.SubmitPrompt — every bound,
// binding, and one-active-turn failure is fail-closed with zero provider
// writes.
func (h *Handlers) HandleManagedSessionPrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Managed == nil {
		http.Error(w, "managed sessions not enabled", http.StatusNotFound)
		return
	}
	var req managedPromptRequest
	if err := decodeStrictJSONBody(w, r, managedPromptMaxBytes+1024, &req); err != nil {
		http.Error(w, "malformed prompt body", http.StatusBadRequest)
		return
	}
	// SubmitPrompt reserves the active turn and writes to the provider. The
	// request principal is captured now and re-validated inside the provider lock.
	p := devicetrust.PrincipalFromContext(r.Context())
	if err := h.Managed.SubmitPrompt(r.PathValue("id"), req.Epoch, req.Text, func() error {
		if h.DeviceRegistry == nil {
			return nil
		}
		return h.DeviceRegistry.CommitEpoch(&devicetrust.EpochToken{DeviceID: p.DeviceID, Epoch: uint64(p.DeviceEpoch)})
	}); err != nil {
		status := http.StatusConflict
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

// managedLifecycleRequest is the SP0.5-C lifecycle body: the exact epoch
// binding only. Unknown fields fail closed.
type managedLifecycleRequest struct {
	Epoch int64 `json:"epoch"`
}

func (h *Handlers) handleManagedLifecycle(w http.ResponseWriter, r *http.Request, op func(id string, epoch int64, deviceID string, deviceEpoch uint64) error) {
	if h.Managed == nil {
		http.Error(w, "managed sessions not enabled", http.StatusNotFound)
		return
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	var req managedLifecycleRequest
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "malformed lifecycle body", http.StatusBadRequest)
		return
	}
	if _, err := dec.Token(); err != io.EOF {
		http.Error(w, "malformed lifecycle body", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	// Stop/kill/delete all mutate provider-owned session state. Recheck after
	// decoding and immediately before dispatching the selected operation.
	p := devicetrust.PrincipalFromContext(r.Context())
	if err := op(id, req.Epoch, p.DeviceID, uint64(p.DeviceEpoch)); err != nil {
		status := http.StatusConflict
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	// Bounded post-op state: the record for stop/kill; a tombstone for delete.
	w.Header().Set("Content-Type", "application/json")
	if rec, ok := h.Managed.Registry().Get(id); ok {
		json.NewEncoder(w).Encode(managedNativeStatusDTO(rec))
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "deleted"})
}

// HandleManagedSessionStop — POST /api/managed-sessions/{id}/stop {epoch}.
func (h *Handlers) HandleManagedSessionStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.handleManagedLifecycle(w, r, h.Managed.Stop)
}

// HandleManagedSessionKill — POST /api/managed-sessions/{id}/kill {epoch}.
func (h *Handlers) HandleManagedSessionKill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.handleManagedLifecycle(w, r, h.Managed.Kill)
}

// HandleManagedSessionDelete — DELETE /api/managed-sessions/{id} {epoch}.
func (h *Handlers) HandleManagedSessionDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.handleManagedLifecycle(w, r, h.Managed.Delete)
}

// ── C1D Claude managed-session lifecycle handlers ──

// handleManagedClaudeLifecycle delegates lifecycle ops to the Claude service.
func (h *Handlers) handleManagedClaudeLifecycle(w http.ResponseWriter, r *http.Request, op func(id string, epoch int64, deviceID string, deviceEpoch uint64) error) {
	if h.ManagedClaude == nil {
		http.Error(w, "managed claude sessions not enabled", http.StatusNotFound)
		return
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	var req managedLifecycleRequest
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "malformed lifecycle body", http.StatusBadRequest)
		return
	}
	if _, err := dec.Token(); err != io.EOF {
		http.Error(w, "malformed lifecycle body", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	p := devicetrust.PrincipalFromContext(r.Context())
	if err := op(id, req.Epoch, p.DeviceID, uint64(p.DeviceEpoch)); err != nil {
		status := http.StatusConflict
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if rec, ok := h.ManagedClaude.Registry().Get(id); ok {
		json.NewEncoder(w).Encode(managedNativeStatusDTO(rec))
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "deleted"})
}

// HandleManagedClaudeSessionStop — POST /api/managed-claude-sessions/{id}/stop {epoch}.
func (h *Handlers) HandleManagedClaudeSessionStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.handleManagedClaudeLifecycle(w, r, h.ManagedClaude.Stop)
}

// HandleManagedClaudeSessionKill — POST /api/managed-claude-sessions/{id}/kill {epoch}.
func (h *Handlers) HandleManagedClaudeSessionKill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.handleManagedClaudeLifecycle(w, r, h.ManagedClaude.Kill)
}

// HandleManagedClaudeSessionDelete — DELETE /api/managed-claude-sessions/{id} {epoch}.
func (h *Handlers) HandleManagedClaudeSessionDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.handleManagedClaudeLifecycle(w, r, h.ManagedClaude.Delete)
}

// appendManagedRows appends managed-session rows built directly from the
// owned registry to the /api/sessions response. Managed identity is
// authoritative: any snapshot row that collides with a managed canonical ID
// (e.g. a spoofed discovery adapter) is dropped so contradictory observer
// evidence can never overwrite or shadow the managed status. SP1-P1: managed
// rows carry the existing bounded SafeApprovalDTO projection (non-actionable,
// zero options) — no second approval DTO or CTA surface is created.
func appendManagedRows(snapshot []SessionTelemetry, managed *ManagedCodexService, approvals *AuthoritativeApprovalStore) []SessionTelemetry {
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
		var safe []SafeApprovalDTO
		if approvals != nil {
			safe = approvals.ListSafe(rec.SessionID)
		}
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
			Approvals:   safe,
		})
	}
	return out
}

// appendClaudeManagedRows appends managed Claude session rows built directly
// from the owned registry to the /api/sessions response. Same authoritative
// contract as appendManagedRows: managed identity overrides any snapshot row
// that collides with a managed canonical ID.
func appendClaudeManagedRows(snapshot []SessionTelemetry, managed *ManagedClaudeService, approvals *AuthoritativeApprovalStore) []SessionTelemetry {
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
	out := make([]SessionTelemetry, 0, len(snapshot)+len(recs))
	for _, row := range snapshot {
		if _, collides := managedIDs[row.ID]; collides {
			continue
		}
		out = append(out, row)
	}
	for _, rec := range recs {
		var safe []SafeApprovalDTO
		if approvals != nil {
			safe = approvals.ListSafe(rec.SessionID)
		}
		out = append(out, SessionTelemetry{
			ID:          rec.SessionID,
			DisplayID:   strings.TrimPrefix(rec.SessionID, claudeHeadlessAdapter+":"),
			State:       string(rec.NativeStatus),
			Adapter:     claudeHeadlessAdapter,
			Runner:      rec.Provider,
			RunnerColor: "#f97316",
			AgentKind:   rec.Provider,
			AgentStatus: string(rec.NativeStatus),
			Events:      []models.AgentEvent{},
			Approvals:   safe,
		})
	}
	return out
}
