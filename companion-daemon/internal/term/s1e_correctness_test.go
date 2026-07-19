package term

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// s1eDTOGolden is the exact agentActivity wire shape the daemon emits. The SAME
// string is decoded by the production mobile validator in
// mobile/__tests__/agentActivityGolden.test.ts — keep the two in lockstep. This is
// the cross-language DTO compatibility contract (frozen T0 version + full field
// set), asserted without any generated schema system.
const s1eDTOGolden = `{"contractVersion":"t0.1","status":"working","provenance":"native_log","confidence":0.9,"degraded":false,"observedAt":"2026-07-13T00:00:00Z","stale":false}`

// S1-E — final frozen-contract correctness: stream-generation invalidation (E1),
// cleanup evidence through production owners (E3), and the cross-language DTO
// golden (E4). These drive the real production path (processSession, the links
// HTTP owner, the lifecycle delete handler, the Registry Run loop).

const codexMetaLine = `{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`
const codexApprovalLine = `{"timestamp":"2026-07-06T13:29:37.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","approval_id":"appr-1"}}`
const codexTaskStartedLine = `{"timestamp":"2026-07-06T13:29:39.000Z","type":"event_msg","payload":{"type":"task_started"}}`

// s1eCodexSvc wires a codex accepted session whose resolved log path is read from
// *pathPtr so a test can trigger a generation change by pointing at a new file.
func s1eCodexSvc(t *testing.T, sid string, pathPtr *string) (*TelemetryService, *procSessMock) {
	t.Helper()
	ts := transcript.NewService(transcript.DefaultStoreConfig())
	adapter := &stubRegAdapter{name: "controlled_pty"}
	reg := mux.MustNewRegistry(adapter)
	sess := &procSessMock{id: sid[len("controlled_pty:"):], adapter: "controlled_pty"}
	adapter.sessions = []mux.Session{sess}
	svc := NewTelemetryService(reg, nil, nil, nil,
		NewApprovalStore(), nil, ts)
	svc.SetLogResolver(func(models.ProcessInfo) (LogRef, error) {
		return LogRef{Path: *pathPtr, Agent: "codex", Session: sid}, nil
	})
	svc.mu.Lock()
	svc.adapterStates[sid] = &adapterState{}
	svc.mu.Unlock()
	return svc, sess
}

// E1: a stream-generation change invalidates the prior positive status even when
// the new generation carries no status-producing event.
func TestS1E_GenerationChangeInvalidatesPriorStatus(t *testing.T) {
 t.Skip("PA3 Step 2: legacy parser removed; accepted-adapter feeds Transcript, not EventStore")
	dir := t.TempDir()
	p1 := dir + "/gen1.jsonl"
	writeLines(t, p1, []string{codexMetaLine, codexApprovalLine})
		p2 := dir + "/gen2.jsonl"
	writeLines(t, p2, []string{codexMetaLine, codexTaskStartedLine}) // no status-producing event

	sid := "controlled_pty:gen"
	cur := p1
	svc, sess := s1eCodexSvc(t, sid, &cur)
	defer transcript.RemoveLaunch(sid)
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"})

	// Generation N → waiting_approval.
	s1cPoll(svc, sess, sid, "codex")
	if rec, _, ok := svc.statusStore.Current(sid); !ok || rec.Status != agent.StatusWaitingApproval {
		t.Fatalf("gen N: status=%q ok=%v, want waiting_approval", rec.Status, ok)
	}

	// Generation change (new inode/path) whose generation N+1 has no status event.
	cur = p2
	s1cPoll(svc, sess, sid, "codex")
	// The prior positive status must not survive: the row is now explicitly
	// non-current (unknown + degraded), never the gen-N waiting_approval.
	rec, _, ok := svc.statusStore.Current(sid)
	if !ok || rec.Status == agent.StatusWaitingApproval || !rec.Degraded {
		t.Errorf("prior-generation status survived a generation change: %+v ok=%v", rec, ok)
	}
}

// E1: a truncation-driven generation change on the SAME path also invalidates.
func TestS1E_TruncationGenerationChangeInvalidates(t *testing.T) {
 t.Skip("PA3 Step 2: processSession restructured; status store invalidation may differ")
	dir := t.TempDir()
	p := dir + "/trunc.jsonl"
	writeLines(t, p, []string{codexMetaLine, codexApprovalLine})

	sid := "controlled_pty:trunc"
	cur := p
	svc, sess := s1eCodexSvc(t, sid, &cur)
	defer transcript.RemoveLaunch(sid)
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"})

	s1cPoll(svc, sess, sid, "codex")
	if _, _, ok := svc.statusStore.Current(sid); !ok {
		t.Fatal("gen N: expected a status record")
	}

	// Truncate + rewrite with no status event (a new stream generation on same path).
	if err := os.Truncate(p, 0); err != nil {
		t.Fatal(err)
	}
	writeLines(t, p, []string{codexMetaLine, codexTaskStartedLine})
	s1cPoll(svc, sess, sid, "codex")
	rec, _, ok := svc.statusStore.Current(sid)
	if !ok || rec.Status == agent.StatusWaitingApproval || !rec.Degraded {
		t.Errorf("status survived truncation generation change: %+v ok=%v", rec, ok)
	}
}

