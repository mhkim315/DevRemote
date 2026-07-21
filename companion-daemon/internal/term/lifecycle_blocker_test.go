package term

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// ── BLOCKER 1: legacy query DELETE must not bypass the M2 contract ──

// PA3 Step 6b R1: removed t.Skip, ActivityBuffer.Append, and Activity field.
// ActivityBuffer stub (nil) does not store history; canonical Transcript
// is authoritative. The core invariant — legacy delete of running managed
// session is rejected — is preserved.
func TestLifecycle_LegacyQueryDelete_ManagedRunning_Rejected(t *testing.T) {
	id := "controlled_pty:m1"
	svc := lcService(t, newLCAdapter("controlled_pty", true))
	svc.OwnedPTY().RegisterForTest(id, "", "n", nil)

	h := &Handlers{Lifecycle: svc}
	// DELETE /api/sessions?id=<managed running> via the legacy handler.
	req := httptest.NewRequest(http.MethodDelete, "/api/sessions?id="+id, nil)
	rr := httptest.NewRecorder()
	h.HandleSessionsAPI(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("legacy delete of running managed session status = %d, want 409 (body=%s)", rr.Code, rr.Body.String())
	}
	// Session must be preserved (not terminated/cleared).
	if e, ok := svc.OwnedPTY().Get(id); !ok || e.State.Terminal() {
		t.Fatalf("catalog after rejected legacy delete = %+v ok=%v, want running preserved", e, ok)
	}
}

// ── BLOCKER 2: unconfirmed termination must not report success/finalize ──

// stuckPTYHandle models a process that accepts signals but never confirms exit.
type stuckPTYHandle struct{ stream *fakeStream }

func (h *stuckPTYHandle) Read(p []byte) (int, error)        { return h.stream.Read(p) }
func (h *stuckPTYHandle) Write(p []byte) (int, error)       { return h.stream.Write(p) }
func (*stuckPTYHandle) Resize(int, int) error               { return nil }
func (*stuckPTYHandle) CloseTransport() error               { return nil }
func (*stuckPTYHandle) Signal(syscall.Signal) SignalOutcome { return SignalOutcome{Delivered: true} }
func (*stuckPTYHandle) Kill() KillOutcome                   { return KillOutcome{Killed: true} }
func (*stuckPTYHandle) Wait(ctx context.Context) LifecycleOutcome {
	<-ctx.Done()
	return LifecycleOutcome{TimedOut: true, Err: ctx.Err()}
}

func TestLifecycle_Stop_UnconfirmedTermination_Fails(t *testing.T) {
	// Unique local id per run: the global recorder map unregisters
	// asynchronously on stop, so a repeated run (-count=N) reusing one fixed
	// id can race the previous iteration's teardown. The runtime code under
	// test is id-agnostic; uniqueness only isolates iterations.
	localID := genLocalID("stuck")
	handle := &stuckPTYHandle{stream: &fakeStream{closed: make(chan struct{})}}
	svc := NewLifecycleService(NewOwnedPTYRuntime(nil, nil), nil)
	svc.OwnedPTY().graceful = 50 * time.Millisecond
	svc.OwnedPTY().killGrace = 50 * time.Millisecond
	id := "controlled_pty:" + localID
	rec, _ := EnsureRecorder(id, func() (ptyStream, error) { return handle.stream, nil })
	o := svc.OwnedPTY()
	o.register(id, "", "n", handle, LaunchIdentity{InstanceID: localID, StartedAt: time.Now()}, func(context.Context) CleanupOutcome { return CleanupOutcome{Completed: true} }, newTerminalTransport(id, 0, handleWriter{handle}, handle, rec), rec)
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
		if e, _ := svc.OwnedPTY().Get(id); !e.State.Terminal() {
			t.Fatalf("iter %d: runtime not terminal", i)
		}
	}
}

// ── BLOCKER 4: Delete is serialized; concurrent Delete → one success ──

func TestLifecycle_ConcurrentDelete_OneSucceeds(t *testing.T) {
	id := "controlled_pty:cd"
	svc := lcService(t, newLCAdapter("controlled_pty", true))
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
	id := "controlled_pty:cs"
	svc := lcService(t, newLCAdapter("controlled_pty", true))
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
	if e, ok := svc.OwnedPTY().Get(id); !ok || !e.State.Terminal() {
		t.Fatalf("runtime not terminal after Stop: %+v", e)
	}
}
