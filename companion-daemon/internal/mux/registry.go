package mux

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

var legacyCmuxRe = regexp.MustCompile(`^cmux:(\d+)$`)

var (
	adapters   = make(map[string]Adapter)
	adaptersMu sync.RWMutex
)

// RegisterAdapter adds a new session adapter to the registry.
func RegisterAdapter(a Adapter) {
	adaptersMu.Lock()
	defer adaptersMu.Unlock()
	adapters[a.Name()] = a
}

// GetAdapter retrieves an adapter by name
func GetAdapter(name string) (Adapter, bool) {
	adaptersMu.RLock()
	defer adaptersMu.RUnlock()
	a, ok := adapters[name]
	return a, ok
}

// GetAdapters returns a list of all registered adapters.
func GetAdapters() []Adapter {
	adaptersMu.RLock()
	defer adaptersMu.RUnlock()
	var list []Adapter
	for _, a := range adapters {
		list = append(list, a)
	}
	return list
}

// MigrateLegacyID converts old cmux:38 formats to cmux:surface:38
func MigrateLegacyID(id string) string {
	if m := legacyCmuxRe.FindStringSubmatch(id); m != nil {
		return "cmux:surface:" + m[1]
	}
	return id
}

func FindSession(id string) (Session, error) {
	id = MigrateLegacyID(id)

	adaptersMu.RLock()
	defer adaptersMu.RUnlock()
	
	parts := strings.SplitN(id, ":", 2)
	if len(parts) == 2 {
		adapterName := parts[0]
		rawID := parts[1]
		if a, ok := adapters[adapterName]; ok {
			s, err := a.GetSession(rawID)
			if err == nil && s != nil {
				return s, nil
			}
			return nil, fmt.Errorf("session %s not found in adapter %s", rawID, adapterName)
		}
	}

	// Fallback to searching all adapters if no prefix is given (for backward compatibility)
	for _, a := range adapters {
		s, err := a.GetSession(id)
		if err == nil && s != nil {
			return s, nil
		}
	}
	return nil, fmt.Errorf("session %s not found in any adapter", id)
}

// GetAllSessions returns all sessions from all registered adapters.

var (
	cachedSessions   []Session
	cachedSessionsAt time.Time
	sessionsCacheMu  sync.RWMutex
)

// InvalidateCache forcefully clears the session list cache
func InvalidateCache() {
	sessionsCacheMu.Lock()
	cachedSessions = nil
	cachedSessionsAt = time.Time{}
	sessionsCacheMu.Unlock()
}

// GetAllSessionsCached returns sessions from all adapters, refreshing at most every 10s.
func GetAllSessionsCached() []Session {
	sessionsCacheMu.RLock()
	if time.Since(cachedSessionsAt) < 10*time.Second && cachedSessions != nil {
		result := cachedSessions
		sessionsCacheMu.RUnlock()
		return result
	}
	sessionsCacheMu.RUnlock()

	// Refresh cache
	sessionsCacheMu.Lock()
	defer sessionsCacheMu.Unlock()

	// Double-check
	if time.Since(cachedSessionsAt) < 30*time.Second && cachedSessions != nil {
		return cachedSessions
	}

	var all []Session
	seen := make(map[string]bool)
	for _, a := range adapters {
		sessions, err := a.ListSessions()
		if err == nil {
			for _, s := range sessions {
				key := s.AdapterName() + ":" + s.ID()
				if !seen[key] {
					seen[key] = true
					all = append(all, s)
				}
			}
		}
	}
	cachedSessions = all
	cachedSessionsAt = time.Now()
	return all
}

