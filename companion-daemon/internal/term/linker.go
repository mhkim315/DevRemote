package term

import (
	"context"
	"encoding/json"
	"devremote/companion-daemon/internal/mux"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"devremote/companion-daemon/internal/models"
)

type SessionLink struct {
	SessionID         string `json:"sessionId"`
	Provider          string `json:"provider"`
	ExternalSessionID string `json:"externalSessionId"`
	SessionTitle      string `json:"sessionTitle"`
	WorkspaceID       string `json:"workspaceId"` // optional for now
	Stale             bool   `json:"stale,omitempty"`
}

var (
	sessionLinks = make(map[string]SessionLink)
	linkerMu     sync.RWMutex
)

func getLinksPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".devremote")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "links.json"), nil
}

// LoadLinks reads links from disk and validates them.
func LoadLinks(reg *mux.Registry) error {
	linkerMu.Lock()
	defer linkerMu.Unlock()

	path, err := getLinksPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No links yet
		}
		return err
	}

	var links []SessionLink
	if err := json.Unmarshal(data, &links); err != nil {
		return err
	}

	var migrated bool
	// Validate and detect stale links
	for i, link := range links {
		newID := mux.MigrateLegacyID(link.SessionID)
		if newID != link.SessionID {
			migrated = true
			link.SessionID = newID
			links[i].SessionID = newID
		}

		// Verify if the session still matches the expected title
		sess, err := reg.FindSession(context.Background(), link.SessionID)
		if err == nil {
			if sess.Title() != link.SessionTitle {
				links[i].Stale = true
			} else {
				links[i].Stale = false
			}
		} else {
			// Session might be offline; we don't mark as stale immediately unless we know it's a mismatch
			// For now, assume it's pending/offline
		}
		sessionLinks[link.SessionID] = links[i]
	}

	if migrated {
		saveLinksLocked()
	}

	return nil
}

func saveLinksLocked() error {
	path, err := getLinksPath()
	if err != nil {
		return err
	}

	var links []SessionLink
	for _, v := range sessionLinks {
		links = append(links, v)
	}

	data, err := json.MarshalIndent(links, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}

	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	f.Close()

	return os.Rename(tmpPath, path)
}

func LinkSession(link SessionLink, reg *mux.Registry) error {
	link.SessionID = mux.MigrateLegacyID(link.SessionID)

	// First fetch the session to get the current title and avoid stale mismatches
	sess, err := reg.FindSession(context.Background(), link.SessionID)
	if err == nil {
		link.SessionTitle = sess.Title()
	}

	linkerMu.Lock()
	sessionLinks[link.SessionID] = link
	err = saveLinksLocked()
	linkerMu.Unlock()

	if err != nil {
		return err
	}

	// Lock-free cache clearing
	ClearTelemetryCache(link.SessionID)
	models.ClearEvents(link.SessionID)
	return nil
}

func UnlinkSession(sessionID string, reg *mux.Registry) error {
	sessionID = mux.MigrateLegacyID(sessionID)

	linkerMu.Lock()
	delete(sessionLinks, sessionID)
	err := saveLinksLocked()
	linkerMu.Unlock()

	if err != nil {
		return err
	}

	// Lock-free cache clearing
	ClearTelemetryCache(sessionID)
	models.ClearEvents(sessionID)
	return nil
}

func GetLink(sessionID string, reg *mux.Registry) (SessionLink, bool) {
	sessionID = mux.MigrateLegacyID(sessionID)

	linkerMu.RLock()
	defer linkerMu.RUnlock()
	l, ok := sessionLinks[sessionID]
	// Check if we need to evaluate staleness dynamically
	if ok && !l.Stale {
		sess, err := reg.FindSession(context.Background(), sessionID)
		if err == nil && sess.Title() != l.SessionTitle {
			l.Stale = true
		}
	}
	return l, ok
}

func GetAllLinks() []SessionLink {
	linkerMu.RLock()
	defer linkerMu.RUnlock()
	var res []SessionLink
	for _, l := range sessionLinks {
		res = append(res, l)
	}
	return res
}

func HandleLinksAPI(w http.ResponseWriter, r *http.Request) {
	reg, _ := RegistryFromContext(r.Context())
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		links := GetAllLinks()
		json.NewEncoder(w).Encode(links)
		return
	} else if r.Method == http.MethodPost {
		// Enforce size limit
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
		// Security: prevent symlink traversal
		if strings.Contains(link.ExternalSessionID, "/") || strings.Contains(link.ExternalSessionID, "\\") || strings.Contains(link.ExternalSessionID, "..") {
			http.Error(w, "invalid uuid format", http.StatusBadRequest)
			return
		}

		if err := LinkSession(link, reg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "linked"})
		return
	} else if r.Method == http.MethodDelete {
		sessionID := r.URL.Query().Get("session")
		if sessionID == "" {
			http.Error(w, "missing session parameter", http.StatusBadRequest)
			return
		}
		if err := UnlinkSession(sessionID, reg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "unlinked"})
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
