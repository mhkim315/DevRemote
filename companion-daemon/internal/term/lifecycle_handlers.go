package term

import (
	"encoding/json"
	"errors"
	"net/http"
)

// HandleSessionStop handles POST /api/sessions/{id}/stop.
func (h *Handlers) HandleSessionStop(w http.ResponseWriter, r *http.Request) {
	if h.Lifecycle == nil {
		writeLifecycleError(w, http.StatusInternalServerError, "lifecycle service unavailable")
		return
	}
	res, err := h.Lifecycle.Stop(r.Context(), r.PathValue("id"))
	writeLifecycleResult(w, res, err)
}

// HandleSessionKill handles POST /api/sessions/{id}/kill.
func (h *Handlers) HandleSessionKill(w http.ResponseWriter, r *http.Request) {
	if h.Lifecycle == nil {
		writeLifecycleError(w, http.StatusInternalServerError, "lifecycle service unavailable")
		return
	}
	res, err := h.Lifecycle.Kill(r.Context(), r.PathValue("id"))
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
