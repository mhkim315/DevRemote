package mux

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// ── PA2c-R5 deterministic barrier tests ──

// prodShapedBarrierAdapter embeds fixtureAdapter and models the production
// controlledPTYAdapter pattern (lock → compare → detach → unlock → Close):
// the lock-acquired channel signals when CompareAndTerminate enters the
// adapter lock; postDetachCh blocks AFTER the session is deleted from the map
// and the lock is released (modelling native.Close() I/O).
type prodShapedBarrierAdapter struct {
	*fixtureAdapter
	lockAcquired chan struct{}
	postDetachCh chan struct{}
	termCalls    int32
}

func (a *prodShapedBarrierAdapter) CompareAndTerminate(ctx context.Context, localID string, expected Session) error {
	atomic.AddInt32(&a.termCalls, 1)

	// --- adapter lock held ---
	a.fixtureAdapter.mu.Lock()

	// Signal that the lock was acquired.
	if a.lockAcquired != nil {
		close(a.lockAcquired)
	}

	s, ok := a.fixtureAdapter.sessions[localID]
	if !ok {
		a.fixtureAdapter.mu.Unlock()
		return ErrSessionNotFound
	}
	if s != expected {
		a.fixtureAdapter.mu.Unlock()
		return ErrStaleSessionIdentity
	}
	delete(a.fixtureAdapter.sessions, localID)
	a.fixtureAdapter.mu.Unlock()
	// --- lock released ---

	// --- post-detach I/O barrier ---
	if a.postDetachCh != nil {
		<-a.postDetachCh
	}
	return nil
}

// TestPA2cR5_TerminationWinsLock_ReplacementSurvives:
// Ordering A: the atomic call acquires the adapter lock (proven by
// preDetachCh closing), the main goroutine blocks on that signal, then
// waits for the lock release (inner.mu.Lock()), injects a same-id
// replacement, and releases the post-detach Close-I/O barrier.  The
// replacement survives untouched.
func TestPA2cR5_TerminationWinsLock_ReplacementSurvives(t *testing.T) {
	fa := NewFixtureAdapter()
	inner := fa.(*fixtureAdapter)
	lockAcquired := make(chan struct{})
	postDetach := make(chan struct{})
	barrier := &prodShapedBarrierAdapter{fixtureAdapter: inner, lockAcquired: lockAcquired, postDetachCh: postDetach}
	reg := MustNewRegistry(barrier)
	canon, sessA := createViaRegistry(t, reg, barrier, "s1")

	termDone := make(chan error, 1)
	go func() {
		termDone <- reg.CompareAndTerminateSession(context.Background(), canon, sessA)
	}()

	// Wait for the atomic call to ACQUIRE the adapter lock (preDetachCh
	// closes inside the CompareAndTerminate lock).
	<-lockAcquired

	// Now the adapter lock is held by the atomic call.  Acquiring it here
	// blocks until the lock→compare→detach→unlock sequence completes.
	inner.mu.Lock()
	repl := &fixtureSession{id: "s1", adapterName: "fixture", title: "s1"}
	inner.sessions["s1"] = repl
	inner.mu.Unlock()
	reg.Invalidate()

	// Release the Close-I/O barrier.
	close(postDetach)
	if err := <-termDone; err != nil {
		t.Fatalf("CompareAndTerminateSession(original) = %v, want nil", err)
	}

	// Replacement survives.
	got, err := reg.FindSession(context.Background(), canon)
	if err != nil {
		t.Fatalf("replacement not found: %v", err)
	}
	if got != repl {
		t.Fatalf("replacement terminated by stale cleanup")
	}
}

// TestPA2cR5_ReplacementInstallsFirst_StaleRejected:
// The replacement registers BEFORE the atomic call enters.  The atomic
// call's identity check sees the REPLACEMENT pointer ≠ expected token →
// ErrStaleSessionIdentity.  The replacement survives.
func TestPA2cR5_ReplacementInstallsFirst_StaleRejected(t *testing.T) {
	fa := NewFixtureAdapter()
	inner := fa.(*fixtureAdapter)
	// No barrier channels — the adapter lock is the only synchronization.
	barrier := &prodShapedBarrierAdapter{fixtureAdapter: inner}
	reg := MustNewRegistry(barrier)
	canon, sessA := createViaRegistry(t, reg, barrier, "s1")

	// Delete original and install replacement BEFORE the atomic call.
	if err := reg.TerminateSession(context.Background(), "fixture", "s1"); err != nil {
		t.Fatal(err)
	}
	_, sessB := createViaRegistry(t, reg, barrier, "s1")

	err := reg.CompareAndTerminateSession(context.Background(), canon, sessA)
	if !errors.Is(err, ErrStaleSessionIdentity) {
		t.Fatalf("CompareAndTerminateSession(stale) = %v, want ErrStaleSessionIdentity", err)
	}
	got, ferr := reg.FindSession(context.Background(), canon)
	if ferr != nil {
		t.Fatalf("replacement not found: %v", ferr)
	}
	if got != sessB {
		t.Fatalf("replacement terminated by stale token")
	}
}

// ── preserved basic tests ──

func TestPA2cR5_AtomicTerminate_MatchTerminates(t *testing.T) {
	fa := NewFixtureAdapter()
	reg := MustNewRegistry(fa)
	canon, sess := createViaRegistry(t, reg, fa, "s1")
	if err := reg.CompareAndTerminateSession(context.Background(), canon, sess); err != nil {
		t.Fatalf("CompareAndTerminateSession(match): %v", err)
	}
	if _, err := reg.FindSession(context.Background(), canon); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("FindSession after match-terminate = %v, want ErrSessionNotFound", err)
	}
}

func TestPA2cR5_AtomicTerminate_NotFound(t *testing.T) {
	fa := NewFixtureAdapter()
	reg := MustNewRegistry(fa)
	orphan := &fixtureSession{id: "ghost", adapterName: "fixture", title: "ghost"}
	if err := reg.CompareAndTerminateSession(context.Background(), "fixture:ghost", orphan); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("CompareAndTerminateSession(orphan) = %v, want ErrSessionNotFound", err)
	}
}

func TestPA2cR5_NonImplementingAdapter_FailsClosed(t *testing.T) {
	reg := MustNewRegistry(&noAtomicAdapter{})
	if err := reg.CompareAndTerminateSession(context.Background(), "noatomic:1",
		&fixtureSession{id: "1", adapterName: "noatomic", title: "1"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("CompareAndTerminateSession on non-implementing adapter = %v, want ErrUnsupported", err)
	}
}

type noAtomicAdapter struct{}

func (a *noAtomicAdapter) Name() string                                        { return "noatomic" }
func (a *noAtomicAdapter) ListSessions(context.Context) ([]Session, error)     { return nil, nil }
func (a *noAtomicAdapter) TerminateSession(_ context.Context, id string) error { return nil }

// ── helpers ──

func createViaRegistry(t *testing.T, reg *Registry, fa Adapter, localID string) (string, Session) {
	t.Helper()
	canon, sess, err := createViaRegistryResult(t, reg, fa, localID)
	if err != nil {
		t.Fatalf("createViaRegistry: %v", err)
	}
	return canon, sess
}

func createViaRegistryResult(t *testing.T, reg *Registry, fa Adapter, localID string) (string, Session, error) {
	t.Helper()
	created, err := reg.CreateSession(context.Background(), fa.Name(), CreateOptions{Name: localID})
	if err != nil {
		return "", nil, err
	}
	canon := fa.Name() + ":" + created
	sess, err := reg.FindSession(context.Background(), canon)
	if err != nil {
		return "", nil, err
	}
	return canon, sess, nil
}
