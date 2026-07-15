package term

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── SP0-P2: production event pump → native status transitions ──

// pumpRecorder collects methods observed by the production pump so tests can
// deterministically wait until a specific crafted message was CONSUMED (not
// merely sent) before asserting it had no status effect.
type pumpRecorder struct {
	mu   sync.Mutex
	seen []string
	cond chan struct{}
}

func newPumpRecorder() *pumpRecorder { return &pumpRecorder{cond: make(chan struct{}, 64)} }

func (r *pumpRecorder) observe(_ string, method string) {
	r.mu.Lock()
	r.seen = append(r.seen, method)
	r.mu.Unlock()
	select {
	case r.cond <- struct{}{}:
	default:
	}
}

func (r *pumpRecorder) waitFor(t *testing.T, method string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		r.mu.Lock()
		for _, m := range r.seen {
			if m == method {
				r.mu.Unlock()
				return
			}
		}
		r.mu.Unlock()
		select {
		case <-r.cond:
		case <-deadline:
			t.Fatalf("pump never consumed %q", method)
		}
	}
}

func waitForStatus(t *testing.T, g *ManagedSessionRegistry, id string, want ManagedNativeStatus) ManagedSessionRecord {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		rec, ok := g.Get(id)
		if ok && rec.NativeStatus == want {
			return rec
		}
		if time.Now().After(deadline) {
			t.Fatalf("status never reached %s (now %+v ok=%v)", want, rec, ok)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestManagedPump_NativeEventsDriveWorkingThenCompleted: after the launch
// pipeline, the PRODUCTION pump (not a direct store call) applies exact
// native turn/started → working and turn/completed → completed transitions.
// Crafted unknown events, unmatched-thread events, and fabricated status
// fields consumed in between never change the record.
func TestManagedPump_NativeEventsDriveWorkingThenCompleted(t *testing.T) {
	const tid = "thread-P2"
	completeGate := make(chan struct{})
	fl := &fakeLauncher{handler: func(method string, id float64, params map[string]any, out func(map[string]any)) {
		switch method {
		case "initialize":
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
		case "thread/start":
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"threadId": tid}})
		case "turn/start":
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
			out(map[string]any{"jsonrpc": "2.0", "method": "turn/started", "params": map[string]any{"threadId": tid, "turnId": "turn-1"}})
			// Adversarial mid-turn noise, all of which must be inert:
			out(map[string]any{"jsonrpc": "2.0", "method": "thread/status/changed", "params": map[string]any{"threadId": tid, "status": "completed"}})
			out(map[string]any{"jsonrpc": "2.0", "method": "managed/status", "params": map[string]any{"threadId": tid, "status": "exited"}})
			out(map[string]any{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{"threadId": "thread-OTHER"}})
			go func() {
				<-completeGate
				out(map[string]any{"jsonrpc": "2.0", "method": "turn/completed", "params": map[string]any{"threadId": tid}})
			}()
		}
	}}
	managed := newTestManagedService(fl)
	rec := newPumpRecorder()
	managed.pumpObserver = rec.observe

	id, err := managed.CreateDetached("")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	waitForStatus(t, managed.Registry(), id, ManagedStatusWorking)

	// Wait until the pump has consumed every adversarial message, then prove
	// none of them changed the status (non-vacuous: the events were read).
	rec.waitFor(t, "thread/status/changed")
	rec.waitFor(t, "managed/status")
	rec.waitFor(t, "turn/completed") // the unmatched-thread one
	if got, _ := managed.Registry().Get(id); got.NativeStatus != ManagedStatusWorking {
		t.Fatalf("crafted events changed status to %s", got.NativeStatus)
	}

	close(completeGate)
	final := waitForStatus(t, managed.Registry(), id, ManagedStatusCompleted)
	if final.Exited {
		t.Fatalf("completed record marked exited: %+v", final)
	}
}

// TestManagedPump_ChildExitMarksExited_AndLateEventsInert: when the child's
// stdout ends, the pump marks the record exited and reaps the child; the
// session can never look current again.
func TestManagedPump_ChildExitMarksExited_AndLateEventsInert(t *testing.T) {
	const tid = "thread-EXIT"
	fl := &fakeLauncher{handler: happyAppServer(tid)}
	managed := newTestManagedService(fl)

	id, err := managed.CreateDetached("")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Simulate child death.
	fl.procs[0].Kill()
	rec := waitForStatus(t, managed.Registry(), id, ManagedStatusExited)
	if !rec.Exited {
		t.Fatalf("record not exited: %+v", rec)
	}
	// A same-epoch late event cannot restore a current status.
	if managed.Registry().UpdateNativeStatus(id, rec.Epoch, ManagedStatusWorking) {
		t.Fatal("late update restored an exited session")
	}
}

// TestManagedShutdown_KillsChildrenAndClosesRegistry: daemon shutdown stops
// and reaps every owned child within the bounded deadline and permanently
// closes the owned registry.
func TestManagedShutdown_KillsChildrenAndClosesRegistry(t *testing.T) {
	fl := &fakeLauncher{handler: happyAppServer("thread-SD")}
	managed := newTestManagedService(fl)

	id, err := managed.CreateDetached("")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := managed.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	select {
	case <-fl.procs[0].killed:
	case <-time.After(2 * time.Second):
		t.Fatal("owned child not killed at shutdown")
	}
	if managed.Registry().UpdateNativeStatus(id, 1, ManagedStatusWorking) {
		t.Fatal("registry writable after shutdown")
	}
	if err := managed.Registry().Register(testRecord("codex_app_server:late", 9)); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("register after shutdown err = %v", err)
	}
}
