package term

import (
	"context"
	"os"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// S1-C — the accepted T1/T2 adapters feed the session-owned agent-activity store
// through the REAL production path (TelemetryService.processSession), reusing the
// same accepted-adapter batch/cursor/version/correlation as Transcript. These
// tests drive processSession, never the store helpers directly.

// s1cSvc wires a TelemetryService with a real transcript service + the S1 store,
// a stub registry session, and a log resolver pointing at logPath for agentKind.
func s1cSvc(t *testing.T, agentKind, logPath, sid string) (*TelemetryService, *procSessMock) {
	t.Helper()
	ts := transcript.NewService(transcript.DefaultStoreConfig())
	adapter := &stubRegAdapter{name: "controlled_pty"}
	reg := mux.MustNewRegistry(adapter)
	sess := &procSessMock{id: sid[len("controlled_pty:"):], adapter: "controlled_pty"}
	adapter.sessions = []mux.Session{sess}
	svc := NewTelemetryService(reg, NewMemoryEventStore(), nil, nil,
		NewApprovalStore(), NewActivityBuffer(100), ts)
	svc.SetLogResolver(func(models.ProcessInfo) (LogRef, error) {
		return LogRef{Path: logPath, Agent: agentKind, Session: sid}, nil
	})
	svc.mu.Lock()
	svc.sessions[sid] = &sessionStateData{LastActivity: time.Now(), State: "idle"}
	svc.mu.Unlock()
	return svc, sess
}

func s1cPoll(svc *TelemetryService, sess *procSessMock, sid, cmd string) {
	svc.processSession(context.Background(), sess,
		map[string]models.ProcessInfo{sid: {PID: 1234, Command: cmd}},
		map[string]bool{"controlled_pty": false}, map[string]bool{})
}

// Codex success: waiting_for_approval → waiting_approval, native_log, not degraded.
func TestS1C_CodexWaitingApproval_ProductionPath(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/codex.jsonl"
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
		`{"timestamp":"2026-07-06T13:29:37.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","approval_id":"appr-1"}}`,
	})
	sid := "controlled_pty:cdx1"
	svc, sess := s1cSvc(t, "codex", logPath, sid)
	defer transcript.RemoveLaunch(sid)
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"})

	s1cPoll(svc, sess, sid, "codex")

	rec, stale, ok := svc.statusStore.Current(sid)
	if !ok {
		t.Fatal("no S1 activity record produced via processSession")
	}
	t.Logf("codex rec: %+v (stale=%v)", rec, stale)
	if rec.Status != agent.StatusWaitingApproval {
		t.Errorf("status=%q, want waiting_approval", rec.Status)
	}
	if rec.Provenance != contract.ProvenanceNativeLog {
		t.Errorf("provenance=%q, want native_log", rec.Provenance)
	}
	if rec.Degraded {
		t.Errorf("healthy waiting_approval must not be degraded: %q", rec.DegradedReason)
	}
	if rec.SessionID != sid {
		t.Errorf("sessionID=%q, want %q", rec.SessionID, sid)
	}
	if rec.Confidence <= 0.5 {
		t.Errorf("confidence=%v, want > 0.5 (native_log not advisory-capped)", rec.Confidence)
	}
	if stale {
		t.Error("fresh record must not be stale")
	}
}

// Claude success: assistant tool_use → working.
func TestS1C_ClaudeWorking_ProductionPath(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/claude.jsonl"
	writeLines(t, logPath, []string{
		`{"type":"user","version":"2.1.202","message":{"role":"user","content":"hi"},"sessionId":"s","uuid":"u0","timestamp":"2026-07-06T13:29:35.399Z"}`,
		`{"type":"assistant","version":"2.1.202","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]},"sessionId":"s","uuid":"u1","timestamp":"2026-07-06T13:29:37.000Z"}`,
	})
	sid := "controlled_pty:cla1"
	svc, sess := s1cSvc(t, "claude", logPath, sid)
	defer transcript.RemoveLaunch(sid)
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid, Provider: "claude", Adapter: "controlled_pty", Version: "2.1.202"})

	s1cPoll(svc, sess, sid, "claude")

	rec, _, ok := svc.statusStore.Current(sid)
	if !ok {
		t.Fatal("no S1 activity record produced for claude via processSession")
	}
	t.Logf("claude rec: %+v", rec)
	if rec.Status != agent.StatusWorking {
		t.Errorf("status=%q, want working", rec.Status)
	}
	if rec.Provenance != contract.ProvenanceNativeLog {
		t.Errorf("provenance=%q, want native_log", rec.Provenance)
	}
	if rec.Degraded {
		t.Errorf("working must not be degraded: %q", rec.DegradedReason)
	}
}

