package term

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
)

// ── Fakes: dispatch + state-rule tests without a real process ──
//
// A fake controlled session has no Recorder, so OwnedPTYRuntime.awaitExit
// returns immediately (GetRecorder is nil) and Stop/Kill converge to a
// terminal state deterministically. PA2c: lifecycle routing is driven by the
// canonical adapter PREFIX dispatch table, never by adapter capability or
// Registry probing.

type lcAdapter struct {
	name       string
	managed    bool
	mu         sync.Mutex
	sessions   map[string]*lcSession
	terminated []string
}

func newLCAdapter(name string, managed bool) *lcAdapter {
	return &lcAdapter{name: name, managed: managed, sessions: map[string]*lcSession{}}
}
func (a *lcAdapter) Name() string { return a.name }
func (a *lcAdapter) ListSessions(context.Context) ([]mux.Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]mux.Session, 0, len(a.sessions))
	for _, s := range a.sessions {
		out = append(out, s)
	}
	return out, nil
}
func (a *lcAdapter) TranscriptCaptureMode() mux.TranscriptCaptureMode {
	return mux.CaptureModeByteStream
}
func (a *lcAdapter) ManagedLifecycle() bool { return a.managed }
func (a *lcAdapter) TerminateSession(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.terminated = append(a.terminated, id)
	delete(a.sessions, id)
	return nil
}

// CompareAndTerminate implements mux.SessionIdentityTerminator so the
// PA2c-R4 atomic cleanups in this package route through the adapter-level
// lock.  Identity comparison and detach happen under the lock; no I/O.
func (a *lcAdapter) CompareAndTerminate(_ context.Context, localID string, expected mux.Session) error {
	a.mu.Lock()
	s, ok := a.sessions[localID]
	if !ok {
		a.mu.Unlock()
		return mux.ErrSessionNotFound
	}
	if s != expected {
		a.mu.Unlock()
		return mux.ErrStaleSessionIdentity
	}
	a.terminated = append(a.terminated, localID)
	delete(a.sessions, localID)
	a.mu.Unlock()
	return nil
}

func (a *lcAdapter) add(localID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessions[localID] = &lcSession{id: localID, adapter: a.name}
	return a.name + ":" + localID
}

type lcSession struct {
	id, adapter string
	mu          sync.Mutex
	signals     []bool
}

func (s *lcSession) ID() string          { return s.id }
func (s *lcSession) AdapterName() string { return s.adapter }
func (s *lcSession) Title() string       { return s.id }
func (s *lcSession) TerminateGroup(force bool) error {
	s.mu.Lock()
	s.signals = append(s.signals, force)
	s.mu.Unlock()
	return nil
}

// lcService builds the PA2c dispatcher over an OwnedPTYRuntime whose spawn
// seam uses the given adapter's registry.
func lcService(t *testing.T, a *lcAdapter) *LifecycleService {
	t.Helper()
	reg := mux.MustNewRegistry(a)
	ctlAdapter, _ := reg.Adapter("controlled_pty")
	owned := NewOwnedPTYRuntime(ctlAdapter, NewActivityBuffer(50), nil)
	owned.graceful = 200 * time.Millisecond
	return NewLifecycleService(owned, NewActivityBuffer(50), nil)
}

// ── PA2c dispatch table: unknown/legacy fail closed, no Registry probe ──

func TestLifecycle_DispatchTable_FailClosed(t *testing.T) {
	// Legacy/unknown adapter prefixes fail closed at dispatch — even when a
	// live session with that id exists in a Registry, because the dispatcher
	// has no Registry to probe. TerminalTransport/byte adapters are
	// deliberately absent from the dispatch table.
	svc := lcService(t, newLCAdapter("controlled_pty", true))
	for _, id := range []string{
		"tmux:e1",        // legacy external adapter
		"cmux:surface:1", // legacy external adapter
		"localpty:x",     // legacy external adapter
		"faketmux:s1",    // unknown adapter (would have passed the old capability gate)
		"garbage-no-colon",
	} {
		if _, err := svc.Stop(context.Background(), id); err != ErrLifecycleUnsupported {
			t.Errorf("Stop(%q) err = %v, want fail-closed ErrLifecycleUnsupported", id, err)
		}
		if _, err := svc.Kill(context.Background(), id); err != ErrLifecycleUnsupported {
			t.Errorf("Kill(%q) err = %v, want fail-closed ErrLifecycleUnsupported", id, err)
		}
		if _, err := svc.Delete(context.Background(), id); err != ErrLifecycleUnsupported {
			t.Errorf("Delete(%q) err = %v, want fail-closed ErrLifecycleUnsupported", id, err)
		}
	}
}

// ── HTTP status-code boundary ──

func lcRequest(t *testing.T, h *Handlers, method, id, action string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/api/sessions/" + id
	if action != "" {
		path += "/" + action
	}
	req := httptest.NewRequest(method, path, nil)
	req.SetPathValue("id", id)
	rr := httptest.NewRecorder()
	switch action {
	case "stop":
		h.HandleSessionStop(rr, req)
	case "kill":
		h.HandleSessionKill(rr, req)
	default:
		h.HandleSessionDelete(rr, req)
	}
	return rr
}

