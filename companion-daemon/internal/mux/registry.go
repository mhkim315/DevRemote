package mux

import (
	"context"
	"fmt"

	"regexp"
	"strings"
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

	// If not found, trigger a forced refresh of the relevant adapter
	parts := strings.SplitN(id, ":", 2)
	if len(parts) == 2 {
		RefreshAdapter(context.Background(), parts[0], true)
	} else {
		GetAllSessionsCached()
	}

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

// RefreshAdapter refreshes a specific adapter and returns its snapshot.
func RefreshAdapter(ctx context.Context, name string, force bool) (AdapterSnapshot, error) {
	adaptersMu.RLock()
	adapter, exists := adapters[name]
	adaptersMu.RUnlock()

	if !exists {
		return AdapterSnapshot{LastError: fmt.Errorf("adapter %s not found", name)}, fmt.Errorf("adapter %s not found", name)
	}

	res, err, _ := refreshGroup.Do(name, func() (interface{}, error) {
		var needsRefresh bool
		sessionsCacheMu.RLock()
		snap, hasSnap := snapshots[name]
		if force || !hasSnap || time.Since(snap.LastAttemptAt) > 10*time.Second {
			needsRefresh = true
		}
		sessionsCacheMu.RUnlock()

		if !needsRefresh {
			return deepCopySnapshot(snap), nil
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

		return deepCopySnapshot(currentSnap), adErr
	})

	if err != nil {
		if snap, ok := res.(AdapterSnapshot); ok {
			return snap, err
		}
		return AdapterSnapshot{LastError: err}, err
	}
	return res.(AdapterSnapshot), nil
}

// GetAllSessionsCached returns sessions from all adapters, refreshing at most every 10s.
func GetAllSessionsCached() []Session {
	var all []Session
	seen := make(map[string]bool)

	for _, adapter := range GetAdapters() {
		snap, _ := RefreshAdapter(context.Background(), adapter.Name(), false)
		for _, s := range snap.Sessions {
			key := s.AdapterName() + ":" + s.ID()
			if !seen[key] {
				seen[key] = true
				all = append(all, s)
			}
		}
	}

	return all
}

// GetAdapterSnapshot safely returns a deep copy of the current snapshot for an adapter
func GetAdapterSnapshot(adapterName string) (AdapterSnapshot, bool) {
	sessionsCacheMu.RLock()
	snap, exists := snapshots[adapterName]
	sessionsCacheMu.RUnlock()

	if !exists {
		return AdapterSnapshot{}, false
	}
	return deepCopySnapshot(snap), true
}

func deepCopySnapshot(snap AdapterSnapshot) AdapterSnapshot {
	copySnap := snap
	if snap.Sessions != nil {
		copySnap.Sessions = make([]Session, len(snap.Sessions))
		copy(copySnap.Sessions, snap.Sessions)
	}
	return copySnap
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
