package term

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
)

// ── BLOCKER 1: legacy query DELETE must not bypass the M2 contract ──

func TestLifecycle_LegacyQueryDelete_ManagedRunning_Rejected(t *testing.T) {
	a := newLCAdapter("controlled_pty", true)
	id := a.add("m1")
	svc := lcService(t, a)
	svc.OwnedPTY().RegisterForTest(id, "", "n", nil)
	svc.activity.Append(ActivityEvent{SessionID: id, Type: ActivityTerminalOutput, Text: "history"})

	h := &Handlers{Registry: svc.OwnedPTY().reg, Activity: svc.activity, Lifecycle: svc}
	// DELETE /api/sessions?id=<managed running> via the legacy handler.
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions?id="+id, nil)
	rr := httptest.NewRecorder()
	h.HandleSessionsAPI(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("legacy delete of running managed session status = %d, want 409 (body=%s)", rr.Code, rr.Body.String())
	}
	// Session and its history must be preserved (not terminated/cleared).
	if e, ok := svc.OwnedPTY().Get(id); !ok || e.State.Terminal() {
		t.Fatalf("catalog after rejected legacy delete = %+v ok=%v, want running preserved", e, ok)
	}
	if got := svc.activity.List(id); len(got) == 0 {
		t.Fatalf("activity history cleared by a rejected legacy delete")
	}
}

// ── BLOCKER 2: unconfirmed termination must not report success/finalize ──

type stuckSession struct {
	id     string
	stream *fakeStream
}

func (s *stuckSession) ID() string          { return s.id }
func (s *stuckSession) AdapterName() string { return "controlled_pty" }
func (s *stuckSession) Title() string       { return s.id }
func (s *stuckSession) OpenStream(context.Context) (mux.TerminalStream, error) {
	return s.stream, nil
}

// TerminateGroup is a no-op: the "process" never dies, so the Recorder never
// closes — modelling a runtime that survives SIGTERM and SIGKILL.
func (s *stuckSession) TerminateGroup(bool) error { return nil }

type stuckAdapter struct{ sess *stuckSession }

func (a *stuckAdapter) Name() string { return "controlled_pty" }
func (a *stuckAdapter) ListSessions(context.Context) ([]mux.Session, error) {
	return []mux.Session{a.sess}, nil
}
func (a *stuckAdapter) TranscriptCaptureMode() mux.TranscriptCaptureMode {
	return mux.CaptureModeByteStream
}
func (a *stuckAdapter) ManagedLifecycle() bool                         { return true }
func (a *stuckAdapter) TerminateSession(context.Context, string) error { return nil }

func TestLifecycle_Stop_UnconfirmedTermination_Fails(t *testing.T) {
	// Unique local id per run: the global recorder map unregisters
	// asynchronously on stop, so a repeated run (-count=N) reusing one fixed
	// id can race the previous iteration's teardown. The runtime code under
	// test is id-agnostic; uniqueness only isolates iterations.
	localID := genLocalID("stuck")
	stuck := &stuckSession{id: localID, stream: &fakeStream{closed: make(chan struct{})}}
	reg := mux.MustNewRegistry(&stuckAdapter{sess: stuck})
	svc := NewLifecycleService(NewOwnedPTYRuntime(reg, NewActivityBuffer(10), nil), NewActivityBuffer(10), nil)
	svc.OwnedPTY().graceful = 50 * time.Millisecond
	svc.OwnedPTY().killGrace = 50 * time.Millisecond
	id := "controlled_pty:" + localID
	EnsureRecorder(id, stuck, svc.activity) // recorder on a never-EOF stream
	svc.OwnedPTY().register(id, "", "n", nil)
	t.Cleanup(func() { DeleteRecorder(id) })

	res, err := svc.Stop(context.Background(), id)
	if err != ErrLifecycleTerminateFailed {
		t.Fatalf("err = %v, want ErrLifecycleTerminateFailed", err)
	}
	if res.State == LifecycleExited || res.State == LifecycleKilled {
		t.Fatalf("state = %q, want non-terminal on unconfirmed termination", res.State)
	}
	// Runtime left intact: catalog not terminal, recorder not removed.
	if e, _ := svc.OwnedPTY().Get(id); e.State.Terminal() {
		t.Fatalf("catalog marked terminal despite unconfirmed termination: %+v", e)
	}
	if GetRecorder(id) == nil {
		t.Fatalf("recorder removed despite unconfirmed termination")
	}
}

// ── BLOCKER 3: a very fast natural exit must still converge to terminal ──

func TestLifecycle_FastNaturalExit_ConvergesTerminal(t *testing.T) {
	svc := realService(t)
	for i := 0; i < 8; i++ {
		id := realManaged(t, svc, "exit 0")
		deadline := time.Now().Add(3 * time.Second)
		for {
			if e, ok := svc.OwnedPTY().Get(id); ok && e.State.Terminal() {
				break
			}
			if time.Now().After(deadline) {
				e, _ := svc.OwnedPTY().Get(id)
				t.Fatalf("iter %d: catalog stuck non-terminal after fast exit: %+v", i, e)
			}
			time.Sleep(10 * time.Millisecond)
		}
		// Adapter internal session must be cleaned up after natural exit.
		if _, err := svc.OwnedPTY().reg.FindSession(context.Background(), id); err == nil {
			t.Fatalf("iter %d: adapter session not removed after natural exit", i)
		}
	}
}

// ── BLOCKER 4: Delete is serialized; concurrent Delete → one success ──

func TestLifecycle_ConcurrentDelete_OneSucceeds(t *testing.T) {
	a := newLCAdapter("controlled_pty", true)
	id := a.add("cd")
	svc := lcService(t, a)
	svc.OwnedPTY().RegisterForTest(id, "", "cd", nil)
	if _, err := svc.Stop(context.Background(), id); err != nil {
		t.Fatalf("stop: %v", err)
	}

	var succ, notfound, other int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch _, err := svc.Delete(context.Background(), id); err {
			case nil:
				atomic.AddInt32(&succ, 1)
			case ErrLifecycleNotFound:
				atomic.AddInt32(&notfound, 1)
			default:
				atomic.AddInt32(&other, 1)
			}
		}()
	}
	wg.Wait()
	if succ != 1 || other != 0 {
		t.Fatalf("concurrent delete: success=%d notfound=%d other=%d, want exactly 1 success, rest 404", succ, notfound, other)
	}
}

// Concurrent Stop must never return an empty state (catalog lookup miss handled).
func TestLifecycle_ConcurrentStop_StableState(t *testing.T) {
	a := newLCAdapter("controlled_pty", true)
	id := a.add("cs")
	svc := lcService(t, a)
	svc.OwnedPTY().RegisterForTest(id, "", "cs", nil)

	var wg sync.WaitGroup
	var bad int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := svc.Stop(context.Background(), id)
			if err == nil && res.State == "" {
				atomic.AddInt32(&bad, 1)
			}
		}()
	}
	wg.Wait()
	if bad != 0 {
		t.Fatalf("%d concurrent Stop responses had empty state", bad)
	}
}

// ── Adapter cleanup after Stop ──

func TestLifecycle_Stop_RemovesAdapterSession(t *testing.T) {
	svc := realService(t)
	id := realManaged(t, svc, "sleep 30")
	if _, err := svc.Stop(context.Background(), id); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := svc.OwnedPTY().reg.FindSession(context.Background(), id); err == nil {
		t.Fatalf("controlled adapter session still present after Stop")
	}
}
