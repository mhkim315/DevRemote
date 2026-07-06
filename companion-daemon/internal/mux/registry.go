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

// Registry owns the adapter, session, and snapshot state for all multiplexer backends.
//
// Phase 1 transitional note: a package-level Default instance is used by existing
// callers until Phase 2 provides an App-level composition root.
type Registry struct {
	adaptersMu sync.RWMutex
	adapters   map[string]Adapter

	snapshotsMu sync.RWMutex
	snapshots   map[string]AdapterSnapshot
	refresh     singleflight.Group
}

// NewRegistry creates a Registry pre-populated with the given adapters.
func NewRegistry(adapters ...Adapter) *Registry {
	r := &Registry{
		adapters:  make(map[string]Adapter),
		snapshots: make(map[string]AdapterSnapshot),
	}
	for _, a := range adapters {
		r.adapters[a.Name()] = a
	}
	return r
}

// Default is the transitional package-level registry.
// It is set by main() during startup. Tests create their own Registry.
var Default = NewRegistry()

// Register adds an adapter and immediately refreshes its session list.
func (r *Registry) Register(adapter Adapter) {
	r.adaptersMu.Lock()
	r.adapters[adapter.Name()] = adapter
	r.adaptersMu.Unlock()
}

// Adapter returns a registered adapter by name.
func (r *Registry) Adapter(name string) (Adapter, bool) {
	r.adaptersMu.RLock()
	defer r.adaptersMu.RUnlock()
	a, ok := r.adapters[name]
	return a, ok
}

// Adapters returns a snapshot of all registered adapters.
func (r *Registry) Adapters() []Adapter {
	r.adaptersMu.RLock()
	defer r.adaptersMu.RUnlock()
	list := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		list = append(list, a)
	}
	return list
}

// FindSession looks up a session by its canonical ID across all adapters.
func (r *Registry) FindSession(id string) (Session, error) {
	id = MigrateLegacyID(id)

	if s, err := r.FindSessionInCache(id); err == nil {
		return s, nil
	}

	parts := strings.SplitN(id, ":", 2)
	if len(parts) == 2 {
		r.Refresh(context.Background(), parts[0], true)
	} else {
		r.Sessions(context.Background())
	}

	if s, err := r.FindSessionInCache(id); err == nil {
		return s, nil
	}
	return nil, fmt.Errorf("session %s not found in any adapter", id)
}

// AdapterSnapshot holds a point-in-time snapshot of an adapter's sessions.
type AdapterSnapshot struct {
	Sessions      []Session
	LastSuccessAt time.Time
	LastAttemptAt time.Time
	LastError     error
}

// Invalidate marks all adapter caches as expired so the next access triggers a refresh.
func (r *Registry) Invalidate() {
	r.snapshotsMu.Lock()
	defer r.snapshotsMu.Unlock()
	for k, snap := range r.snapshots {
		snap.LastAttemptAt = time.Time{}
		r.snapshots[k] = snap
	}
}

// Refresh forces or conditionally refreshes the session list for a single adapter.
func (r *Registry) Refresh(ctx context.Context, name string, force bool) (AdapterSnapshot, error) {
	r.adaptersMu.RLock()
	adapter, exists := r.adapters[name]
	r.adaptersMu.RUnlock()

	if !exists {
		err := fmt.Errorf("adapter %s not found", name)
		return AdapterSnapshot{LastError: err}, err
	}

	res, err, _ := r.refresh.Do(name, func() (interface{}, error) {
		var needsRefresh bool
		r.snapshotsMu.RLock()
		snap, hasSnap := r.snapshots[name]
		if force || !hasSnap || time.Since(snap.LastAttemptAt) > 10*time.Second {
			needsRefresh = true
		}
		r.snapshotsMu.RUnlock()

		if !needsRefresh {
			return deepCopySnapshot(snap), nil
		}

		sessions, adErr := adapter.ListSessions()

		r.snapshotsMu.Lock()
		currentSnap := r.snapshots[name]
		currentSnap.LastAttemptAt = time.Now()
		currentSnap.LastError = adErr

		if adErr == nil {
			currentSnap.Sessions = sessions
			currentSnap.LastSuccessAt = time.Now()
		} else {
			fmt.Printf("registry: adapter %s refresh failed (retaining stale cache): %v\n", name, adErr)
		}
		r.snapshots[name] = currentSnap
		r.snapshotsMu.Unlock()

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

// Sessions returns the full set of sessions from all adapters.
func (r *Registry) Sessions(ctx context.Context) []Session {
	var all []Session
	seen := make(map[string]bool)

	for _, adapter := range r.Adapters() {
		snap, _ := r.Refresh(ctx, adapter.Name(), false)
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

// Snapshot returns a copy of the cached snapshot for an adapter.
func (r *Registry) Snapshot(name string) (AdapterSnapshot, bool) {
	r.snapshotsMu.RLock()
	snap, exists := r.snapshots[name]
	r.snapshotsMu.RUnlock()
	if !exists {
		return AdapterSnapshot{}, false
	}
	return deepCopySnapshot(snap), true
}

// FindSessionInCache searches the snapshot cache without refreshing.
func (r *Registry) FindSessionInCache(id string) (Session, error) {
	id = MigrateLegacyID(id)

	r.snapshotsMu.RLock()
	defer r.snapshotsMu.RUnlock()
	for _, snap := range r.snapshots {
		for _, s := range snap.Sessions {
			key := s.AdapterName() + ":" + s.ID()
			if key == id {
				return s, nil
			}
		}
	}
	return nil, fmt.Errorf("session %s not found in cache", id)
}

// ── Legacy ID migration (pure function, not stateful) ──

var legacyCmuxRe = regexp.MustCompile(`^cmux:(\d+)$`)

// MigrateLegacyID converts old cmux:38 formats to cmux:surface:38
func MigrateLegacyID(id string) string {
	if m := legacyCmuxRe.FindStringSubmatch(id); m != nil {
		return "cmux:surface:" + m[1]
	}
	return id
}

// ── Internal helpers ──

func deepCopySnapshot(snap AdapterSnapshot) AdapterSnapshot {
	copySnap := snap
	if snap.Sessions != nil {
		copySnap.Sessions = make([]Session, len(snap.Sessions))
		copy(copySnap.Sessions, snap.Sessions)
	}
	return copySnap
}
