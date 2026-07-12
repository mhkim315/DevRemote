package transcript

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// HandleTranscript returns the session-scoped Transcript read API.
//
// GET /api/sessions/{id}/transcript
//
// Returns a TranscriptResponse envelope with separated semantic and
// fallback channels. When AgentEvent is primary, byte-stream segments
// are routed to the fallback channel.
func HandleTranscript(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		sessionID := r.PathValue("id")
		if sessionID == "" {
			http.Error(w, "missing session id", http.StatusBadRequest)
			return
		}

		// Bounded page size.
		limit := 1000
		if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
			if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 5000 {
				limit = n
			}
		}

		var segments []TranscriptSegment

		if afterStr := r.URL.Query().Get("after"); afterStr != "" {
			cursor, err := strconv.ParseInt(afterStr, 10, 64)
			if err != nil {
				http.Error(w, "invalid cursor", http.StatusBadRequest)
				return
			}
			segments = svc.ListTranscriptAfter(sessionID, cursor)
		} else {
			segments = svc.ListTranscript(sessionID)
		}

		// Apply page limit.
		if len(segments) > limit {
			segments = segments[:limit]
		}

		resp := svc.BuildResponse(sessionID, segments)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(resp)
	}
}

// HandleTranscriptStats is an optional diagnostic endpoint.
func HandleTranscriptStats(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		sessionID := r.PathValue("id")
		if sessionID == "" {
			http.Error(w, "missing session id", http.StatusBadRequest)
			return
		}

		stats := svc.TranscriptStats(sessionID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	}
}
