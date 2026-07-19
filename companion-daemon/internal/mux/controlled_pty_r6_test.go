package mux

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
)

// ── PA2c-R6 production controlledPTYAdapter tests ──
//
// The nil-in-production testCloseBarrier seam gates the Close/process/PTY
// I/O boundary deterministically.  Sessions are hand-seeded into the adapter
// map with nil-safe native handles that model Close without spawning a real
// OS process, so the adapter's lock, identity comparison, detach primitive,
// and Close-after-unlock ordering are exercised on the REAL production
// adapter.

// newTestNative returns a *NativeSession whose Close() succeeds without
// panicking.  A real os.Pipe fd pair serves as the PTY; a minimal os.Process
// satisfies the nil-guard in Close.
func newTestNative() *NativeSession {
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	r.Close()
	return &NativeSession{
		Cmd: &exec.Cmd{Process: &os.Process{Pid: -1}},
		PTY: w,
	}
}

// seedSession inserts a session into the adapter map with the given local id
// and a valid NativeSession. Returns the exact *controlledPTYSession
// pointer for identity comparison.
func seedSession(adapter *controlledPTYAdapter, localID, title string) *controlledPTYSession {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	s := &controlledPTYSession{id: localID, title: title, native: newTestNative()}
	adapter.sessions[localID] = s
	return s
}

// TestPA2cR6_ProductionCompareAndTerminate_MatchTerminates: the PRODUCTION
// adapter's CompareAndTerminate matches the exact session pointer under
// a.mu, detaches, releases the lock, calls Close, and returns nil.
func TestPA2cR6_ProductionCompareAndTerminate_MatchTerminates(t *testing.T) {
	adapter := &controlledPTYAdapter{sessions: map[string]*controlledPTYSession{}}
	reg := MustNewRegistry(adapter)
	sess := seedSession(adapter, "s1", "test")

	var closed int32
	origBarrier := testCloseBarrier
	testCloseBarrier = func() { atomic.AddInt32(&closed, 1) }
	defer func() { testCloseBarrier = origBarrier }()

	err := reg.CompareAndTerminateSession(context.Background(), "controlled_pty:s1", sess)
	if err != nil {
		t.Fatalf("CompareAndTerminateSession(match): %v", err)
	}
	if atomic.LoadInt32(&closed) != 1 {
		t.Fatalf("Close barrier not called")
	}
	// Session detached: not in map.
	adapter.mu.Lock()
	_, ok := adapter.sessions["s1"]
	adapter.mu.Unlock()
	if ok {
		t.Fatal("session still in adapter map after match-terminate")
	}
}

// TestPA2cR6_DetachBeforeClose_ReplacementSurvives: the PRODUCTION
// CompareAndTerminate acquires the adapter lock, matches the session,
// detaches under the lock, releases the lock, and blocks in the Close
// barrier.  While blocked, a same-id replacement is inserted.  The
// replacement survives after Close completes.
func TestPA2cR6_DetachBeforeClose_ReplacementSurvives(t *testing.T) {
	adapter := &controlledPTYAdapter{sessions: map[string]*controlledPTYSession{}}
	reg := MustNewRegistry(adapter)
	sess := seedSession(adapter, "s1", "original")

	closeCalled := make(chan struct{})
	closeFinished := make(chan struct{})
	var closeCount int32
	origBarrier := testCloseBarrier
	testCloseBarrier = func() {
		atomic.AddInt32(&closeCount, 1)
		close(closeCalled)
		<-closeFinished
	}
	defer func() { testCloseBarrier = origBarrier }()

	termDone := make(chan error, 1)
	go func() {
		termDone <- reg.CompareAndTerminateSession(context.Background(), "controlled_pty:s1", sess)
	}()

	// Wait for Close barrier — proves detach completed under lock and lock released.
	<-closeCalled

	// Inject replacement while Close is blocked.
	repl := seedSession(adapter, "s1", "replacement")

	close(closeFinished)
	if err := <-termDone; err != nil {
		t.Fatalf("CompareAndTerminateSession(original) = %v, want nil", err)
	}

	// Replacement survives.
	adapter.mu.Lock()
	got := adapter.sessions["s1"]
	adapter.mu.Unlock()
	if got != repl {
		t.Fatalf("replacement session not in adapter map after detach+Close")
	}
}

