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
	svc := NewTelemetryService(reg, NewMemoryEventStore(), NewNopLinkStore(), nil, nil,
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
	transcript.RegisterLaunch(sid, "codex", "controlled_pty", "0.144.1", 0, 1)

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
	transcript.RegisterLaunch(sid, "claude", "controlled_pty", "2.1.202", 0, 1)

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
	transcript.RegisterLaunch(sid, "codex", "controlled_pty", "0.144.1", 0, 1)

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
	transcript.RegisterLaunch(sid, "codex", "controlled_pty", "0.144.1", 0, 1)

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
	transcript.RegisterLaunch(sid, "codex", "controlled_pty", "0.144.1", 0, 1)

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
