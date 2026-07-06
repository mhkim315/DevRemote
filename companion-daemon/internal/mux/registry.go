package mux

import (
	"fmt"
	"regexp"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
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

	// Try the cache first (without triggering a refresh)
	if s, err := FindSessionInCache(id); err == nil {
		return s, nil
	}

	// If not found, trigger a refresh to ensure we have the latest
	GetAllSessionsCached()

	// Try one more time
	if s, err := FindSessionInCache(id); err == nil {
		return s, nil
	}

	return nil, fmt.Errorf("session %s not found in any adapter", id)
}

// GetAllSessions returns all sessions from all registered adapters.

type AdapterSnapshot struct {
	Sessions      []Session
	LastSuccessAt time.Time
	LastAttemptAt time.Time
	LastError     error
}

var (
	snapshots       = make(map[string]AdapterSnapshot)
	sessionsCacheMu sync.RWMutex
	refreshGroup    singleflight.Group
)

// InvalidateCache forcefully expires the cache so the next call triggers a refresh,
// while preserving the stale data in case the refresh fails.
func InvalidateCache() {
	sessionsCacheMu.Lock()
	defer sessionsCacheMu.Unlock()
	for k, snap := range snapshots {
		snap.LastAttemptAt = time.Time{}
		snapshots[k] = snap
	}
}

// GetAllSessionsCached returns sessions from all adapters, refreshing at most every 10s.
func GetAllSessionsCached() []Session {
	var all []Session
	seen := make(map[string]bool)

	for _, adapter := range GetAdapters() {
		name := adapter.Name()
		// Use singleflight to deduplicate concurrent refresh requests for the same adapter
		res, err, _ := refreshGroup.Do(name, func() (interface{}, error) {
			var needsRefresh bool
			sessionsCacheMu.RLock()
			snap, exists := snapshots[name]
			if !exists || time.Since(snap.LastAttemptAt) > 10*time.Second {
				needsRefresh = true
			}
			sessionsCacheMu.RUnlock()

			if !needsRefresh {
				return snap.Sessions, nil
			}

			// Lock-free discovery execution
			sessions, adErr := adapter.ListSessions()

			// Write lock only for state update
			sessionsCacheMu.Lock()
			currentSnap := snapshots[name]
			currentSnap.LastAttemptAt = time.Now()
			currentSnap.LastError = adErr

			if adErr == nil {
				currentSnap.Sessions = sessions
				currentSnap.LastSuccessAt = time.Now()
			} else {
				// Keep stale Sessions if err != nil
				fmt.Printf("registry: adapter %s refresh failed (retaining stale cache): %v\n", name, adErr)
			}
			snapshots[name] = currentSnap
			sessionsCacheMu.Unlock()

			return currentSnap.Sessions, nil
		})

		if err != nil {
			fmt.Printf("registry: singleflight err: %v\n", err)
			continue
		}

		sessions := res.([]Session)
		for _, s := range sessions {
			key := s.AdapterName() + ":" + s.ID()
			if !seen[key] {
				seen[key] = true
				all = append(all, s)
			}
		}
	}

	return all
}

// FindSessionInCache specifically looks for a session in the existing cache
// without triggering a refresh or calling adapter.GetSession.
func FindSessionInCache(id string) (Session, error) {
	id = MigrateLegacyID(id)

	sessionsCacheMu.RLock()
	defer sessionsCacheMu.RUnlock()
	for _, snap := range snapshots {
		for _, s := range snap.Sessions {
			key := s.AdapterName() + ":" + s.ID()
			if key == id {
				return s, nil
			}
		}
	}
	return nil, fmt.Errorf("session %s not found in cache", id)
}