func TestLifecycle_HTTPStatusCodes(t *testing.T) {
	// Legacy adapter → 422 fail closed, not 500.
	ext := newLCAdapter("tmux", false)
	extID := ext.add("e1")
	svcE := lcService(t, ext)
	hE := &Handlers{Lifecycle: svcE}
	for _, action := range []string{"stop", "kill", ""} {
		method := http.MethodPost
		if action == "" {
			method = http.MethodDelete
		}
		if rr := lcRequest(t, hE, method, extID, action); rr.Code != http.StatusUnprocessableEntity {
			t.Fatalf("external %q status = %d, want 422 (body=%s)", action, rr.Code, rr.Body.String())
		}
	}

	// Unknown controlled_pty session → 404.
	man := newLCAdapter("controlled_pty", true)
	svcM := lcService(t, man)
	hM := &Handlers{Lifecycle: svcM}
	if rr := lcRequest(t, hM, http.MethodPost, "controlled_pty:missing", "stop"); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown stop status = %d, want 404", rr.Code)
	}
	if rr := lcRequest(t, hM, http.MethodDelete, "controlled_pty:missing", ""); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown delete status = %d, want 404", rr.Code)
	}

	// Managed stop → 200 with structured result.
	sid := man.add("m1")
	svcM.OwnedPTY().RegisterForTest(sid, "", "n", nil)
	rr := lcRequest(t, hM, http.MethodPost, sid, "stop")
	if rr.Code != http.StatusOK {
		t.Fatalf("managed stop status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var res LifecycleResult
	json.Unmarshal(rr.Body.Bytes(), &res)
	if res.SessionID != sid || res.Action != "stop" || res.State != LifecycleExited {
		t.Fatalf("result = %+v, want stop/exited for %s", res, sid)
	}
}

// ── Stop idempotency + Delete state rules (fakes) ──

func TestLifecycle_StopIdempotentThenDelete(t *testing.T) {
	a := newLCAdapter("controlled_pty", true)
	id := a.add("m1")
	svc := lcService(t, a)
	svc.OwnedPTY().RegisterForTest(id, "", "n", nil)

	r1, err := svc.Stop(context.Background(), id)
	if err != nil || r1.State != LifecycleExited {
		t.Fatalf("first stop = %+v err=%v, want exited", r1, err)
	}
	// Repeated Stop is safe and returns the current terminal state.
	r2, err := svc.Stop(context.Background(), id)
	if err != nil || r2.State != LifecycleExited {
		t.Fatalf("second stop = %+v err=%v, want exited", r2, err)
	}
	// Delete of a terminal session removes the owned record + activity.
	if _, err := svc.Delete(context.Background(), id); err != nil {
		t.Fatalf("delete exited: %v", err)
	}
	if _, ok := svc.OwnedPTY().Get(id); ok {
		t.Fatalf("owned record still present after delete")
	}
	// Repeated Delete → 404 (gone).
	if _, err := svc.Delete(context.Background(), id); err != ErrLifecycleNotFound {
		t.Fatalf("repeated delete err = %v, want not found", err)
	}
}

func TestLifecycle_DeleteRunningRejected_PreservesUnrelated(t *testing.T) {
	a := newLCAdapter("controlled_pty", true)
	idA := a.add("a")
	idB := a.add("b")
	svc := lcService(t, a)
	svc.OwnedPTY().RegisterForTest(idA, "", "a", nil)
	svc.OwnedPTY().RegisterForTest(idB, "", "b", nil)

	// Running session cannot be silently deleted → 409-equivalent error.
	if _, err := svc.Delete(context.Background(), idA); err != ErrLifecycleNotTerminal {
		t.Fatalf("delete running err = %v, want not-terminal", err)
	}
	if _, ok := svc.OwnedPTY().Get(idA); !ok {
		t.Fatalf("running session A was removed by a rejected delete")
	}

	// Stop + delete A; B must be untouched.
	svc.Stop(context.Background(), idA)
	if _, err := svc.Delete(context.Background(), idA); err != nil {
		t.Fatalf("delete exited A: %v", err)
	}
	if _, ok := svc.OwnedPTY().Get(idB); !ok {
		t.Fatalf("unrelated session B was affected by deleting A")
	}
}

// ── Real process: actual termination, escalation, subscriber EOF ──

// realOwned builds an OwnedPTYRuntime over the real controlled-PTY adapter.
func realOwned(t *testing.T) *OwnedPTYRuntime {
	t.Helper()
	reg := mux.MustNewRegistry(mux.NewControlledPTYAdapter())
	ctlAdapter, _ := reg.Adapter("controlled_pty")
	owned := NewOwnedPTYRuntime(ctlAdapter, NewActivityBuffer(100), nil)
	owned.graceful = 400 * time.Millisecond
	return owned
}

func realService(t *testing.T) *LifecycleService {
	t.Helper()
	return NewLifecycleService(realOwned(t), NewActivityBuffer(100), nil)
}

// findSessionInAdapter returns a session by canonical id from an adapter's
// session list, or nil if not found.
func findSessionInAdapter(adapter mux.Adapter, canonicalID string) (mux.Session, bool) {
	ref := mux.ParseSessionID(canonicalID)
	sessions, _ := adapter.ListSessions(context.Background())
	for _, s := range sessions {
		if s.ID() == ref.LocalID {
			return s, true
		}
	}
	return nil, false
}

// realManaged launches a real controlled-PTY runtime through the owner.
func realManaged(t *testing.T, svc *LifecycleService, shellCmd string) string {
	t.Helper()
	owned := svc.OwnedPTY()
	opts := mux.CreateOptions{Name: genLocalID("lc"), Executable: "/bin/sh", Args: []string{"-c", shellCmd}}
	id, err := owned.Create(context.Background(), opts, "", "test")
	if err != nil {
		t.Fatalf("create real session: %v", err)
	}
	t.Cleanup(func() {
		DeleteRecorder(id)
		// PA2d: terminate via owned adapter, not Registry
		_ = owned.terminateAdapterSession(context.Background(), mux.ParseSessionID(id).LocalID)
	})
	return id
}

func TestLifecycle_Stop_RealProcess_TerminatesAndRetainsHistory(t *testing.T) {
	svc := realService(t)
	id := realManaged(t, svc, "sleep 30")
	// A subscriber must receive EOF (channel close) when the session ends.
	rec := GetRecorder(id)
	if rec == nil {
		t.Fatal("no recorder")
	}
	_, sub := rec.SubscribeWithBootstrap()

	res, err := svc.Stop(context.Background(), id)
	if err != nil || res.State != LifecycleExited {
		t.Fatalf("stop = %+v err=%v, want exited", res, err)
	}
	// Local subscriber cleanup: channel is closed (session-ended/EOF).
	select {
	case _, ok := <-sub:
		if ok {
			// drain until closed
			for range sub {
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not receive EOF after Stop")
	}
	// History remains: owned lifecycle row present + terminal.
	entry, ok := svc.OwnedPTY().Get(id)
	if !ok || entry.State != LifecycleExited {
		t.Fatalf("owned record after stop = %+v ok=%v, want exited row retained", entry, ok)
	}
	// Recorder is gone (stopped once).
	if GetRecorder(id) != nil {
		t.Fatal("recorder still present after stop")
	}
}

func TestLifecycle_Stop_SigkillEscalation(t *testing.T) {
	svc := realService(t)
	// sh ignores SIGTERM (trap) and keeps the PTY open by respawning children,
	// so graceful Stop must wait the timeout and escalate to SIGKILL.
	id := realManaged(t, svc, "trap '' TERM; while true; do sleep 1; done")
	// Give sh time to install its TERM trap before signalling, otherwise the
	// early SIGTERM lands before the handler and kills it.
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	res, err := svc.Stop(context.Background(), id)
	if err != nil || res.State != LifecycleExited {
		t.Fatalf("stop(stubborn) = %+v err=%v, want exited", res, err)
	}
	if elapsed := time.Since(start); elapsed < svc.OwnedPTY().graceful {
		t.Fatalf("stop returned in %v, expected to wait the graceful timeout before SIGKILL", elapsed)
	}
}

func TestLifecycle_Kill_RealProcess(t *testing.T) {
	svc := realService(t)
	id := realManaged(t, svc, "sleep 30")
	res, err := svc.Kill(context.Background(), id)
	if err != nil || res.State != LifecycleKilled {
		t.Fatalf("kill = %+v err=%v, want killed", res, err)
	}
}

func TestLifecycle_StopKillAndNaturalRaces(t *testing.T) {
	// Concurrent Stop + Kill on the same real session must be safe and converge.
	svc := realService(t)
	id := realManaged(t, svc, "sleep 30")
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); svc.Stop(context.Background(), id) }()
		go func() { defer wg.Done(); svc.Kill(context.Background(), id) }()
	}
	wg.Wait()
	entry, ok := svc.OwnedPTY().Get(id)
	if !ok || !entry.State.Terminal() {
		t.Fatalf("after races owned record = %+v ok=%v, want terminal", entry, ok)
	}

	// Natural exit racing with Stop.
	id2 := realManaged(t, svc, "exit 0")
	// Give the process a moment to exit naturally, then Stop concurrently.
	var wg2 sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg2.Add(1)
		go func() { defer wg2.Done(); svc.Stop(context.Background(), id2) }()
	}
	wg2.Wait()
	if e, ok := svc.OwnedPTY().Get(id2); !ok || !e.State.Terminal() {
		t.Fatalf("natural-exit race owned record = %+v ok=%v, want terminal", e, ok)
	}
}
