package cockpit

import (
	"encoding/json"
	"net/http"
)

// RegisterCockpitHandler exposes the store's read-only projection. It registers
// only GET and deliberately provides no authority action endpoint.
func RegisterCockpitHandler(mux *http.ServeMux, store *CockpitStore) {
	mux.HandleFunc("GET /api/cockpit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(store.ReadAll())
	})
}