// TestPA2cR6_StaleIdentity_ProductionAdapterReturnsStale: replacement
// seeded before the atomic call.  CompareAndTerminate sees the replacement
// pointer (not the expected token), returns ErrStaleSessionIdentity, and
// Close is NEVER called.
func TestPA2cR6_StaleIdentity_ProductionAdapterReturnsStale(t *testing.T) {
	adapter := &controlledPTYAdapter{sessions: map[string]*controlledPTYSession{}}
	reg := MustNewRegistry(adapter)
	sessA := seedSession(adapter, "s1", "original")

	var closeCount int32
	origBarrier := testCloseBarrier
	testCloseBarrier = func() { atomic.AddInt32(&closeCount, 1) }
	defer func() { testCloseBarrier = origBarrier }()

	// Replace under the same local id.
	adapter.mu.Lock()
	delete(adapter.sessions, "s1")
	repl := &controlledPTYSession{id: "s1", title: "replacement", native: &NativeSession{}}
	adapter.sessions["s1"] = repl
	adapter.mu.Unlock()
	reg.Invalidate()

	err := reg.CompareAndTerminateSession(context.Background(), "controlled_pty:s1", sessA)
	if !errors.Is(err, ErrStaleSessionIdentity) {
		t.Fatalf("CompareAndTerminateSession(stale) = %v, want ErrStaleSessionIdentity", err)
	}
	if n := atomic.LoadInt32(&closeCount); n != 0 {
		t.Fatalf("Close barrier called %d times on stale identity, want 0", n)
	}
	// Replacement survives.
	adapter.mu.Lock()
	got := adapter.sessions["s1"]
	adapter.mu.Unlock()
	if got != repl {
		t.Fatalf("replacement terminated by stale token")
	}
}

// TestPA2cR6_KnownBad_CheckThenDelete_NegativeControl: the known-bad
// pattern (FindSession then TerminateSession by id) sees the REPLACEMENT
// pointer after cache refresh and terminates the wrong session.  The atomic
// CompareAndTerminateSession correctly rejects the stale token.
func TestPA2cR6_KnownBad_CheckThenDelete_NegativeControl(t *testing.T) {
	adapter := &controlledPTYAdapter{sessions: map[string]*controlledPTYSession{}}
	reg := MustNewRegistry(adapter)
	sessA := seedSession(adapter, "s1", "original")

	// Replace under the same local id.
	adapter.mu.Lock()
	delete(adapter.sessions, "s1")
	repl := &controlledPTYSession{id: "s1", title: "replacement", native: &NativeSession{}}
	adapter.sessions["s1"] = repl
	adapter.mu.Unlock()
	reg.Invalidate()

	// FindSession returns the REPLACEMENT (cache refreshed).
	found, err := reg.FindSession(context.Background(), "controlled_pty:s1")
	if err != nil {
		t.Fatal(err)
	}
	if found == sessA {
		t.Fatal("FindSession returned old pointer after replacement")
	}
	// The non-atomic TerminateSession (by id only) WOULD delete the
	// replacement.  The atomic path correctly rejects the stale token.
	if err := reg.CompareAndTerminateSession(context.Background(), "controlled_pty:s1", sessA); !errors.Is(err, ErrStaleSessionIdentity) {
		t.Fatalf("stale token not rejected by atomic path: %v", err)
	}
	// Replacement is still there (atomic call did not touch it).
	adapter.mu.Lock()
	got := adapter.sessions["s1"]
	adapter.mu.Unlock()
	if got != repl {
		t.Fatalf("replacement terminated by atomic call on stale token")
	}
}
