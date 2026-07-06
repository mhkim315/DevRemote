package mux

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

type dummyAdapter struct {
	name     string
	sessions []Session
	err      error
	callCnt  int
	mu       sync.Mutex
}

func (a *dummyAdapter) Name() string { return a.name }
func (a *dummyAdapter) ListSessions() ([]Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.callCnt++
	if a.err != nil {
		return nil, a.err
	}
	return a.sessions, nil
}
func (a *dummyAdapter) GetSession(id string) (Session, error) { return nil, nil }
func (a *dummyAdapter) CreateSession(ctx context.Context, opts CreateOptions) (string, error) {
	return "", nil
}
func (a *dummyAdapter) TerminateSession(ctx context.Context, id string) error { return nil }

type dummySession struct {
	id      string
	adapter string
}

func (s *dummySession) ID() string          { return s.id }
func (s *dummySession) AdapterName() string { return s.adapter }
func (s *dummySession) Title() string       { return "dummy" }

func TestRegistryDeadlockAndCache(t *testing.T) {
	// Backup global state
	adaptersMu.Lock()
	oldAdapters := adapters
	adapters = make(map[string]Adapter)
	adaptersMu.Unlock()

	sessionsCacheMu.Lock()
	oldSnapshots := snapshots
	snapshots = make(map[string]AdapterSnapshot)
	sessionsCacheMu.Unlock()

	t.Cleanup(func() {
		adaptersMu.Lock()
		adapters = oldAdapters
		adaptersMu.Unlock()

		sessionsCacheMu.Lock()
		snapshots = oldSnapshots
		sessionsCacheMu.Unlock()
	})

	InvalidateCache()

	adapter1 := &dummyAdapter{
		name: "test1",
		sessions: []Session{
			&dummySession{id: "s1", adapter: "test1"},
		},
	}
	RegisterAdapter(adapter1)

	// Ensure the cache is populated
	GetAllSessionsCached()

	// 1. Test canonical ID lookup
	s, err := FindSession("test1:s1")
	if err != nil {
		t.Fatalf("expected to find test1:s1, got err: %v", err)
	}
	if s.ID() != "s1" {
		t.Errorf("expected session id s1, got %v", s.ID())
	}

	// 2. Test legacy migration
	s2, err := FindSession("cmux:40")
	if err == nil {
		t.Errorf("expected error for non-existent migrated session, got %v", s2)
	}

	// 3. Concurrent FindSession and GetAdapters (to catch deadlock)
	var wg sync.WaitGroup
	done := make(chan struct{})

	go func() {
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				FindSession("test1:s1")
			}()

			wg.Add(1)
			go func() {
				defer wg.Done()
				GetAdapters()
			}()
		}
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("possible deadlock detected in concurrent FindSession/GetAdapters")
	}
}

func TestStaleCacheOnFailure(t *testing.T) {
	// Backup global state
	adaptersMu.Lock()
	oldAdapters := adapters
	adapters = make(map[string]Adapter)
	adaptersMu.Unlock()

	sessionsCacheMu.Lock()
	oldSnapshots := snapshots
	snapshots = make(map[string]AdapterSnapshot)
	sessionsCacheMu.Unlock()

	t.Cleanup(func() {
		adaptersMu.Lock()
		adapters = oldAdapters
		adaptersMu.Unlock()

		sessionsCacheMu.Lock()
		snapshots = oldSnapshots
		sessionsCacheMu.Unlock()
	})

	InvalidateCache()

	cmux := &dummyAdapter{
		name: "cmux",
		sessions: []Session{
			&dummySession{id: "s1", adapter: "cmux"},
			&dummySession{id: "s2", adapter: "cmux"},
		},
	}
	tmux := &dummyAdapter{
		name: "tmux",
		sessions: []Session{
			&dummySession{id: "t1", adapter: "tmux"},
		},
	}
	RegisterAdapter(cmux)
	RegisterAdapter(tmux)

	// 1. Initial success: 2 cmux, 1 tmux
	GetAllSessionsCached()

	sessionsCacheMu.RLock()
	cSnap := snapshots["cmux"]
	tSnap := snapshots["tmux"]
	sessionsCacheMu.RUnlock()

	if len(cSnap.Sessions) != 2 {
		t.Fatalf("expected 2 cmux sessions")
	}
	if len(tSnap.Sessions) != 1 {
		t.Fatalf("expected 1 tmux session")
	}

	// 2. Refresh fails for cmux, tmux still succeeds
	cmux.mu.Lock()
	cmux.err = fmt.Errorf("socket connection failed")
	cmux.mu.Unlock()

	// Need to invalidate cache so GetAllSessionsCached triggers refresh immediately
	InvalidateCache()
	GetAllSessionsCached()

	// Should still have 2 cmux sessions (stale cache retained)
	sessionsCacheMu.RLock()
	cmuxSnap := snapshots["cmux"]
	tmuxSnap := snapshots["tmux"]
	sessionsCacheMu.RUnlock()

	if len(cmuxSnap.Sessions) != 2 {
		t.Fatalf("expected stale cache to retain 2 cmux sessions, got %d", len(cmuxSnap.Sessions))
	}
	if cmuxSnap.LastError == nil {
		t.Fatalf("expected cmux to record LastError, got nil")
	}
	if len(tmuxSnap.Sessions) != 1 {
		t.Fatalf("tmux should be unaffected, expected 1 session")
	}

	// 3. Refresh succeeds but returns 0 sessions
	cmux.mu.Lock()
	cmux.err = nil
	cmux.sessions = []Session{}
	cmux.mu.Unlock()

	InvalidateCache()
	GetAllSessionsCached()

	// Should now update to 0 cmux sessions
	sessionsCacheMu.RLock()
	cmuxSnap2 := snapshots["cmux"]
	sessionsCacheMu.RUnlock()

	if len(cmuxSnap2.Sessions) != 0 {
		t.Fatalf("expected cache to update to 0 cmux sessions, got %d", len(cmuxSnap2.Sessions))
	}
	if cmuxSnap2.LastError != nil {
		t.Fatalf("expected no LastError")
	}
}
