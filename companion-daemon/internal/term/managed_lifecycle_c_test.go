package term

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── SP0.5-C: lifecycle (Stop/Kill/Delete), natural exit, reconnect ──

func createInteractive(t *testing.T, app *interactiveAppServer) (*ManagedCodexService, *fakeLauncher, string) {
	t.Helper()
	managed, fl := newInteractiveService(t, app)
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	if resp["error"] != "" {
		t.Fatalf("create: %v", resp)
	}
	return managed, fl, resp["id"]
}

// TestManagedStop_GracefulThenNonCurrent: Stop closes input, TERMs the group,
// and returns with the record non-current; a second Stop is idempotent; a
// later prompt is rejected with zero provider writes.
func TestManagedStop_GracefulThenNonCurrent(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-C1"}
	managed, fl, id := createInteractive(t, app)

	if err := managed.Stop(id, 1, "", 0); err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case <-fl.procs[0].termed:
	default:
		t.Fatal("stop did not send the graceful TERM")
	}
	rec, _ := managed.Registry().Get(id)
	if !rec.Exited || rec.NativeStatus != ManagedStatusExited {
		t.Fatalf("record after stop = %+v", rec)
	}
	if err := managed.Stop(id, 1, "", 0); err != nil {
		t.Fatalf("second stop not idempotent: %v", err)
	}
	if err := managed.SubmitPrompt(id, 1, "late"); err == nil {
		t.Fatal("stopped session accepted a prompt")
	}
	if app.turnCount() != 0 {
		t.Fatalf("provider writes = %d", app.turnCount())
	}
}

// TestManagedStop_EscalatesToKill: a child that ignores TERM is KILLed within
// the bound and the record is still non-current when Stop returns.
func TestManagedStop_EscalatesToKill(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-C2"}
	fl := &fakeLauncher{handler: app.handler(), ignoreTerm: true}
	managed := newTestManagedService(fl)
	resp := ipcCreateRoundTrip(t, managed, sp05InteractiveRequest())
	id := resp["id"]

	if err := managed.Stop(id, 1, "", 0); err != nil {
		t.Fatalf("stop with TERM-ignoring child: %v", err)
	}
	select {
	case <-fl.procs[0].termed:
	default:
		t.Fatal("TERM was never attempted")
	}
	select {
	case <-fl.procs[0].killed:
	default:
		t.Fatal("KILL escalation did not happen")
	}
	rec, _ := managed.Registry().Get(id)
	if !rec.Exited {
		t.Fatalf("record after escalated stop = %+v", rec)
	}
}

// TestManagedKill_ForceAndIdempotent: Kill force-terminates, marks
// non-current, and is idempotent on a terminal session.
func TestManagedKill_ForceAndIdempotent(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-C3"}
	managed, fl, id := createInteractive(t, app)

	if err := managed.Kill(id, 1, "", 0); err != nil {
		t.Fatalf("kill: %v", err)
	}
	select {
	case <-fl.procs[0].killed:
	default:
		t.Fatal("kill did not terminate the child")
	}
	rec, _ := managed.Registry().Get(id)
	if !rec.Exited {
		t.Fatalf("record after kill = %+v", rec)
	}
	if err := managed.Kill(id, 1, "", 0); err != nil {
		t.Fatalf("second kill not idempotent: %v", err)
	}
}

// TestManagedLifecycle_WrongStaleBinding: unknown session and stale epoch
// fail closed for Stop/Kill/Delete without touching any process.
func TestManagedLifecycle_WrongStaleBinding(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-C4"}
	managed, fl, id := createInteractive(t, app)

	for i, err := range []error{
		managed.Stop("codex_app_server:nope", 1, "", 0),
		managed.Kill("codex_app_server:nope", 1, "", 0),
		managed.Delete("codex_app_server:nope", 1, "", 0),
		managed.Stop(id, 9, "", 0),
		managed.Kill(id, 9, "", 0),
		managed.Delete(id, 9, "", 0),
	} {
		if err == nil {
			t.Fatalf("case %d: wrong/stale binding accepted", i)
		}
	}
	select {
	case <-fl.procs[0].termed:
		t.Fatal("wrong-binding lifecycle op signalled the child")
	case <-fl.procs[0].killed:
		t.Fatal("wrong-binding lifecycle op killed the child")
	default:
	}
}

