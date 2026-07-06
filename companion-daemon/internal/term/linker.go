package term

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"devremote/companion-daemon/internal/mux"
)

type SessionLink struct {
	SessionID         string `json:"sessionId"`
	Provider          string `json:"provider"`
	ExternalSessionID string `json:"externalSessionId"`
	SessionTitle      string `json:"sessionTitle"`
	WorkspaceID       string `json:"workspaceId"` // optional for now
	Stale             bool   `json:"stale,omitempty"`
}

// LoadLinks reads links from the LinkStore and validates stale status.
// Legacy ID migration is handled by LinkStore.Load().
func LoadLinks(reg *mux.Registry, store LinkStore) error {
	ctx := context.Background()
	if err := store.Load(ctx); err != nil {
		return err
	}

	// Detect stale links by comparing stored title with current session title.
	for _, link := range store.List() {
		if link.Stale {
			continue
		}
		if sess, err := reg.FindSession(ctx, link.SessionID); err == nil {
			if sess.Title() != link.SessionTitle {
				link.Stale = true
				_ = store.Put(ctx, link)
			}
		}
	}

	return nil
}

// LinkSession creates or updates a session link.
func LinkSession(link SessionLink, reg *mux.Registry, store LinkStore, events EventStore) error {
	link.SessionID = mux.MigrateLegacyID(link.SessionID)

	if sess, err := reg.FindSession(context.Background(), link.SessionID); err == nil {
		link.SessionTitle = sess.Title()
	}

	if err := store.Put(context.Background(), link); err != nil {
		return err
	}

	events.Clear(link.SessionID)
	return nil
}

// UnlinkSession removes a session link.
func UnlinkSession(sessionID string, reg *mux.Registry, store LinkStore, events EventStore) error {
	sessionID = mux.MigrateLegacyID(sessionID)

	if err := store.Delete(context.Background(), sessionID); err != nil {
		return err
	}

	events.Clear(sessionID)
	return nil
}

// GetLink returns a link and checks staleness dynamically.
func GetLink(sessionID string, reg *mux.Registry, store LinkStore) (SessionLink, bool) {
	sessionID = mux.MigrateLegacyID(sessionID)
	l, ok := store.Get(sessionID)
	if ok && !l.Stale {
		if sess, err := reg.FindSession(context.Background(), sessionID); err == nil && sess.Title() != l.SessionTitle {
			l.Stale = true
		}
	}
	return l, ok
}

// GetAllLinks returns all links.
func GetAllLinks(store LinkStore) []SessionLink {
	return store.List()
}

func (h *Handlers) HandleLinksAPI(w http.ResponseWriter, r *http.Request) {
	reg := h.Registry

	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		links := GetAllLinks(h.Links)
		json.NewEncoder(w).Encode(links)
		return
	} else if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		var link SessionLink
		if err := json.NewDecoder(r.Body).Decode(&link); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if link.SessionID == "" || link.ExternalSessionID == "" || link.Provider == "" {
			http.Error(w, "missing required fields", http.StatusBadRequest)
			return
		}
		if strings.Contains(link.ExternalSessionID, "/") || strings.Contains(link.ExternalSessionID, "\\") || strings.Contains(link.ExternalSessionID, "..") {
			http.Error(w, "invalid uuid format", http.StatusBadRequest)
			return
		}

		if err := LinkSession(link, reg, h.Links, h.Events); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if h.Telemetry != nil {
			h.Telemetry.Clear(link.SessionID)
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "linked"})
		return
	} else if r.Method == http.MethodDelete {
		sessionID := r.URL.Query().Get("session")
		if sessionID == "" {
			http.Error(w, "missing session parameter", http.StatusBadRequest)
			return
		}
		if err := UnlinkSession(sessionID, reg, h.Links, h.Events); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if h.Telemetry != nil {
			h.Telemetry.Clear(sessionID)
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "unlinked"})
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
