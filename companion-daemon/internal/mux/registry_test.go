package mux

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

type dummyAdapter struct {
	name            string
	sessions        []Session
	err             error
	callCnt         int
	createID        string
	createErr       error
	createCalls     int
	terminateErr    error
	terminateCalls  int
	lastCreateOpts  CreateOptions
	lastTerminateID string
	listStarted     chan struct{}
	listRelease     chan struct{}
	mu              sync.Mutex
}

func (a *dummyAdapter) Name() string { return a.name }
func (a *dummyAdapter) ListSessions(ctx context.Context) ([]Session, error) {
	if a.listStarted != nil {
		select {
		case a.listStarted <- struct{}{}:
		default:
		}
	}
	if a.listRelease != nil {
		<-a.listRelease
	}
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
	a.mu.Lock()
	defer a.mu.Unlock()
	a.createCalls++
	a.lastCreateOpts = opts
	return a.createID, a.createErr
}
func (a *dummyAdapter) TerminateSession(ctx context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.terminateCalls++
	a.lastTerminateID = id
	return a.terminateErr
}

type dummySession struct {
	id      string
	adapter string
}

func (s *dummySession) ID() string          { return s.id }
func (s *dummySession) AdapterName() string { return s.adapter }
func (s *dummySession) Title() string       { return "dummy" }

func TestRegistryDeadlockAndCache(t *testing.T) {
	t.Parallel()

	r := MustNewRegistry(
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

	r := MustNewRegistry(cmux, tmux)

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

func TestRegistrySessionsStableOrder(t *testing.T) {
	t.Parallel()

	registry := MustNewRegistry(
		&dummyAdapter{
			name: "tmux",
			sessions: []Session{
				&dummySession{id: "zeta", adapter: "tmux"},
				&dummySession{id: "alpha", adapter: "tmux"},
			},
		},
		&dummyAdapter{
			name: "cmux",
			sessions: []Session{
				&dummySession{id: "surface:9", adapter: "cmux"},
				&dummySession{id: "surface:1", adapter: "cmux"},
			},
		},
	)

	sessions := registry.Sessions(context.Background())
	got := make([]string, 0, len(sessions))
	for _, s := range sessions {
		got = append(got, s.AdapterName()+":"+s.ID())
	}
	want := []string{"cmux:surface:1", "cmux:surface:9", "tmux:alpha", "tmux:zeta"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sessions order = %v, want %v", got, want)
	}
}

func TestRegistryIsolation(t *testing.T) {
	t.Parallel()

	r1 := MustNewRegistry(&dummyAdapter{name: "a1", sessions: []Session{&dummySession{id: "x", adapter: "a1"}}})
	r2 := MustNewRegistry(&dummyAdapter{name: "a2", sessions: []Session{&dummySession{id: "y", adapter: "a2"}}})

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

func TestRegistryCreateSessionInvalidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		createErr       error
		wantInvalidated bool
	}{
		{name: "success invalidates", wantInvalidated: true},
		{name: "failure preserves cache", createErr: errors.New("create failed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &dummyAdapter{
				name:      "fake",
				createID:  "created",
				createErr: tt.createErr,
				sessions:  []Session{&dummySession{id: "existing", adapter: "fake"}},
			}
			registry := MustNewRegistry(adapter)
			registry.Sessions(context.Background())

			before, ok := registry.Snapshot("fake")
			if !ok || before.LastAttemptAt.IsZero() {
				t.Fatal("expected populated snapshot before mutation")
			}

			opts := CreateOptions{Name: "new-session", WorkspaceID: "workspace:1"}
			id, err := registry.CreateSession(context.Background(), "fake", opts)
			if !errors.Is(err, tt.createErr) {
				t.Fatalf("CreateSession error = %v, want %v", err, tt.createErr)
			}
			if tt.createErr == nil && id != "created" {
				t.Fatalf("created ID = %q, want created", id)
			}

			after, _ := registry.Snapshot("fake")
			if after.LastAttemptAt.IsZero() != tt.wantInvalidated {
				t.Fatalf("LastAttemptAt zero = %v, want %v", after.LastAttemptAt.IsZero(), tt.wantInvalidated)
			}
			if adapter.createCalls != 1 || !reflect.DeepEqual(adapter.lastCreateOpts, opts) {
				t.Fatalf("unexpected create call: count=%d opts=%+v", adapter.createCalls, adapter.lastCreateOpts)
			}
		})
	}
}

func TestRegistryTerminateSessionInvalidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		terminateErr    error
		wantInvalidated bool
	}{
		{name: "success invalidates", wantInvalidated: true},
		{name: "failure preserves cache", terminateErr: errors.New("terminate failed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &dummyAdapter{
				name:         "fake",
				terminateErr: tt.terminateErr,
				sessions:     []Session{&dummySession{id: "existing", adapter: "fake"}},
			}
			registry := MustNewRegistry(adapter)
			registry.Sessions(context.Background())

			err := registry.TerminateSession(context.Background(), "fake", "existing")
			if !errors.Is(err, tt.terminateErr) {
				t.Fatalf("TerminateSession error = %v, want %v", err, tt.terminateErr)
			}

			after, _ := registry.Snapshot("fake")
			if after.LastAttemptAt.IsZero() != tt.wantInvalidated {
				t.Fatalf("LastAttemptAt zero = %v, want %v", after.LastAttemptAt.IsZero(), tt.wantInvalidated)
			}
			if adapter.terminateCalls != 1 || adapter.lastTerminateID != "existing" {
				t.Fatalf("unexpected terminate call: count=%d id=%q", adapter.terminateCalls, adapter.lastTerminateID)
			}
		})
	}
}

func TestRegistryFindSessionPropagatesCancellation(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	adapter := &dummyAdapter{
		name:        "blocking",
		listStarted: started,
		listRelease: release,
	}
	registry := MustNewRegistry(adapter)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := registry.FindSession(ctx, "blocking:missing")
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("adapter refresh did not start")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("FindSession error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("FindSession did not return after cancellation")
	}
	close(release)
}