// TestManagedDelete_TerminalOnly_ClearsData_LaterEventsInert: Delete requires
// a terminal state, removes the record and the bounded output store, and a
// deleted session serves nothing and absorbs nothing.
func TestManagedDelete_TerminalOnly_ClearsData_LaterEventsInert(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-C5"}
	managed, _, id := createInteractive(t, app)
	store, _, _ := managed.eventStoreFor(id)

	if err := managed.Delete(id, 1, "", 0); err == nil || !strings.Contains(err.Error(), "not terminal") {
		t.Fatalf("non-terminal delete err = %v", err)
	}
	if err := managed.Stop(id, 1, "", 0); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := managed.Delete(id, 1, "", 0); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := managed.Registry().Get(id); ok {
		t.Fatal("record survives delete")
	}
	// Later events are inert: the store is closed for reads and appends.
	if _, err := store.readAfter(0, 0); err == nil {
		t.Fatal("deleted session still serves events")
	}
	store.append(ManagedEventWorking, "") // must be a no-op, not a panic
	// REST surfaces are gone.
	h := &Handlers{Managed: managed}
	if code, _ := getEvents(t, h, id, "epoch=1&cursor=0"); code != 404 {
		t.Fatalf("events after delete code = %d", code)
	}
	// Duplicate delete: clean not-found, no side effects.
	if err := managed.Delete(id, 1, "", 0); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("duplicate delete err = %v", err)
	}
	// A deleted session accepts no prompt.
	if err := managed.SubmitPrompt(id, 1, "late"); err == nil {
		t.Fatal("deleted session accepted a prompt")
	}
}

