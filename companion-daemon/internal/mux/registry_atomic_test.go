package mux

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// ── PA2c-R4 atomic CompareAndTerminateSession tests ──
//
// The R3 sleep-based concurrent test is replaced with deterministic barrier
// tests that use channel coordination (no time.Sleep), covering the two
// relevant lock orderings at the adapter's CompareAndTerminate boundary and
// at the Registry level.

func TestPA2cR4_AtomicTerminate_MatchTerminates(t *testing.T) {
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

func TestPA2cR4_AtomicTerminate_StaleIdentity_ReplacementSurvives(t *testing.T) {
	fa := NewFixtureAdapter()
	reg := MustNewRegistry(fa)
	canon, sessA := createViaRegistry(t, reg, fa, "s1")
	if err := reg.TerminateSession(context.Background(), "fixture", "s1"); err != nil {
		t.Fatal(err)
	}
	_, sessB := createViaRegistry(t, reg, fa, "s1")
	if err := reg.CompareAndTerminateSession(context.Background(), canon, sessA); !errors.Is(err, ErrStaleSessionIdentity) {
		t.Fatalf("CompareAndTerminateSession(stale) = %v, want ErrStaleSessionIdentity", err)
	}
	got, err := reg.FindSession(context.Background(), canon)
	if err != nil {
		t.Fatalf("FindSession(replacement) = %v, want alive", err)
	}
	if got != sessB {
		t.Fatalf("replacement instance %p terminated by stale token (want %p alive)", got, sessB)
	}
}

func TestPA2cR4_AtomicTerminate_NotFound(t *testing.T) {
	fa := NewFixtureAdapter()
	reg := MustNewRegistry(fa)
	orphan := &fixtureSession{id: "ghost", adapterName: "fixture", title: "ghost"}
	if err := reg.CompareAndTerminateSession(context.Background(), "fixture:ghost", orphan); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("CompareAndTerminateSession(orphan) = %v, want ErrSessionNotFound", err)
	}
}

// testBarrierAdapter: CompareAndTerminate blocks on entryCh and then on
// detachCh before proceeding, so tests can coordinate around the adapter's
// synchronization boundary deterministically.
type testBarrierAdapter struct {
	*fixtureAdapter
	entryCh   chan struct{} // adapter blocks here when CompareAndTerminate is entered
	detachCh  chan struct{} // adapter blocks here BEFORE the session-map write
	termCalls int32
}

func (a *testBarrierAdapter) CompareAndTerminate(ctx context.Context, localID string, expected Session) error {
	atomic.AddInt32(&a.termCalls, 1)
	if a.entryCh != nil {
		<-a.entryCh
	}
	if a.detachCh != nil {
		<-a.detachCh
	}
	return a.fixtureAdapter.CompareAndTerminate(ctx, localID, expected)
}

// TestPA2cR4_Barrier_ReplacementInsertedAtDecisivePoint_Survives:
// the atomic call enters the adapter and blocks BEFORE the adapter lock;
// the replacement is registered; the atomic call is unblocked and proceeds
// to the identity comparison — which sees the REPLACEMENT, not the stale
// token — and rejects with ErrStaleSessionIdentity.  The replacement
// survives entirely untouched.
func TestPA2cR4_Barrier_ReplacementInsertedAtDecisivePoint_Survives(t *testing.T) {
	fa := NewFixtureAdapter()
	inner := fa.(*fixtureAdapter)
	entryCh := make(chan struct{})
	barrier := &testBarrierAdapter{fixtureAdapter: inner, entryCh: entryCh}
	reg := MustNewRegistry(barrier)
	canon, sessA := createViaRegistry(t, reg, barrier, "s1")

	termDone := make(chan error, 1)
	go func() {
		termDone <- reg.CompareAndTerminateSession(context.Background(), canon, sessA)
	}()

	// Replacement is inserted BEFORE the adapter lock is acquired — same
	// canonical id, different session pointer.
	if err := reg.TerminateSession(context.Background(), "fixture", "s1"); err != nil {
		t.Fatal(err)
	}
	_, sessB := createViaRegistry(t, reg, barrier, "s1")
	close(entryCh) // release the atomic call

	err := <-termDone
	if !errors.Is(err, ErrStaleSessionIdentity) {
		t.Fatalf("CompareAndTerminateSession = %v, want ErrStaleSessionIdentity", err)
	}
	got, ferr := reg.FindSession(context.Background(), canon)
	if ferr != nil {
		t.Fatalf("replacement not found: %v", ferr)
	}
	if got != sessB {
		t.Fatalf("replacement instance %p terminated by stale token", got)
	}
}

// TestPA2cR4_Barrier_OriginalCleanupCompletes: the atomic call matches the
// original session BEFORE any replacement; the adapter lock is entered
// with the original still present; the decisive comparison passes and the
// session is detached.  The adapter's CompareAndTerminate completes
// successfully, and the session is gone afterwards.
func TestPA2cR4_Barrier_OriginalCleanupCompletes(t *testing.T) {
	fa := NewFixtureAdapter()
	inner := fa.(*fixtureAdapter)
	detachCh := make(chan struct{})
	barrier := &testBarrierAdapter{fixtureAdapter: inner, detachCh: detachCh}
	reg := MustNewRegistry(barrier)
	canon, sessA := createViaRegistry(t, reg, barrier, "s1")

	termDone := make(chan error, 1)
	go func() {
		termDone <- reg.CompareAndTerminateSession(context.Background(), canon, sessA)
	}()

	// No replacement — the original session is still the only entry.
	close(detachCh) // release the adapter

	if err := <-termDone; err != nil {
		t.Fatalf("CompareAndTerminateSession(original) = %v, want nil", err)
	}
	if _, err := reg.FindSession(context.Background(), canon); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("session not removed after match-terminate: %v", err)
	}
}

// TestPA2cR4_NonImplementingAdapter_FailsClosed: an adapter that does NOT
// implement SessionIdentityTerminator gets ErrUnsupported from the
// Registry — no non-atomic fallback exists (R4 finding 1).
func TestPA2cR4_NonImplementingAdapter_FailsClosed(t *testing.T) {
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

var _ SessionCreator = (*fixtureAdapter)(nil)
var _ SessionTerminator = (*fixtureAdapter)(nil)
var _ SessionIdentityTerminator = (*fixtureAdapter)(nil)

func createViaRegistry(t *testing.T, reg *Registry, fa Adapter, localID string) (string, Session) {
	t.Helper()
	created, err := reg.CreateSession(context.Background(), fa.Name(), CreateOptions{Name: localID})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	canon := fa.Name() + ":" + created
	sess, err := reg.FindSession(context.Background(), canon)
	if err != nil {
		t.Fatalf("FindSession after create: %v", err)
	}
	return canon, sess
}
