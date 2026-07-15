package term

import (
	"strings"
	"sync"
	"testing"
)

// ── SP0.5-R1 blocker 3: one-active-turn bound to the EXACT turn identity ──

// scriptedTurnServer gives the test manual control over provider
// notifications: turn/start responses assign real turn ids, but lifecycle
// notifications are emitted ONLY when the test injects them.
type scriptedTurnServer struct {
	threadID string
	mu       sync.Mutex
	turnIDs  []string // assigned per turn/start, in order
	emit     func(map[string]any)
	emitMu   sync.Mutex
	// handled receives one signal per turn/start AFTER its response was
	// emitted, so tests can inject notifications in wire order.
	handled chan struct{}
}

func (s *scriptedTurnServer) handler() scriptHandler {
	return func(method string, id float64, _ map[string]any, out func(map[string]any)) {
		s.emitMu.Lock()
		s.emit = out
		s.emitMu.Unlock()
		switch method {
		case "initialize":
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
		case "thread/start":
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"threadId": s.threadID}})
		case "turn/start":
			s.mu.Lock()
			turnID := s.turnIDs[0]
			s.turnIDs = s.turnIDs[1:]
			s.mu.Unlock()
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"turn": map[string]any{"id": turnID}}})
			s.handled <- struct{}{}
		}
	}
}

// inject emits one provider notification on the live connection.
func (s *scriptedTurnServer) inject(method string, params map[string]any) {
	s.emitMu.Lock()
	emit := s.emit
	s.emitMu.Unlock()
	emit(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (s *scriptedTurnServer) turnParams(threadID, turnID string) map[string]any {
	return map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID}}
}

// TestManagedTurnIdentity_StaleCompletionInert reproduces the reviewer
// interleaving: turn A completes; turn B starts; a LATE DUPLICATE completion
// of turn A arrives — it must not release turn B's claim (no third prompt is
// accepted, status stays working). The known-bad control (the same message
// with turn B's exact id) does release the turn.
func TestManagedTurnIdentity_StaleCompletionInert(t *testing.T) {
	srv := &scriptedTurnServer{threadID: "thread-T1", turnIDs: []string{"turn-A", "turn-B", "turn-D"}, handled: make(chan struct{}, 4)}
	rec := newPumpRecorder()
	fl := &fakeLauncher{handler: srv.handler()}
	managed := newTestManagedService(fl)
	managed.pumpObserver = rec.observe
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	id := resp["id"]

	// Turn A: start, run, complete.
	if err := managed.SubmitPrompt(id, 1, "prompt A"); err != nil {
		t.Fatalf("prompt A: %v", err)
	}
	<-srv.handled // turn/start response emitted (wire order preserved)
	srv.inject("turn/started", srv.turnParams("thread-T1", "turn-A"))
	waitForStatus(t, managed.Registry(), id, ManagedStatusWorking)
	srv.inject("turn/completed", srv.turnParams("thread-T1", "turn-A"))
	waitForStatus(t, managed.Registry(), id, ManagedStatusCompleted)

	// Turn B starts and is working.
	if err := managed.SubmitPrompt(id, 1, "prompt B"); err != nil {
		t.Fatalf("prompt B: %v", err)
	}
	<-srv.handled
	srv.inject("turn/started", srv.turnParams("thread-T1", "turn-B"))
	waitForStatus(t, managed.Registry(), id, ManagedStatusWorking)

	// LATE DUPLICATE completion of turn A — must be inert.
	srv.inject("turn/completed", srv.turnParams("thread-T1", "turn-A"))
	rec.waitForCount(t, "turn/completed", 2) // the stale one was CONSUMED
	if got, _ := managed.Registry().Get(id); got.NativeStatus != ManagedStatusWorking {
		t.Fatalf("stale completion changed status to %s", got.NativeStatus)
	}
	if err := managed.SubmitPrompt(id, 1, "prompt C"); err == nil || !strings.Contains(err.Error(), "turn already active") {
		t.Fatalf("stale completion released the active turn: prompt C err = %v", err)
	}

	// A wrong-turn assistant item is likewise inert.
	srv.inject("item/completed", map[string]any{
		"threadId": "thread-T1", "turnId": "turn-A",
		"item": map[string]any{"type": "agentMessage", "id": "i", "text": "stale text"},
	})
	rec.waitFor(t, "item/completed")
	store, _, _ := managed.eventStoreFor(id)
	events, _ := store.readAfter(0, 0)
	for _, ev := range events {
		if ev.Kind == ManagedEventAssistant && ev.Text == "stale text" {
			t.Fatal("wrong-turn assistant item was projected")
		}
	}

	// KNOWN-BAD CONTROL: the exact current turn id DOES complete the turn.
	srv.inject("turn/completed", srv.turnParams("thread-T1", "turn-B"))
	waitForStatus(t, managed.Registry(), id, ManagedStatusCompleted)
	if err := managed.SubmitPrompt(id, 1, "prompt D"); err != nil {
		t.Fatalf("exact completion did not release the turn: %v", err)
	}
}

// TestManagedTurnIdentity_ForeignStartedCannotFabricateWorking: a
// turn/started for a turn we never requested (no active claim, or a
// different id while one is pending) never produces working.
func TestManagedTurnIdentity_ForeignStartedCannotFabricateWorking(t *testing.T) {
	srv := &scriptedTurnServer{threadID: "thread-T2", turnIDs: []string{"turn-X"}, handled: make(chan struct{}, 4)}
	rec := newPumpRecorder()
	fl := &fakeLauncher{handler: srv.handler()}
	managed := newTestManagedService(fl)
	managed.pumpObserver = rec.observe
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	id := resp["id"]

	// No claim at all: a spontaneous turn/started is inert.
	srv.inject("turn/started", srv.turnParams("thread-T2", "turn-GHOST"))
	rec.waitFor(t, "turn/started")
	if got, _ := managed.Registry().Get(id); got.NativeStatus != ManagedStatusIdle {
		t.Fatalf("ghost turn/started fabricated %s", got.NativeStatus)
	}

	// With a claim bound to turn-X, a different turn's started is inert.
	if err := managed.SubmitPrompt(id, 1, "prompt X"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	<-srv.handled
	srv.inject("turn/started", srv.turnParams("thread-T2", "turn-X"))
	waitForStatus(t, managed.Registry(), id, ManagedStatusWorking)
	srv.inject("turn/completed", srv.turnParams("thread-T2", "turn-GHOST"))
	rec.waitFor(t, "turn/completed")
	if got, _ := managed.Registry().Get(id); got.NativeStatus != ManagedStatusWorking {
		t.Fatalf("ghost completion changed status to %s", got.NativeStatus)
	}
}
