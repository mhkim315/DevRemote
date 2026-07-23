package cockpit

import (
	"encoding/json"
	"net/http"

	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/timeline/writer"
)

// RegisterTimelineStatsHandler exposes the writer's Stats and Health as a
// read-only authenticated GET endpoint. It never blocks on I/O.
func RegisterTimelineStatsHandler(mux *http.ServeMux, w *writer.Writer, sessions *devicetrust.DeviceSessionManager) {
	if w == nil {
		return
	}
	handler := func(rw http.ResponseWriter, r *http.Request) {
		stats := w.Stats()
		degraded, reason := w.HealthSnapshot()
		cfg := w.ConfigSnapshot()
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(map[string]interface{}{
			"timeline": map[string]interface{}{
				"enabled":    true,
				"shadowPath": cfg.Path,
				"appended":   stats.Appended,
				"dropped":    stats.Dropped,
				"failures":   stats.Failures,
				"degraded":   degraded,
				"reason":     reason,
			},
		})
	}
	mux.HandleFunc("GET /api/timeline/stats", devicetrust.RequirePrincipal(sessions, handler, devicetrust.PermSessionsRead))
}
