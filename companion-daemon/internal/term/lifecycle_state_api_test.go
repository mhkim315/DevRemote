package term

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"devremote/companion-daemon/internal/mux"
)

// ── M3b remediation (BLOCKER 1+2): /api/sessions is authoritative for managed
// lifecycle state, and retains terminal rows after Registry removal. ──

// lsAdapter is a managed/external adapter whose live sessions are controllable
// per-test. managed=true mirrors controlled_pty; managed=false mirrors tmux.
type lsAdapter struct {
	name     string
	managed  bool
	sessions map[string]mux.Session
}

func newLSAdapter(name string, managed bool, liveLocalIDs ...string) *lsAdapter {
	a := &lsAdapter{name: name, managed: managed, sessions: map[string]mux.Session{}}
	for _, id := range liveLocalIDs {
		a.sessions[id] = &capBoundarySession{id: id, adapter: name}
	}
	return a
}
func (a *lsAdapter) Name() string { return a.name }
func (a *lsAdapter) ListSessions(context.Context) ([]mux.Session, error) {
	out := make([]mux.Session, 0, len(a.sessions))
	for _, s := range a.sessions {
		out = append(out, s)
	}
	return out, nil
}
func (a *lsAdapter) TranscriptCaptureMode() mux.TranscriptCaptureMode {
	return mux.CaptureModeByteStream
}
func (a *lsAdapter) ManagedLifecycle() bool { return a.managed }

// seedCatalog directly seeds an owned-PTY lifecycle row in a chosen state so
// every state (incl. failed, which the runtime only reaches on spawn error) is
// testable.
func seedCatalog(svc *LifecycleService, id, adapter, name string, state LifecycleState) {
	o := svc.OwnedPTY()
	o.mu.Lock()
	o.nextGen++
	o.entries[id] = &CatalogEntry{ID: id, Adapter: adapter, Name: name, State: state, Generation: o.nextGen}
	o.mu.Unlock()
}

func getSessionsSnapshot(t *testing.T, h *Handlers) map[string]SessionTelemetry {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rr := httptest.NewRecorder()
	h.HandleSessionsAPI(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var rows []SessionTelemetry
	if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	byID := map[string]SessionTelemetry{}
	for _, r := range rows {
		if _, dup := byID[r.ID]; dup {
			t.Fatalf("duplicate session id in snapshot (dedup failed): %s", r.ID)
		}
		byID[r.ID] = r
	}
	return byID
}

func TestAPISessions_AuthoritativeLifecycleState(t *testing.T) {
	managed := newLSAdapter("controlled_pty", true, "run", "stop")
	external := newLSAdapter("tmux", false, "tm1")
	reg := mux.MustNewRegistry(managed, external)
	svc := NewLifecycleService(NewOwnedPTYRuntime(reg, NewActivityBuffer(50), nil), NewActivityBuffer(50), nil)

	// Live managed rows with authoritative catalog states.
	seedCatalog(svc, "controlled_pty:run", "controlled_pty", "runner", LifecycleRunning)
	seedCatalog(svc, "controlled_pty:stop", "controlled_pty", "stopper", LifecycleStopping)
	// Retained terminal rows whose runtime has left the Registry.
	seedCatalog(svc, "controlled_pty:exited", "controlled_pty", "gone", LifecycleExited)
	seedCatalog(svc, "controlled_pty:killed", "controlled_pty", "killed", LifecycleKilled)
	seedCatalog(svc, "controlled_pty:failed", "controlled_pty", "failed", LifecycleFailed)

	h := &Handlers{Registry: reg, Events: NewMemoryEventStore(), Lifecycle: svc}
	byID := getSessionsSnapshot(t, h)

	cases := map[string]string{
		"controlled_pty:run":    "running",
		"controlled_pty:stop":   "stopping",
		"controlled_pty:exited": "exited",
		"controlled_pty:killed": "killed",
		"controlled_pty:failed": "failed",
	}
	for id, wantState := range cases {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("session %s missing from /api/sessions (retained row not surfaced)", id)
		}
		if row.LifecycleState != wantState {
			t.Errorf("%s lifecycleState = %q, want %q", id, row.LifecycleState, wantState)
		}
		if !slices.Contains(row.AdapterCapabilities, "managedLifecycle") {
			t.Errorf("%s adapterCapabilities missing managedLifecycle: %v", id, row.AdapterCapabilities)
		}
	}

	// Retained terminal rows keep the history read affordance (Activity/Transcript
	// remain reviewable until Delete).
	for _, id := range []string{"controlled_pty:exited", "controlled_pty:killed", "controlled_pty:failed"} {
		if !slices.Contains(byID[id].Capabilities, "history") {
			t.Errorf("%s retained row missing history capability: %v", id, byID[id].Capabilities)
		}
	}

	// External (non-managed) session carries NO lifecycleState and is not managed.
	tm, ok := byID["tmux:tm1"]
	if !ok {
		t.Fatalf("external tmux session missing from snapshot")
	}
	if tm.LifecycleState != "" {
		t.Errorf("external tmux lifecycleState = %q, want empty", tm.LifecycleState)
	}
	if slices.Contains(tm.AdapterCapabilities, "managedLifecycle") {
		t.Errorf("external tmux must not advertise managedLifecycle: %v", tm.AdapterCapabilities)
	}
}

