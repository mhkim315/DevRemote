package mux

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPA2cR3_AtomicTerminate_MatchTerminates(t *testing.T) {
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

func TestPA2cR3_AtomicTerminate_StaleIdentity_ReplacementSurvives(t *testing.T) {
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

func TestPA2cR3_AtomicTerminate_NotFound(t *testing.T) {
	fa := NewFixtureAdapter()
	reg := MustNewRegistry(fa)
	orphan := &fixtureSession{id: "ghost", adapterName: "fixture", title: "ghost"}
	if err := reg.CompareAndTerminateSession(context.Background(), "fixture:ghost", orphan); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("CompareAndTerminateSession(orphan) = %v, want ErrSessionNotFound", err)
	}
}

type blockingTermAdapter struct {
	Adapter
	inner     Adapter
	blockCh   <-chan struct{}
	termCalls int32
}

func (a *blockingTermAdapter) CreateSession(ctx context.Context, opts CreateOptions) (string, error) {
	if sc, ok := a.inner.(SessionCreator); ok {
		return sc.CreateSession(ctx, opts)
	}
	return "", ErrUnsupported
}
func (a *blockingTermAdapter) TerminateSession(ctx context.Context, id string) error {
	atomic.AddInt32(&a.termCalls, 1)
	if a.blockCh != nil {
		<-a.blockCh
	}
	return a.inner.(SessionTerminator).TerminateSession(ctx, id)
}
func (a *blockingTermAdapter) ListSessions(ctx context.Context) ([]Session, error) {
	return a.inner.ListSessions(ctx)
}
func (a *blockingTermAdapter) CompareAndTerminate(ctx context.Context, localID string, expected Session) error {
	atomic.AddInt32(&a.termCalls, 1)
	if sit, ok := a.inner.(SessionIdentityTerminator); ok {
		if a.blockCh != nil {
			<-a.blockCh
		}
		return sit.CompareAndTerminate(ctx, localID, expected)
	}
	return ErrUnsupported
}

func TestPA2cR3_AtomicTerminate_ConcurrentDoesNotDeadlock(t *testing.T) {
	fa := NewFixtureAdapter()
	unblock := make(chan struct{})
	blocker := &blockingTermAdapter{Adapter: fa, inner: fa, blockCh: unblock}
	reg := MustNewRegistry(blocker)
	canon, sessA := createViaRegistry(t, reg, blocker, "a1")

	termDone := make(chan error, 1)
	go func() {
		termDone <- reg.CompareAndTerminateSession(context.Background(), canon, sessA)
	}()
	time.Sleep(150 * time.Millisecond)
	if atomic.LoadInt32(&blocker.termCalls) != 1 {
		t.Fatal("blocking adapter not entered")
	}
	fix := fa.(*fixtureAdapter)
	fix.mu.Lock()
	delete(fix.sessions, "a1")
	fix.mu.Unlock()
	close(unblock)

	if err := <-termDone; err != nil {
		if !errors.Is(err, ErrSessionNotFound) {
			t.Logf("post-race terminate err = %v (expected not-found or nil)", err)
		}
	}
}

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
