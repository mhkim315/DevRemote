package cockpit

import (
	"encoding/json"
	"net/http"

	"devremote/companion-daemon/internal/devicetrust"
)

// RegisterCockpitHandler exposes the store's read-only projection. It registers
// only GET and deliberately provides no authority action endpoint.
func RegisterCockpitHandler(mux *http.ServeMux, store *CockpitStore, sessions *devicetrust.DeviceSessionManager) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		store.Refresh()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(store.ReadAll())
	}
	mux.HandleFunc("GET /api/cockpit", devicetrust.RequirePrincipal(sessions, handler, devicetrust.PermSessionsRead))
}
