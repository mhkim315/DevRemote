package term

import (
	"encoding/json"
	"errors"
	"net/http"

	"devremote/companion-daemon/internal/devicetrust"
)

// auditLifecycle records a redacted stop/kill audit event using the request's
// authenticated principal (if any). No-op when no audit log is configured.
func (h *Handlers) auditLifecycle(r *http.Request, action, id string, err error) {
	if h.Audit == nil {
		return
	}
	result := devicetrust.ResultOK
	if err != nil {
		result = devicetrust.ResultError
	}
	ev := devicetrust.AuditEvent{Action: action, SessionID: id, Result: result}
	if p := devicetrust.PrincipalFromContext(r.Context()); p != nil {
		ev.DeviceID = p.DeviceID
		ev.CorrelationID = p.BearerSessionID
	}
	h.Audit.Record(ev)
}

// HandleSessionStop handles POST /api/sessions/{id}/stop.
func (h *Handlers) HandleSessionStop(w http.ResponseWriter, r *http.Request) {
	if h.Lifecycle == nil {
		writeLifecycleError(w, http.StatusInternalServerError, "lifecycle service unavailable")
		return
	}
	id := r.PathValue("id")
	res, err := h.Lifecycle.Stop(r.Context(), id)
	h.auditLifecycle(r, devicetrust.ActionSessionStop, id, err)
	writeLifecycleResult(w, res, err)
}

// HandleSessionKill handles POST /api/sessions/{id}/kill.
func (h *Handlers) HandleSessionKill(w http.ResponseWriter, r *http.Request) {
	if h.Lifecycle == nil {
		writeLifecycleError(w, http.StatusInternalServerError, "lifecycle service unavailable")
		return
	}
	id := r.PathValue("id")
	res, err := h.Lifecycle.Kill(r.Context(), id)
	h.auditLifecycle(r, devicetrust.ActionSessionKill, id, err)
	writeLifecycleResult(w, res, err)
}

// HandleSessionDelete handles DELETE /api/sessions/{id} (path form). The legacy
// query-form DELETE stays on HandleSessionsAPI for migration.
func (h *Handlers) HandleSessionDelete(w http.ResponseWriter, r *http.Request) {
	if h.Lifecycle == nil {
		writeLifecycleError(w, http.StatusInternalServerError, "lifecycle service unavailable")
		return
	}
	res, err := h.Lifecycle.Delete(r.Context(), r.PathValue("id"))
	writeLifecycleResult(w, res, err)
}

// writeLifecycleResult maps a lifecycle result/error to a structured JSON
// response with an appropriate, non-generic status code.
func writeLifecycleResult(w http.ResponseWriter, res LifecycleResult, err error) {
	if err != nil {
		switch {
		case errors.Is(err, ErrLifecycleNotFound):
			writeLifecycleError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrLifecycleUnsupported):
			// Capability boundary: not a managed session.
			writeLifecycleError(w, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, ErrLifecycleNotTerminal):
			writeLifecycleError(w, http.StatusConflict, err.Error())
		default:
			writeLifecycleError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(res)
}

func writeLifecycleError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
