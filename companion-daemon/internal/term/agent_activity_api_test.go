package term

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// S1-D — the additive authenticated agent-activity DTO on /api/sessions. These
// tests prove the DTO keeps activity separate from lifecycle/health, flags stale
// records, disappears immediately after Delete, and is gated by sessions:read.

const s1dSID = "controlled_pty:s1"

// s1dSetup builds a Handlers with a live managed session and a wired status store.
func s1dSetup(t *testing.T) (*Handlers, *LifecycleService, *TelemetryService) {
	t.Helper()
	managed := newLSAdapter("controlled_pty", true, "s1")
	reg := mux.MustNewRegistry(managed)
	ctlAdapter, _ := reg.Adapter("controlled_pty")
	life := NewLifecycleService(NewOwnedPTYRuntime(ctlAdapter, nil, nil), nil, nil)
	seedCatalog(life, s1dSID, "controlled_pty", "s1", LifecycleRunning)
	ts := transcript.NewService(transcript.DefaultStoreConfig())
	telem := NewTelemetryService(reg, nil, nil, nil,
		NewApprovalStore(), nil, ts)
	life.SetStatusClearer(telem) // production Delete → status clear wiring
	h := &Handlers{Registry: reg, Events: nil, Telemetry: telem, Lifecycle: life}
	return h, life, telem
}

