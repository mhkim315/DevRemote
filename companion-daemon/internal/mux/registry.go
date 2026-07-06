package mux

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Registry owns the adapter, session, and snapshot state for all multiplexer backends.
type Registry struct {
	adaptersMu sync.RWMutex
	adapters   map[string]Adapter

	snapshotsMu sync.RWMutex
	snapshots   map[string]AdapterSnapshot
	refresh     singleflight.Group
}

// NewRegistry creates a Registry pre-populated with the given adapters.
// NewRegistry creates a Registry. Returns error on duplicate or invalid adapter names.
func NewRegistry(adapters ...Adapter) (*Registry, error) {
	r := &Registry{
		adapters:  make(map[string]Adapter),
		snapshots: make(map[string]AdapterSnapshot),
	}
	for _, a := range adapters {
		if err := r.Register(a); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// MustNewRegistry is like NewRegistry but panics on error. For tests.
func MustNewRegistry(adapters ...Adapter) *Registry {
	r, err := NewRegistry(adapters...)
	if err != nil {
		panic(fmt.Sprintf("MustNewRegistry: %v", err))
	}
	return r
}

// Register adds an adapter. Checks name validity, rejects duplicates.
func (r *Registry) Register(adapter Adapter) error {
	name := adapter.Name()
	if err := ValidateAdapterName(name); err != nil {
		return err
	}
	r.adaptersMu.Lock()
	defer r.adaptersMu.Unlock()
	if _, exists := r.adapters[name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateAdapter, name)
	}
	r.adapters[name] = adapter
	return nil
}

// CreateSession delegates to the named adapter and invalidates the cache on success.
func (r *Registry) CreateSession(ctx context.Context, adapterName string, opts CreateOptions) (string, error) {
	adapter, ok := r.Adapter(adapterName)
	if !ok {
		return "", fmt.Errorf("%w: adapter %s", ErrAdapterUnavailable, adapterName)
	}
	creator, ok := adapter.(SessionCreator)
	if !ok {
		return "", fmt.Errorf("%w: adapter %s", ErrUnsupported, adapterName)
	}
	id, err := creator.CreateSession(ctx, opts)
	if err != nil {
		return "", err
	}
	r.Invalidate()
	return id, nil
}

// TerminateSession delegates to the named adapter and invalidates the cache on success.
func (r *Registry) TerminateSession(ctx context.Context, adapterName string, id string) error {
	adapter, ok := r.Adapter(adapterName)
	if !ok {
		return fmt.Errorf("%w: adapter %s", ErrAdapterUnavailable, adapterName)
	}
	terminator, ok := adapter.(SessionTerminator)
	if !ok {
		return fmt.Errorf("%w: adapter %s", ErrUnsupported, adapterName)
	}
	if err := terminator.TerminateSession(ctx, id); err != nil {
		return err
	}
	r.Invalidate()
	return nil
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

// FindSession looks up a session by canonical ID, force-refreshing the target
// adapter first. Returns ErrSessionNotFound if not found, ErrAdapterUnavailable
// if the adapter cannot be reached, ErrInvalidSessionID if the ID is malformed.
func (r *Registry) FindSession(ctx context.Context, id string) (Session, error) {
	id = MigrateLegacyID(id)
	ref := ParseSessionID(id)
	if err := ref.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSessionID, err)
	}
	canonical := ref.Canonical()

	if _, err := r.Refresh(ctx, ref.Adapter, true); err != nil {
		return nil, fmt.Errorf("%w: adapter %s refresh failed: %w", ErrAdapterUnavailable, ref.Adapter, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s, err := r.FindSessionInCache(canonical); err == nil {
		return s, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, canonical)
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
		err := fmt.Errorf("%w: adapter %s", ErrAdapterUnavailable, name)
		return AdapterSnapshot{LastError: err}, err
	}

	// Phase 3 deferred: singleflight shares first waiter's context across all
	// concurrent callers. If the first caller cancels, other waiters get the
	// cancelled result. Phase 3 lifecycle/concurrency will add per-caller
	// timeout isolation.
	resultCh := r.refresh.DoChan(name, func() (interface{}, error) {
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

		sessions, adErr := adapter.ListSessions(ctx)

		r.snapshotsMu.Lock()
		currentSnap := r.snapshots[name]
		currentSnap.LastAttemptAt = time.Now()
		currentSnap.LastError = adErr

		if adErr == nil {
			currentSnap.Sessions = sessions
			currentSnap.LastSuccessAt = time.Now()
		} else {
			fmt.Printf("registry: adapter %s refresh failed (retaining stale cache): %v\n", name, adErr)
			var wrapped error
			if errors.Is(adErr, context.DeadlineExceeded) {
				wrapped = fmt.Errorf("%w: %w: %w", ErrTimeout, ErrAdapterUnavailable, adErr)
			} else {
				wrapped = fmt.Errorf("%w: %w", ErrAdapterUnavailable, adErr)
			}
			currentSnap.LastError = wrapped
			adErr = wrapped
		}
		r.snapshots[name] = currentSnap
		r.snapshotsMu.Unlock()

		return deepCopySnapshot(currentSnap), adErr
	})

	select {
	case <-ctx.Done():
		snap, _ := r.Snapshot(name)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return snap, fmt.Errorf("%w: %w", ErrTimeout, ctx.Err())
		}
		return snap, ctx.Err()
	case result := <-resultCh:
		if result.Err != nil {
			if snap, ok := result.Val.(AdapterSnapshot); ok {
				return snap, result.Err
			}
			return AdapterSnapshot{LastError: result.Err}, result.Err
		}
		return result.Val.(AdapterSnapshot), nil
	}
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
	sort.SliceStable(all, func(i, j int) bool {
		left := all[i].AdapterName() + ":" + all[i].ID()
		right := all[j].AdapterName() + ":" + all[j].ID()
		return left < right
	})
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
