package transcript

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// HandleTranscript is the HTTP handler for the session-scoped Transcript read API.
//
// GET /api/sessions/{id}/transcript
//   - Returns all segments for the session, oldest-first.
//   - Query param `?after=<seq>` returns only segments with Seq > seq.
//
// Authentication and session-scoping are enforced by the caller (middleware).
// This handler performs no auth; it only reads from the Transcript store.
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

		if segments == nil {
			segments = []TranscriptSegment{}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(segments)
	}
}

// HandleTranscriptStats is an optional diagnostic endpoint.
//
// GET /api/sessions/{id}/transcript/stats
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

// HandleTranscriptSource is an optional arbitration query endpoint.
//
// GET /api/sessions/{id}/transcript/source
func HandleTranscriptSource(svc *Service) http.HandlerFunc {
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

		source := svc.PrimarySource(sessionID)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"sessionId":     sessionID,
			"primarySource": string(source),
		})
	}
}