func seedWorking(st *AgentStatusStore) {
	st.Update(AgentStatusUpdate{SessionID: s1dSID, Generation: 1, Version: "0.144.1", Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev(s1dSID, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
}

// The DTO exposes advisory activity as a dimension SEPARATE from lifecycle and
// poll health; all three remain independently visible on the same row.
func TestS1D_AgentActivity_SeparateDimensions(t *testing.T) {
	h, _, telem := s1dSetup(t)
	seedWorking(telem.statusStore)

	row := getSessionsSnapshot(t, h)[s1dSID]
	if row.AgentActivity == nil {
		t.Fatal("agentActivity missing from row")
	}
	if row.AgentActivity.Status != string(agent.StatusWorking) {
		t.Errorf("activity.status = %q, want working", row.AgentActivity.Status)
	}
	if row.AgentActivity.Provenance != string(contract.ProvenanceNativeLog) {
		t.Errorf("activity.provenance = %q, want native_log", row.AgentActivity.Provenance)
	}
	if row.AgentActivity.ObservedAt == "" {
		t.Error("activity.observedAt empty")
	}
	// Lifecycle authority remains a SEPARATE, independently-present field.
	if row.LifecycleState != "running" {
		t.Errorf("lifecycleState = %q, want running (activity must not collapse it)", row.LifecycleState)
	}
	// The row's poll-health stale (dimension 3) is distinct from activity.stale.
	if row.Stale {
		t.Error("healthy row poll-stale should be false")
	}
	// No raw error/evidence leaks: the DTO has no such field by construction, and
	// the row's LastError stays empty for a healthy session.
	if row.LastError != "" {
		t.Errorf("unexpected lastError leak: %q", row.LastError)
	}
}

// A stale activity record is flagged stale AND keeps its status (never blanked),
// so the UI can avoid presenting it as current activity.
func TestS1D_AgentActivity_StaleFlagged(t *testing.T) {
	h, _, telem := s1dSetup(t)
	base := time.Unix(1_000_000, 0)
	telem.statusStore.now = func() time.Time { return base }
	telem.statusStore.staleAfter = 10 * time.Second
	telem.statusStore.Update(AgentStatusUpdate{SessionID: s1dSID, Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev(s1dSID, agent.EventThinking, contract.ProvenanceNativeLog, 0.9)}})
	// Advance the clock past the staleness horizon.
	telem.statusStore.now = func() time.Time { return base.Add(30 * time.Second) }

	row := getSessionsSnapshot(t, h)[s1dSID]
	if row.AgentActivity == nil || !row.AgentActivity.Stale {
		t.Fatalf("expected stale activity, got %+v", row.AgentActivity)
	}
	if row.AgentActivity.Status != string(agent.StatusThinking) {
		t.Errorf("stale activity blanked its status = %q, want thinking preserved", row.AgentActivity.Status)
	}
}

// The real authenticated DELETE route (HandleSessionDelete) removes the activity
// from the API immediately, and a recreated id does not resurrect the old activity
// — all observed through the production /api/sessions response.
func TestS1D_RealDeleteRouteRemovesActivity_APIObserved(t *testing.T) {
	h, life, telem := s1dSetup(t)
	seedWorking(telem.statusStore)
	seedCatalog(life, s1dSID, "controlled_pty", "s1", LifecycleExited) // terminal → deletable
	if getSessionsSnapshot(t, h)[s1dSID].AgentActivity == nil {
		t.Fatal("precondition: agentActivity must be present before delete")
	}

	// Drive the REAL authenticated DELETE route + real HandleSessionDelete handler.
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	rawDel, _, _, err := m.CreateAfterVerifiedChallenge("d", "host", "boot", []string{devicetrust.PermHistoryDelete})
	if err != nil {
		t.Fatalf("mint delete: %v", err)
	}
	delRoute := devicetrust.RequirePrincipal(m, h.HandleSessionDelete, devicetrust.PermHistoryDelete)
	req := httptest.NewRequest("DELETE", "/api/sessions/"+s1dSID, nil)
	req.SetPathValue("id", s1dSID)
	req.Header.Set("Authorization", "Bearer "+rawDel)
	rr := httptest.NewRecorder()
	delRoute(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("DELETE route: %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}

	// API observation: the activity is gone right after the delete.
	if row, ok := getSessionsSnapshot(t, h)[s1dSID]; ok && row.AgentActivity != nil {
		t.Errorf("agentActivity survived the real DELETE route: %+v", row.AgentActivity)
	}

	// Recreate the same id → fresh activity, no inheritance of the old status.
	telem.statusStore.Update(AgentStatusUpdate{SessionID: s1dSID, Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev(s1dSID, agent.EventThinking, contract.ProvenanceNativeLog, 0.9)}})
	row := getSessionsSnapshot(t, h)[s1dSID]
	if row.AgentActivity == nil || row.AgentActivity.Status != string(agent.StatusThinking) {
		t.Errorf("recreated activity = %+v, want thinking (fresh, no inheritance)", row.AgentActivity)
	}
}

// A device WITHOUT history:delete cannot delete (403), so activity is not removed.
func TestS1D_DeleteRoute_RequiresHistoryDeletePermission(t *testing.T) {
	h, life, telem := s1dSetup(t)
	seedWorking(telem.statusStore)
	seedCatalog(life, s1dSID, "controlled_pty", "s1", LifecycleExited)

	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	rawRead, _, _, _ := m.CreateAfterVerifiedChallenge("d", "host", "boot", devicetrust.PermissionsForRole(devicetrust.RoleMember))
	delRoute := devicetrust.RequirePrincipal(m, h.HandleSessionDelete, devicetrust.PermHistoryDelete)
	req := httptest.NewRequest("DELETE", "/api/sessions/"+s1dSID, nil)
	req.SetPathValue("id", s1dSID)
	req.Header.Set("Authorization", "Bearer "+rawRead) // member: no history:delete
	rr := httptest.NewRecorder()
	delRoute(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("delete without history:delete: %d, want 403", rr.Code)
	}
	if getSessionsSnapshot(t, h)[s1dSID].AgentActivity == nil {
		t.Error("activity wrongly removed by an unauthorized delete")
	}
}

// GET /api/sessions (production wrapper) enforces sessions:read: 401 without a
// token, 403 without the permission, 200 with it — and the 200 body carries the
// agentActivity DTO.
func TestS1D_SessionsReadAuthGate(t *testing.T) {
	h, _, telem := s1dSetup(t)
	seedWorking(telem.statusStore)
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	route := devicetrust.RequirePrincipal(m, h.HandleSessionsV2, devicetrust.PermSessionsRead)

	// 401 — missing token.
	rr := httptest.NewRecorder()
	route(rr, httptest.NewRequest("GET", "/api/sessions", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("missing token: %d, want 401", rr.Code)
	}

	// 401 — malformed bearer.
	reqBad := httptest.NewRequest("GET", "/api/sessions", nil)
	reqBad.Header.Set("Authorization", "Bearer not-a-real-token")
	rrBad := httptest.NewRecorder()
	route(rrBad, reqBad)
	if rrBad.Code != http.StatusUnauthorized {
		t.Errorf("malformed bearer: %d, want 401", rrBad.Code)
	}

	// 403 — a principal WITHOUT sessions:read.
	rawNoRead, _, _, err := m.CreateAfterVerifiedChallenge("d1", "host", "boot", []string{devicetrust.PermSessionsKill})
	if err != nil {
		t.Fatalf("mint no-read: %v", err)
	}
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+rawNoRead)
	rr = httptest.NewRecorder()
	route(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("no sessions:read: %d, want 403", rr.Code)
	}

	// 200 — a principal WITH sessions:read; body includes agentActivity.
	rawRead, _, _, err := m.CreateAfterVerifiedChallenge("d2", "host", "boot", devicetrust.PermissionsForRole(devicetrust.RoleMember))
	if err != nil {
		t.Fatalf("mint read: %v", err)
	}
	req = httptest.NewRequest("GET", "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+rawRead)
	rr = httptest.NewRecorder()
	route(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("valid read: %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var rows []SessionTelemetry
	if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.ID == s1dSID && r.AgentActivity != nil {
			found = true
		}
	}
	if !found {
		t.Error("authenticated 200 response missing agentActivity for the session")
	}
}

// D2: one session's activity never appears on another session's row.
func TestS1D_NoCrossSessionActivityLeak(t *testing.T) {
	// Two live managed sessions; only s1 has an activity record.
	managed := newLSAdapter("controlled_pty", true, "s1", "s2")
	reg := mux.MustNewRegistry(managed)
	ctlAdapter, _ := reg.Adapter("controlled_pty")
	life := NewLifecycleService(NewOwnedPTYRuntime(ctlAdapter, nil, nil), nil, nil)
	seedCatalog(life, "controlled_pty:s1", "controlled_pty", "s1", LifecycleRunning)
	seedCatalog(life, "controlled_pty:s2", "controlled_pty", "s2", LifecycleRunning)
	ts := transcript.NewService(transcript.DefaultStoreConfig())
	telem := NewTelemetryService(reg, nil, nil, nil,
		NewApprovalStore(), nil, ts)
	life.SetStatusClearer(telem)
	telem.statusStore.Update(AgentStatusUpdate{SessionID: "controlled_pty:s1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev("controlled_pty:s1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	h := &Handlers{Registry: reg, Events: nil, Telemetry: telem, Lifecycle: life}

	byID := getSessionsSnapshot(t, h)
	if byID["controlled_pty:s1"].AgentActivity == nil {
		t.Fatal("s1 should have activity")
	}
	if byID["controlled_pty:s2"].AgentActivity != nil {
		t.Errorf("s2 leaked activity from another session: %+v", byID["controlled_pty:s2"].AgentActivity)
	}
}

// D6/D9: the activity DTO carries its version + only bounded product fields, and
// never leaks raw errors, evidence, paths, or unexpected keys.
func TestS1D_AgentActivity_BoundedFieldsAndVersion(t *testing.T) {
	h, _, telem := s1dSetup(t)
	seedWorking(telem.statusStore)

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rr := httptest.NewRecorder()
	h.HandleSessionsAPI(rr, req)
	raw := rr.Body.String()

	// Version present and exact (the frozen T0 contract version, not a new one).
	if !contains(raw, `"contractVersion":"`+contract.ContractVersion+`"`) {
		t.Errorf("missing/incorrect activity contractVersion in %s", raw)
	}

	// Decode the activity object and assert its key set is exactly the allowed one.
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
		t.Fatalf("json: %v", err)
	}
	allowed := map[string]bool{
		"contractVersion": true, "status": true, "provenance": true,
		"confidence": true, "degraded": true, "observedAt": true, "stale": true,
	}
	for _, row := range rows {
		aa, ok := row["agentActivity"]
		if !ok {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(aa, &obj); err != nil {
			t.Fatalf("activity json: %v", err)
		}
		for k := range obj {
			if !allowed[k] {
				t.Errorf("agentActivity exposes unexpected field %q (possible private-data leak)", k)
			}
		}
	}
	// No lifecycle / private markers embedded in the activity projection.
	for _, bad := range []string{"lastError", "/Users/", "sk-", "rawRef", "cwd", "command"} {
		if contains(rawActivityOnly(t, rr.Body.Bytes()), bad) {
			t.Errorf("agentActivity leaked forbidden token %q", bad)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// rawActivityOnly extracts just the concatenated agentActivity objects so the
// forbidden-token scan targets the projection, not unrelated row fields.
func rawActivityOnly(t *testing.T, body []byte) string {
	t.Helper()
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("json: %v", err)
	}
	out := ""
	for _, row := range rows {
		if aa, ok := row["agentActivity"]; ok {
			out += string(aa)
		}
	}
	return out
}
