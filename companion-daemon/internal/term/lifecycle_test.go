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

// ── Fakes: capability + state-rule tests without a real process ──
//
// A fake managed session has no Recorder, so LifecycleService.awaitExit returns
// immediately (GetRecorder is nil) and Stop/Kill converge to a terminal state
// deterministically. Capability behavior is driven by the adapter's
// ManagedLifecycle() declaration, NOT its name.

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

func lcService(t *testing.T, a *lcAdapter) *LifecycleService {
	t.Helper()
	reg := mux.MustNewRegistry(a)
	svc := NewLifecycleService(reg, NewActivityBuffer(50))
	svc.graceful = 200 * time.Millisecond
	return svc
}

// ── Capability enforcement (capability-driven, not adapter-name) ──

func TestLifecycle_CapabilityGate(t *testing.T) {
	cases := []struct {
		name        string
		adapterName string
		managed     bool
		wantErr     error
	}{
		// A managed adapter that is NOT named controlled_pty still gets lifecycle
		// actions — proves the gate is capability-driven, not name-driven.
		{"managed non-controlled-name", "faketmux", true, nil},
		{"controlled_pty managed", "controlled_pty", true, nil},
		{"tmux external", "tmux", false, ErrLifecycleUnsupported},
		{"cmux external", "cmux", false, ErrLifecycleUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newLCAdapter(tc.adapterName, tc.managed)
			id := a.add("s1")
			svc := lcService(t, a)
			svc.Register(id, tc.adapterName, "", "n", nil)

			for _, action := range []string{"stop", "kill"} {
				var err error
				if action == "stop" {
					_, err = svc.Stop(context.Background(), id)
				} else {
					// re-register (previous Stop finalized it)
					a2 := newLCAdapter(tc.adapterName, tc.managed)
					id2 := a2.add("s2")
					svc2 := lcService(t, a2)
					svc2.Register(id2, tc.adapterName, "", "n", nil)
					_, err = svc2.Kill(context.Background(), id2)
				}
				if tc.wantErr == nil && err != nil {
					t.Fatalf("%s: unexpected error: %v", action, err)
				}
				if tc.wantErr != nil && err != tc.wantErr {
					t.Fatalf("%s: err = %v, want %v", action, err, tc.wantErr)
				}
			}
		})
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
	// Unsupported adapter → 422, not 500.
	ext := newLCAdapter("tmux", false)
	extID := ext.add("e1")
	svcE := lcService(t, ext)
	svcE.Register(extID, "tmux", "", "n", nil)
	hE := &Handlers{Registry: svcE.reg, Lifecycle: svcE}
	for _, action := range []string{"stop", "kill", ""} {
		method := http.MethodPost
		if action == "" {
			method = http.MethodDelete
		}
		if rr := lcRequest(t, hE, method, extID, action); rr.Code != http.StatusUnprocessableEntity {
			t.Fatalf("external %q status = %d, want 422 (body=%s)", action, rr.Code, rr.Body.String())
		}
	}

	// Unknown session → 404.
	man := newLCAdapter("controlled_pty", true)
	svcM := lcService(t, man)
	hM := &Handlers{Registry: svcM.reg, Lifecycle: svcM}
	if rr := lcRequest(t, hM, http.MethodPost, "controlled_pty:missing", "stop"); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown stop status = %d, want 404", rr.Code)
	}
	if rr := lcRequest(t, hM, http.MethodDelete, "controlled_pty:missing", ""); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown delete status = %d, want 404", rr.Code)
	}

	// Managed stop → 200 with structured result.
	sid := man.add("m1")
	svcM.Register(sid, "controlled_pty", "", "n", nil)
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
	svc.Register(id, "controlled_pty", "", "n", nil)

	r1, err := svc.Stop(context.Background(), id)
	if err != nil || r1.State != LifecycleExited {
		t.Fatalf("first stop = %+v err=%v, want exited", r1, err)
	}
	// Repeated Stop is safe and returns the current terminal state.
	r2, err := svc.Stop(context.Background(), id)
	if err != nil || r2.State != LifecycleExited {
		t.Fatalf("second stop = %+v err=%v, want exited", r2, err)
	}
	// Delete of a terminal session removes catalog + activity.
	if _, err := svc.Delete(context.Background(), id); err != nil {
		t.Fatalf("delete exited: %v", err)
	}
	if _, ok := svc.catalog.Get(id); ok {
		t.Fatalf("catalog entry still present after delete")
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
	svc.Register(idA, "controlled_pty", "", "a", nil)
	svc.Register(idB, "controlled_pty", "", "b", nil)

	// Running session cannot be silently deleted → 409-equivalent error.
	if _, err := svc.Delete(context.Background(), idA); err != ErrLifecycleNotTerminal {
		t.Fatalf("delete running err = %v, want not-terminal", err)
	}
	if _, ok := svc.catalog.Get(idA); !ok {
		t.Fatalf("running session A was removed by a rejected delete")
	}

	// Stop + delete A; B must be untouched.
	svc.Stop(context.Background(), idA)
	if _, err := svc.Delete(context.Background(), idA); err != nil {
		t.Fatalf("delete exited A: %v", err)
	}
	if _, ok := svc.catalog.Get(idB); !ok {
		t.Fatalf("unrelated session B was affected by deleting A")
	}
}

// ── Real process: actual termination, escalation, subscriber EOF ──

func realManaged(t *testing.T, svc *LifecycleService, shellCmd string) string {
	t.Helper()
	opts := mux.CreateOptions{Name: genLocalID("lc"), Executable: "/bin/sh", Args: []string{"-c", shellCmd}}
	id, rec, err := createControlledSession(context.Background(), svc.reg, svc.activity, opts)
	if err != nil {
		t.Fatalf("create real session: %v", err)
	}
	svc.Register(id, "controlled_pty", "", "test", rec)
	t.Cleanup(func() {
		DeleteRecorder(id)
		_ = svc.reg.TerminateSession(context.Background(), "controlled_pty", mux.ParseSessionID(id).LocalID)
	})
	return id
}

func realService(t *testing.T) *LifecycleService {
	t.Helper()
	reg := mux.MustNewRegistry(mux.NewControlledPTYAdapter())
	svc := NewLifecycleService(reg, NewActivityBuffer(100))
	svc.graceful = 400 * time.Millisecond
	return svc
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
	// History remains: catalog row present + terminal.
	entry, ok := svc.catalog.Get(id)
	if !ok || entry.State != LifecycleExited {
		t.Fatalf("catalog after stop = %+v ok=%v, want exited row retained", entry, ok)
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
	if elapsed := time.Since(start); elapsed < svc.graceful {
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
	entry, ok := svc.catalog.Get(id)
	if !ok || !entry.State.Terminal() {
		t.Fatalf("after races catalog = %+v ok=%v, want terminal", entry, ok)
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
	if e, ok := svc.catalog.Get(id2); !ok || !e.State.Terminal() {
		t.Fatalf("natural-exit race catalog = %+v ok=%v, want terminal", e, ok)
	}
}
