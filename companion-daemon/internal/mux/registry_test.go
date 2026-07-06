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
	t.Parallel()

	r := NewRegistry(
		&dummyAdapter{
			name: "test1",
			sessions: []Session{
				&dummySession{id: "s1", adapter: "test1"},
			},
		},
	)

	// Populate cache
	r.Sessions(context.Background())

	// 1. Test canonical ID lookup
	s, err := r.FindSession(context.Background(), "test1:s1")
	if err != nil {
		t.Fatalf("expected to find test1:s1, got err: %v", err)
	}
	if s.ID() != "s1" {
		t.Errorf("expected session id s1, got %v", s.ID())
	}

	// 2. Test legacy migration
	s2, err := r.FindSession(context.Background(), "cmux:40")
	if err == nil {
		t.Errorf("expected error for non-existent migrated session, got %v", s2)
	}

	// 3. Concurrent FindSession and Adapters (deadlock detection)
	var wg sync.WaitGroup
	done := make(chan struct{})

	go func() {
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r.FindSession(context.Background(), "test1:s1")
			}()

			wg.Add(1)
			go func() {
				defer wg.Done()
				r.Adapters()
			}()
		}
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("possible deadlock detected in concurrent FindSession/Adapters")
	}
}

func TestStaleCacheOnFailure(t *testing.T) {
	t.Parallel()

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

	r := NewRegistry(cmux, tmux)

	// 1. Initial success: 2 cmux, 1 tmux
	r.Sessions(context.Background())

	cSnap, _ := r.Snapshot("cmux")
	tSnap, _ := r.Snapshot("tmux")

	if len(cSnap.Sessions) != 2 {
		t.Fatalf("expected 2 cmux sessions, got %d", len(cSnap.Sessions))
	}
	if len(tSnap.Sessions) != 1 {
		t.Fatalf("expected 1 tmux session, got %d", len(tSnap.Sessions))
	}

	// 2. Refresh fails for cmux, tmux still succeeds
	cmux.mu.Lock()
	cmux.err = fmt.Errorf("socket connection failed")
	cmux.mu.Unlock()

	refreshed, refreshErr := r.Refresh(context.Background(), "cmux", true)
	if refreshErr == nil {
		t.Fatal("expected forced refresh to return the adapter error")
	}
	if len(refreshed.Sessions) != 2 {
		t.Fatalf("expected forced refresh to return 2 stale sessions, got %d", len(refreshed.Sessions))
	}

	cmuxSnap, _ := r.Snapshot("cmux")
	tmuxSnap, _ := r.Snapshot("tmux")

	if len(cmuxSnap.Sessions) != 2 {
		t.Fatalf("expected stale cache to retain 2 cmux sessions, got %d", len(cmuxSnap.Sessions))
	}
	if cmuxSnap.LastError == nil {
		t.Fatalf("expected cmux to record LastError, got nil")
	}
	if len(tmuxSnap.Sessions) != 1 {
		t.Fatalf("tmux should be unaffected, expected 1 session, got %d", len(tmuxSnap.Sessions))
	}

	// 3. Refresh succeeds but returns 0 sessions
	cmux.mu.Lock()
	cmux.err = nil
	cmux.sessions = []Session{}
	cmux.mu.Unlock()

	r.Invalidate()
	r.Sessions(context.Background())

	cmuxSnap2, _ := r.Snapshot("cmux")

	if len(cmuxSnap2.Sessions) != 0 {
		t.Fatalf("expected cache to update to 0 cmux sessions, got %d", len(cmuxSnap2.Sessions))
	}
	if cmuxSnap2.LastError != nil {
		t.Fatalf("expected no LastError, got %v", cmuxSnap2.LastError)
	}
}

func TestRegistryIsolation(t *testing.T) {
	t.Parallel()

	r1 := NewRegistry(&dummyAdapter{name: "a1", sessions: []Session{&dummySession{id: "x", adapter: "a1"}}})
	r2 := NewRegistry(&dummyAdapter{name: "a2", sessions: []Session{&dummySession{id: "y", adapter: "a2"}}})

	r1.Sessions(context.Background())
	r2.Sessions(context.Background())

	// r1 should not see r2's sessions
	if _, err := r1.FindSession(context.Background(), "a2:y"); err == nil {
		t.Fatal("r1 should not see r2's sessions")
	}
	if _, err := r2.FindSession(context.Background(), "a1:x"); err == nil {
		t.Fatal("r2 should not see r1's sessions")
	}

	// Each should see their own
	if s, err := r1.FindSession(context.Background(), "a1:x"); err != nil {
		t.Fatalf("r1 should see its own session: %v", err)
	} else if s.ID() != "x" {
		t.Errorf("wrong session: %s", s.ID())
	}
	if s, err := r2.FindSession(context.Background(), "a2:y"); err != nil {
		t.Fatalf("r2 should see its own session: %v", err)
	} else if s.ID() != "y" {
		t.Errorf("wrong session: %s", s.ID())
	}
}