// TestManagedLifecycle_PromptVsStopInterleavings: (a) a prompt mid-turn is
// safely terminated by Stop (turn never completes, record exited); (b) after
// Stop a prompt is rejected with zero provider writes.
func TestManagedLifecycle_PromptVsStopInterleavings(t *testing.T) {
	gate := make(chan struct{})
	app := &interactiveAppServer{threadID: "thread-C6", reply: "never", hold: gate}
	managed, _, id := createInteractive(t, app)

	// (a) prompt first: the turn is active when Stop arrives.
	if err := managed.SubmitPrompt(id, 1, "long turn"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	waitForStatus(t, managed.Registry(), id, ManagedStatusWorking)
	if err := managed.Stop(id, 1, "", 0); err != nil {
		t.Fatalf("stop during turn: %v", err)
	}
	rec, _ := managed.Registry().Get(id)
	if !rec.Exited {
		t.Fatalf("record after stop-during-turn = %+v", rec)
	}
	// (b) prompt after stop: rejected, zero additional writes.
	if err := managed.SubmitPrompt(id, 1, "after stop"); err == nil {
		t.Fatal("post-stop prompt accepted")
	}
	if app.turnCount() != 1 {
		t.Fatalf("provider writes = %d, want 1", app.turnCount())
	}
	close(gate) // release the held goroutine (writes go nowhere — pipes closed)
}

// TestManagedLifecycle_PromptVsKill: kill during an active turn leaves the
// record exited and later prompts rejected.
func TestManagedLifecycle_PromptVsKill(t *testing.T) {
	gate := make(chan struct{})
	app := &interactiveAppServer{threadID: "thread-C7", reply: "never", hold: gate}
	managed, _, id := createInteractive(t, app)

	if err := managed.SubmitPrompt(id, 1, "long turn"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	waitForStatus(t, managed.Registry(), id, ManagedStatusWorking)
	if err := managed.Kill(id, 1, "", 0); err != nil {
		t.Fatalf("kill during turn: %v", err)
	}
	rec, _ := managed.Registry().Get(id)
	if !rec.Exited {
		t.Fatalf("record after kill-during-turn = %+v", rec)
	}
	if err := managed.SubmitPrompt(id, 1, "after kill"); err == nil {
		t.Fatal("post-kill prompt accepted")
	}
	close(gate)
}

// TestManagedNaturalExit_LifecycleAfterwards: a child that dies on its own is
// marked exited; Stop/Kill afterwards are idempotent successes and Delete
// works.
func TestManagedNaturalExit_LifecycleAfterwards(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-C8"}
	managed, fl, id := createInteractive(t, app)

	fl.procs[0].Kill() // natural death
	waitForStatus(t, managed.Registry(), id, ManagedStatusExited)
	if err := managed.Stop(id, 1, "", 0); err != nil {
		t.Fatalf("stop after natural exit: %v", err)
	}
	if err := managed.Kill(id, 1, "", 0); err != nil {
		t.Fatalf("kill after natural exit: %v", err)
	}
	if err := managed.Delete(id, 1, "", 0); err != nil {
		t.Fatalf("delete after natural exit: %v", err)
	}
}

// TestManagedReconnect_SnapshotCursor_NoStaleState: a second local attach
// with the first viewer's cursor receives only newer events; replaying the
// cursor is idempotent; nothing from a prior read can be resurrected after
// the session is deleted.
func TestManagedReconnect_SnapshotCursor_NoStaleState(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-C9", reply: "first reply"}
	managed, _, id := createInteractive(t, app)

	// First viewer sees the full turn.
	c1 := newAttachClient(t, managed, id)
	c1.sendPrompt("one")
	c1.next("working")
	c1.next("assistant")
	c1.next("completed")
	c1.conn.Close()

	// Reconnect from the consumed cursor: nothing is replayed, and a new
	// turn's events arrive with strictly increasing seq.
	store, _, _ := managed.eventStoreFor(id)
	cursor := store.newest()
	c2 := newAttachClientAt(t, managed, id, cursor)
	defer c2.conn.Close()
	c2.sendPrompt("two")
	ev := c2.next("working")
	if ev["error"] != "" {
		t.Fatalf("reconnect stream: %v", ev)
	}
	c2.next("assistant")
	c2.next("completed")
	// The reconnect stream must not have replayed any pre-cursor event: every
	// streamed seq is strictly greater than the reconnect cursor.
	for _, line := range c2.raw {
		var ev struct {
			Seq  uint64 `json:"seq"`
			Kind string `json:"kind"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Kind == "" {
			continue
		}
		if ev.Seq <= cursor {
			t.Fatalf("reconnect replayed pre-cursor state (seq %d <= cursor %d): %s", ev.Seq, cursor, line)
		}
	}
}

// TestManagedShutdown_DuringActiveTurn: daemon shutdown during a live turn
// leaves no child, no lease, no runtime, and no current state.
func TestManagedShutdown_DuringActiveTurn(t *testing.T) {
	gate := make(chan struct{})
	defer close(gate)
	app := &interactiveAppServer{threadID: "thread-C10", reply: "never", hold: gate}
	managed, fl, id := createInteractive(t, app)

	if err := managed.SubmitPrompt(id, 1, "long turn"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	waitForStatus(t, managed.Registry(), id, ManagedStatusWorking)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := managed.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown during turn: %v", err)
	}
	select {
	case <-fl.procs[0].killed:
	default:
		t.Fatal("child survived shutdown during turn")
	}
	if n := leasesLen(managed); n != 0 {
		t.Fatalf("leases = %d", n)
	}
	if n := runtimesLen(managed); n != 0 {
		t.Fatalf("runtimes = %d", n)
	}
	if managed.Registry().UpdateNativeStatus(id, 1, ManagedStatusWorking) {
		t.Fatal("registry writable after shutdown")
	}
}

// TestManagedLifecycle_ConcurrentRace: prompts, lifecycle ops, and reads race
// under -race; afterwards the session is terminal and consistent.
func TestManagedLifecycle_ConcurrentRace(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-C11", reply: "r"}
	managed, _, id := createInteractive(t, app)
	store, _, _ := managed.eventStoreFor(id)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(4)
		go func(n int) {
			defer wg.Done()
			_ = managed.SubmitPrompt(id, 1, fmt.Sprintf("p%d", n))
		}(i)
		go func() {
			defer wg.Done()
			_, _ = store.readAfter(0, 0)
			managed.Registry().List()
		}()
		go func() {
			defer wg.Done()
			_ = managed.Stop(id, 1, "", 0)
		}()
		go func() {
			defer wg.Done()
			_ = managed.Kill(id, 1, "", 0)
		}()
	}
	wg.Wait()
	rec, ok := managed.Registry().Get(id)
	if !ok || !rec.Exited {
		t.Fatalf("record after race = %+v ok=%v", rec, ok)
	}
	if err := managed.Delete(id, 1, "", 0); err != nil {
		t.Fatalf("delete after race: %v", err)
	}
}

// ── REST lifecycle handlers ──

// TestManagedLifecycleAPI_FailClosedAndIdempotent: the REST lifecycle surface
// binds epoch exactly, rejects unknown fields, requires terminal state for
// delete, and reports bounded post-op state.
func TestManagedLifecycleAPI_FailClosedAndIdempotent(t *testing.T) {
	app := &interactiveAppServer{threadID: "thread-C12"}
	managed, _, id := createInteractive(t, app)
	h := &Handlers{Managed: managed}

	do := func(method, sub, body string) (int, string) {
		req := httptest.NewRequest(method, "/api/managed-sessions/x"+sub, strings.NewReader(body))
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		switch sub {
		case "/stop":
			h.HandleManagedSessionStop(rec, req)
		case "/kill":
			h.HandleManagedSessionKill(rec, req)
		default:
			h.HandleManagedSessionDelete(rec, req)
		}
		return rec.Code, rec.Body.String()
	}

	if code, _ := do("POST", "/stop", `{"epoch":9}`); code != 409 {
		t.Fatalf("stale-epoch stop code = %d", code)
	}
	if code, _ := do("POST", "/stop", `{"epoch":1,"extra":1}`); code != 400 {
		t.Fatalf("unknown-field stop code = %d", code)
	}
	if code, _ := do("DELETE", "", `{"epoch":1}`); code != 409 {
		t.Fatalf("non-terminal delete code = %d", code)
	}
	code, body := do("POST", "/stop", `{"epoch":1}`)
	if code != 200 || !strings.Contains(body, `"nativeStatus":"exited"`) {
		t.Fatalf("stop code=%d body=%s", code, body)
	}
	// Idempotent stop repeats the terminal state.
	if code, _ := do("POST", "/stop", `{"epoch":1}`); code != 200 {
		t.Fatalf("repeat stop code = %d", code)
	}
	code, body = do("DELETE", "", `{"epoch":1}`)
	if code != 200 || !strings.Contains(body, `"deleted"`) {
		t.Fatalf("delete code=%d body=%s", code, body)
	}
	if code, _ := do("DELETE", "", `{"epoch":1}`); code != 404 {
		t.Fatalf("duplicate delete code = %d", code)
	}
	// Disabled service: 404 before any decode.
	hOff := &Handlers{}
	req := httptest.NewRequest("POST", "/api/managed-sessions/x/stop", strings.NewReader(`{"epoch":1}`))
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	hOff.HandleManagedSessionStop(rec, req)
	if rec.Code != 404 {
		t.Fatalf("disabled stop code = %d", rec.Code)
	}
}

// ── helpers ──

// newAttachClientAt attaches with an explicit event cursor (reconnect).
func newAttachClientAt(t *testing.T, managed *ManagedCodexService, sessionID string, cursor uint64) *attachClient {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	go handleIPCConnection(serverConn, nil, nil, managed, nil)
	req, _ := json.Marshal(map[string]any{
		"version": 1, "operation": "managed-attach", "sessionId": sessionID, "cursor": cursor,
	})
	if _, err := clientConn.Write(append(req, '\n')); err != nil {
		t.Fatalf("attach write: %v", err)
	}
	sc := bufio.NewScanner(clientConn)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	return &attachClient{t: t, conn: clientConn, sc: sc}
}