// Version conflict → authority revoked to unknown + degraded (Revoke path).
func TestS1C_VersionConflict_RevokesToUnknownDegraded(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/codex_bad.jsonl"
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"9.9.9"}}`,
		`{"timestamp":"2026-07-06T13:29:37.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","approval_id":"appr-1"}}`,
	})
	sid := "controlled_pty:cdx2"
	svc, sess := s1cSvc(t, "codex", logPath, sid)
	defer transcript.RemoveLaunch(sid)
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"})

	s1cPoll(svc, sess, sid, "codex")

	rec, _, ok := svc.statusStore.Current(sid)
	if !ok {
		t.Fatal("version conflict should still record a (degraded) result")
	}
	t.Logf("conflict rec: %+v", rec)
	if rec.Status != agent.StatusUnknown || !rec.Degraded {
		t.Errorf("version conflict: status=%q degraded=%v, want unknown/degraded", rec.Status, rec.Degraded)
	}
}

// Unavailable correlation (no launch binding) → no authoritative record.
func TestS1C_UnavailableCorrelation_NoRecord(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/codex_uncorr.jsonl"
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
		`{"timestamp":"2026-07-06T13:29:37.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","approval_id":"appr-1"}}`,
	})
	sid := "controlled_pty:cdx3"
	svc, sess := s1cSvc(t, "codex", logPath, sid)
	// No RegisterLaunch → correlation is Unavailable.

	s1cPoll(svc, sess, sid, "codex")

	if rec, _, ok := svc.statusStore.Current(sid); ok {
		t.Errorf("uncorrelated session must have no activity record, got %+v", rec)
	}
}

// A non-status-only poll must not fabricate or downgrade a status (record stays absent).
func TestS1C_NonStatusEventsOnly_NoRecord(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/codex_nonstatus.jsonl"
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
		`{"timestamp":"2026-07-06T13:29:37.000Z","type":"response_item","payload":{"role":"assistant","content":"hi"}}`,
	})
	sid := "controlled_pty:cdx4"
	svc, sess := s1cSvc(t, "codex", logPath, sid)
	defer transcript.RemoveLaunch(sid)
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"})

	s1cPoll(svc, sess, sid, "codex")

	if rec, _, ok := svc.statusStore.Current(sid); ok {
		t.Errorf("non-status events must not create a record, got %+v", rec)
	}
}

// Incremental polling: a second poll delivering only a non-status event leaves the
// prior status intact (no downgrade), and lifecycle stays uncoupled.
func TestS1C_IncrementalPoll_LeavesPrior(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/codex_inc.jsonl"
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
		`{"timestamp":"2026-07-06T13:29:37.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","approval_id":"appr-1"}}`,
	})
	sid := "controlled_pty:cdx5"
	svc, sess := s1cSvc(t, "codex", logPath, sid)
	defer transcript.RemoveLaunch(sid)
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"})

	s1cPoll(svc, sess, sid, "codex")
	first, _, ok := svc.statusStore.Current(sid)
	if !ok || first.Status != agent.StatusWaitingApproval {
		t.Fatalf("first poll: status=%q ok=%v, want waiting_approval", first.Status, ok)
	}

	// Append a non-status event and poll again.
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("{\"timestamp\":\"2026-07-06T13:29:39.000Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\"}}\n")
	f.Close()

	s1cPoll(svc, sess, sid, "codex")
	second, _, ok := svc.statusStore.Current(sid)
	if !ok || second.Status != agent.StatusWaitingApproval {
		t.Errorf("second poll downgraded prior: status=%q, want waiting_approval (unchanged)", second.Status)
	}
}

// B6a: a previously-valid session that LOSES correlation is revoked to unknown+degraded.
func TestS1C_CorrelationLoss_Revokes(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/codex_cl.jsonl"
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
		`{"timestamp":"2026-07-06T13:29:37.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","approval_id":"appr-1"}}`,
	})
	sid := "controlled_pty:cdxloss"
	svc, sess := s1cSvc(t, "codex", logPath, sid)
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"})

	s1cPoll(svc, sess, sid, "codex")
	if first, _, ok := svc.statusStore.Current(sid); !ok || first.Status != agent.StatusWaitingApproval {
		t.Fatalf("poll 1: status=%q ok=%v, want waiting_approval", first.Status, ok)
	}

	// Correlation lost (binding removed) — the next poll must revoke the prior status.
	transcript.RemoveLaunch(sid)
	s1cPoll(svc, sess, sid, "codex")
	rec, _, ok := svc.statusStore.Current(sid)
	if !ok || rec.Status != agent.StatusUnknown || !rec.Degraded {
		t.Errorf("after correlation loss: status=%q degraded=%v, want unknown/degraded", rec.Status, rec.Degraded)
	}
}

// B6c: the real production Delete handler clears the S1 status immediately, and a
// recreated session with the same id does not inherit it.
func TestS1C_DeleteHandlerClearsStatus_NoInherit(t *testing.T) {
	managed := newLSAdapter("controlled_pty", true, "d1")
	reg := mux.MustNewRegistry(managed)
	life := NewLifecycleService(NewOwnedPTYRuntime(reg, NewActivityBuffer(50), nil), NewActivityBuffer(50), nil)
	ts := transcript.NewService(transcript.DefaultStoreConfig())
	telem := NewTelemetryService(reg, NewMemoryEventStore(), nil, nil,
		NewApprovalStore(), NewActivityBuffer(100), ts)
	life.SetStatusClearer(telem) // production wiring (mirrors app.go)

	sid := "controlled_pty:d1"
	// Seed a real activity record through the store's production API.
	telem.statusStore.Update(AgentStatusUpdate{SessionID: sid, Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev(sid, agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9)}})
	if _, _, ok := telem.statusStore.Current(sid); !ok {
		t.Fatal("precondition: status record must exist before delete")
	}

	// Terminal catalog row so Delete is permitted, then run the REAL Delete handler.
	seedCatalog(life, sid, "controlled_pty", "d1", LifecycleExited)
	if _, err := life.Delete(context.Background(), sid); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, ok := telem.statusStore.Current(sid); ok {
		t.Error("Delete handler did not clear the S1 status record")
	}

	// Recreate the same id → must start fresh (no inheritance of a prior status).
	rec := telem.statusStore.Update(AgentStatusUpdate{SessionID: sid, Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{ev(sid, agent.EventThinking, contract.ProvenanceNativeLog, 0.9)}})
	if rec.Status != agent.StatusThinking {
		t.Errorf("recreated session status=%q, want thinking (fresh, no inheritance)", rec.Status)
	}
}

// B4 (production): one-shot and incremental polling converge on the same final status.
func TestS1C_OneShotEqualsIncremental_Claude(t *testing.T) {
	thinkingLine := `{"type":"assistant","version":"2.1.202","message":{"role":"assistant","content":[{"type":"thinking","thinking":"x"}]},"sessionId":"s","uuid":"u1","timestamp":"2026-07-06T13:29:36.000Z"}`
	toolLine := `{"type":"assistant","version":"2.1.202","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]},"sessionId":"s","uuid":"u2","timestamp":"2026-07-06T13:29:37.000Z"}`
	userLine := `{"type":"user","version":"2.1.202","message":{"role":"user","content":"hi"},"sessionId":"s","uuid":"u0","timestamp":"2026-07-06T13:29:35.399Z"}`

	// One-shot: all three lines present in a single poll.
	dir1 := t.TempDir()
	p1 := dir1 + "/one.jsonl"
	writeLines(t, p1, []string{userLine, thinkingLine, toolLine})
	sid1 := "controlled_pty:one"
	svc1, sess1 := s1cSvc(t, "claude", p1, sid1)
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid1, Provider: "claude", Adapter: "controlled_pty", Version: "2.1.202"})
	defer transcript.RemoveLaunch(sid1)
	s1cPoll(svc1, sess1, sid1, "claude")
	one, _, ok := svc1.statusStore.Current(sid1)
	if !ok || one.Status != agent.StatusWorking {
		t.Fatalf("one-shot: status=%q ok=%v, want working", one.Status, ok)
	}

	// Incremental: thinking first, then append tool_use and poll again.
	dir2 := t.TempDir()
	p2 := dir2 + "/inc.jsonl"
	writeLines(t, p2, []string{userLine, thinkingLine})
	sid2 := "controlled_pty:inc"
	svc2, sess2 := s1cSvc(t, "claude", p2, sid2)
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid2, Provider: "claude", Adapter: "controlled_pty", Version: "2.1.202"})
	defer transcript.RemoveLaunch(sid2)
	s1cPoll(svc2, sess2, sid2, "claude")
	if mid, _, _ := svc2.statusStore.Current(sid2); mid.Status != agent.StatusThinking {
		t.Fatalf("incremental step 1: status=%q, want thinking", mid.Status)
	}
	f, err := os.OpenFile(p2, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(toolLine + "\n")
	f.Close()
	s1cPoll(svc2, sess2, sid2, "claude")
	inc, _, ok := svc2.statusStore.Current(sid2)
	if !ok || inc.Status != agent.StatusWorking {
		t.Errorf("incremental final: status=%q, want working (== one-shot)", inc.Status)
	}
}