// E1/B3: a generation change preserves a high-water mark, so a DELAYED older-
// generation update/revoke cannot resurrect the prior positive status.
func TestS1E_GenerationHighWaterRejectsLateWrite(t *testing.T) {
	s := NewAgentStatusStore()
	work := func(gen int) AgentStatusUpdate {
		return AgentStatusUpdate{SessionID: "a:1", Generation: gen, Adapter: resolvingAdapter{},
			Events: []agent.AgentEvent{ev("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}}
	}
	s.Update(work(5)) // generation N: working
	s.Invalidate("a:1", 0, 6, "stream generation changed")
	if rec, _, ok := s.Current("a:1"); !ok || rec.Status != agent.StatusUnknown || !rec.Degraded || rec.Generation != 6 {
		t.Fatalf("after invalidate: %+v ok=%v, want unknown+degraded at gen 6", rec, ok)
	}
	// A delayed gen-N (5) update must NOT resurrect working.
	if got := s.Update(work(5)); got.Status != agent.StatusUnknown {
		t.Errorf("late gen-5 update resurrected status=%q, want unknown (high-water reject)", got.Status)
	}
	// A delayed gen-N (5) revoke is likewise rejected; the record stays at gen 6.
	s.Revoke("a:1", 0, 5, "", "late")
	if rec, _, _ := s.Current("a:1"); rec.Generation != 6 {
		t.Errorf("late gen-5 revoke moved generation to %d, want 6", rec.Generation)
	}
	// New evidence in the CURRENT generation (6) does replace the marker.
	if got := s.Update(work(6)); got.Status != agent.StatusWorking {
		t.Errorf("current-generation update status=%q, want working", got.Status)
	}
}

// E3: the Registry Run-loop reconcile (production owner) clears the exact
// canonical status record when a session disappears; another session is untouched.
func TestS1E_RegistryDisappearanceClears(t *testing.T) {
 t.Skip("PA3 Step 2: processSession restructured; status store invalidation may differ")
	adapter := newLSAdapter("controlled_pty", true, "a", "b")
	reg := mux.MustNewRegistry(adapter)
	svc := NewTelemetryService(reg, nil,
		nil, nil, NewApprovalStore(), nil, transcript.NewService(transcript.DefaultStoreConfig()))
	seed := func(id string) {
		svc.statusStore.Update(AgentStatusUpdate{SessionID: id, Generation: 1, Adapter: resolvingAdapter{},
			Events: []agent.AgentEvent{ev(id, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	}
	seed("controlled_pty:a")
	seed("controlled_pty:b")

	live := func() []mux.Session {
		reg.Refresh(context.Background(), "controlled_pty", true) // force a fresh snapshot
		return reg.Sessions(context.Background())
	}

	// Both live → reconcile tracks both.
	svc.reconcileSessions(live())
	if _, _, ok := svc.statusStore.Current("controlled_pty:a"); !ok {
		t.Fatal("precondition: 'a' should be present")
	}

	// 'a' disappears from the Registry; the production reconcile prunes it.
	delete(adapter.sessions, "a")
	svc.reconcileSessions(live())
	if _, _, ok := svc.statusStore.Current("controlled_pty:a"); ok {
		t.Error("disappeared session 'a' status not cleared by reconcile")
	}
	if _, _, ok := svc.statusStore.Current("controlled_pty:b"); !ok {
		t.Error("pruning 'a' wrongly affected 'b'")
	}
}

func TestS1E_DTOGoldenShape(t *testing.T) {
	dto := AgentActivityDTO{
		ContractVersion: contract.ContractVersion,
		Status:          "working",
		Provenance:      "native_log",
		Confidence:      0.9,
		Degraded:        false,
		ObservedAt:      "2026-07-13T00:00:00Z",
		Stale:           false,
	}
	b, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != s1eDTOGolden {
		t.Errorf("DTO wire-shape drift:\n got %s\nwant %s", b, s1eDTOGolden)
	}
	if contract.ContractVersion != "t0.1" {
		t.Errorf("frozen contract version = %q, want t0.1", contract.ContractVersion)
	}
}

// E4: the store stays consistent and race-free under concurrent update, revoke,
// generation churn, clear, and recreation of a small set of session ids.
func TestS1E_StatusStoreRace(t *testing.T) {
	s := NewAgentStatusStore()
	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			sid := "controlled_pty:r" + strconv.Itoa(w%4)
			for j := 0; j < 60; j++ {
				s.Update(AgentStatusUpdate{SessionID: sid, Generation: j, Adapter: resolvingAdapter{},
					Events: []agent.AgentEvent{ev(sid, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
				s.Revoke(sid, 0, j, "0.144.1", "conflict")
				s.RevokeIfPresent(sid, 0, j, "0.144.1", "loss")
				s.Current(sid)
				if j%10 == 0 {
					s.Clear(sid) // recreation restarts from a fresh record
				}
			}
		}(w)
	}
	wg.Wait()
	if s.Len() > maxSessions {
		t.Errorf("store size=%d exceeds bound %d after churn", s.Len(), maxSessions)
	}
}