func TestAPISessions_LiveRowWinsOverCatalog_NoDuplicate(t *testing.T) {
	// A session both live (Registry) AND cataloged must appear exactly once, with
	// the authoritative catalog state annotated onto the live row.
	managed := newLSAdapter("controlled_pty", true, "s1")
	reg := mux.MustNewRegistry(managed)
	svc := NewLifecycleService(NewOwnedPTYRuntime(reg, NewActivityBuffer(50), nil), NewActivityBuffer(50), nil)
	seedCatalog(svc, "controlled_pty:s1", "controlled_pty", "s1", LifecycleStopping)

	h := &Handlers{Registry: reg, Events: NewMemoryEventStore(), Lifecycle: svc}
	byID := getSessionsSnapshot(t, h) // getSessionsSnapshot fails on any duplicate id
	if byID["controlled_pty:s1"].LifecycleState != "stopping" {
		t.Errorf("live+cataloged row lifecycleState = %q, want stopping", byID["controlled_pty:s1"].LifecycleState)
	}
}

func TestAPISessions_DeleteRemovesRetainedRow(t *testing.T) {
	// A retained terminal row is listable until Delete History succeeds, then gone.
	managed := newLSAdapter("controlled_pty", true) // no live sessions
	reg := mux.MustNewRegistry(managed)
	svc := NewLifecycleService(NewOwnedPTYRuntime(reg, NewActivityBuffer(50), nil), NewActivityBuffer(50), nil)
	seedCatalog(svc, "controlled_pty:done", "controlled_pty", "done", LifecycleExited)

	h := &Handlers{Registry: reg, Events: NewMemoryEventStore(), Lifecycle: svc}

	if _, ok := getSessionsSnapshot(t, h)["controlled_pty:done"]; !ok {
		t.Fatalf("retained terminal row must be listable before Delete")
	}
	if _, err := svc.Delete(context.Background(), "controlled_pty:done"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := getSessionsSnapshot(t, h)["controlled_pty:done"]; ok {
		t.Fatalf("retained row must be gone from /api/sessions after Delete")
	}
}

func TestAPISessions_NoLifecycleServiceIsInert(t *testing.T) {
	// Without a LifecycleService, the snapshot is unchanged (no lifecycleState,
	// no phantom rows) — additive behavior only.
	managed := newLSAdapter("controlled_pty", true, "s1")
	reg := mux.MustNewRegistry(managed)
	h := &Handlers{Registry: reg, Events: NewMemoryEventStore()} // Lifecycle nil
	byID := getSessionsSnapshot(t, h)
	if byID["controlled_pty:s1"].LifecycleState != "" {
		t.Errorf("no-lifecycle snapshot must carry empty lifecycleState")
	}
}
