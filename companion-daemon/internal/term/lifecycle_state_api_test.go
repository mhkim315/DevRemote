package term

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// ── M3b remediation (BLOCKER 1+2): /api/sessions is authoritative for managed
// lifecycle state, and retains terminal rows after Registry removal. ──

// lsAdapter is a managed/external adapter whose live sessions are controllable
// per-test. managed=true mirrors controlled_pty; managed=false mirrors legacy.
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
	svc := NewLifecycleService(NewOwnedPTYRuntime(nil, nil), nil)

	// Live managed rows with authoritative catalog states.
	seedCatalog(svc, "controlled_pty:run", "controlled_pty", "runner", LifecycleRunning)
	seedCatalog(svc, "controlled_pty:stop", "controlled_pty", "stopper", LifecycleStopping)
	// Retained terminal rows whose runtime has left the Registry.
	seedCatalog(svc, "controlled_pty:exited", "controlled_pty", "gone", LifecycleExited)
	seedCatalog(svc, "controlled_pty:killed", "controlled_pty", "killed", LifecycleKilled)
	seedCatalog(svc, "controlled_pty:failed", "controlled_pty", "failed", LifecycleFailed)

	snapshot := []SessionTelemetry{{ID: "controlled_pty:run", Adapter: "controlled_pty", AdapterCapabilities: []string{"managedLifecycle"}}, {ID: "controlled_pty:stop", Adapter: "controlled_pty", AdapterCapabilities: []string{"managedLifecycle"}}, {ID: "legacy:tm1", Adapter: "legacy"}}
	rows := mergeLifecycleState(snapshot, svc)
	byID := map[string]SessionTelemetry{}
	for _, row := range rows {
		byID[row.ID] = row
	}

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
		if (id == "controlled_pty:run" || id == "controlled_pty:stop") && !slices.Contains(row.AdapterCapabilities, "managedLifecycle") {
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
	tm, ok := byID["legacy:tm1"]
	if !ok {
		t.Fatalf("external legacy session missing from snapshot")
	}
	if tm.LifecycleState != "" {
		t.Errorf("external legacy lifecycleState = %q, want empty", tm.LifecycleState)
	}
	if slices.Contains(tm.AdapterCapabilities, "managedLifecycle") {
		t.Errorf("external legacy must not advertise managedLifecycle: %v", tm.AdapterCapabilities)
	}
}

func TestAPISessions_LiveRowWinsOverCatalog_NoDuplicate(t *testing.T) {
	// A session both live (Registry) AND cataloged must appear exactly once, with
	// the authoritative catalog state annotated onto the live row.
	svc := NewLifecycleService(NewOwnedPTYRuntime(nil, nil), nil)
	seedCatalog(svc, "controlled_pty:s1", "controlled_pty", "s1", LifecycleStopping)

	rows := mergeLifecycleState([]SessionTelemetry{{ID: "controlled_pty:s1", Adapter: "controlled_pty"}}, svc)
	byID := map[string]SessionTelemetry{}
	for _, row := range rows {
		if _, ok := byID[row.ID]; ok {
			t.Fatal("duplicate")
		}
		byID[row.ID] = row
	}
	if byID["controlled_pty:s1"].LifecycleState != "stopping" {
		t.Errorf("live+cataloged row lifecycleState = %q, want stopping", byID["controlled_pty:s1"].LifecycleState)
	}
}

func TestAPISessions_DeleteRemovesRetainedRow(t *testing.T) {
	// A retained terminal row is listable until Delete History succeeds, then gone.
	svc := NewLifecycleService(NewOwnedPTYRuntime(nil, nil), nil)
	seedCatalog(svc, "controlled_pty:done", "controlled_pty", "done", LifecycleExited)

	listed := mergeLifecycleState(nil, svc)
	if len(listed) != 1 || listed[0].ID != "controlled_pty:done" {
		t.Fatalf("retained terminal row must be listable before Delete")
	}
	if _, err := svc.Delete(context.Background(), "controlled_pty:done"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if rows := mergeLifecycleState(nil, svc); len(rows) != 0 {
		t.Fatalf("retained row must be gone after Delete: %v", rows)
	}
}

func TestAPISessions_NoLifecycleServiceIsInert(t *testing.T) {
	// Without a LifecycleService, the snapshot is unchanged (no lifecycleState,
	// no phantom rows) — additive behavior only.
	rows := mergeLifecycleState([]SessionTelemetry{{ID: "controlled_pty:s1", Adapter: "controlled_pty"}}, nil)
	if rows[0].LifecycleState != "" {
		t.Errorf("no-lifecycle snapshot must carry empty lifecycleState")
	}
}
