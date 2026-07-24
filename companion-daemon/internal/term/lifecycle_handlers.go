package term

import (
	"encoding/json"
	"errors"
	"net/http"

	"devremote/companion-daemon/internal/devicetrust"
)

// auditLifecycle records a redacted stop/kill audit event. It records ONLY the
// canonical server-derived session ID from the lifecycle result (validated by a
// real session lookup); the untrusted URL path value is never echoed. When the
// operation failed and no canonical ID is available, sessionId is omitted.
func (h *Handlers) auditLifecycle(r *http.Request, action string, res LifecycleResult, err error) {
	if h.Audit == nil {
		return
	}
	result := devicetrust.ResultOK
	if err != nil {
		result = devicetrust.ResultError
	}
	ev := devicetrust.AuditEvent{Action: action, SessionID: res.SessionID, Result: result}
	if p := devicetrust.PrincipalFromContext(r.Context()); p != nil {
		ev.DeviceID = p.DeviceID
		ev.CorrelationID = p.BearerSessionID
	}
	h.Audit.Record(ev)
}

// HandleSessionStop handles POST /api/sessions/{id}/stop.
func (h *Handlers) HandleSessionStop(w http.ResponseWriter, r *http.Request) {
	if err := h.recheckEpoch(r); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if h.Lifecycle == nil {
		writeLifecycleError(w, http.StatusInternalServerError, "lifecycle service unavailable")
		return
	}
	res, err := h.Lifecycle.Stop(r.Context(), r.PathValue("id"))
	h.auditLifecycle(r, devicetrust.ActionSessionStop, res, err)
	writeLifecycleResult(w, res, err)
}

// HandleSessionKill handles POST /api/sessions/{id}/kill.
func (h *Handlers) HandleSessionKill(w http.ResponseWriter, r *http.Request) {
	if err := h.recheckEpoch(r); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if h.Lifecycle == nil {
		writeLifecycleError(w, http.StatusInternalServerError, "lifecycle service unavailable")
		return
	}
	res, err := h.Lifecycle.Kill(r.Context(), r.PathValue("id"))
	h.auditLifecycle(r, devicetrust.ActionSessionKill, res, err)
	writeLifecycleResult(w, res, err)
}

// HandleSessionDelete handles DELETE /api/sessions/{id} (path form). The legacy
// query-form DELETE stays on HandleSessionsAPI for migration.
func (h *Handlers) HandleSessionDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.recheckEpoch(r); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
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
		case errors.Is(err, ErrLifecycleStaleGeneration):
			// PA2c: the addressed generation was replaced between catalog lookup
			// and the owner's decisive comparison; the replacement is unaffected.
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
