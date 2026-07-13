package term

import (
	"encoding/json"
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/transcript"
)

// S1.1-A production path — the winning-evidence Seq is preserved when the accepted
// adapter feeds the store through TelemetryService.processSession (the real S1-C
// wiring), and the public agentActivity DTO bytes are unchanged (WinningSeq is an
// internal reference, never serialized).

// The codex waiting_for_approval event resolves to waiting_approval; the store
// must bind a winner (HasWinningSeq) through the production path.
func TestS11A_ProductionPathPreservesWinner(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/codex.jsonl"
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
		`{"timestamp":"2026-07-06T13:29:37.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","approval_id":"appr-1"}}`,
	})
	sid := "controlled_pty:cdxA"
	svc, sess := s1cSvc(t, "codex", logPath, sid)
	defer transcript.RemoveLaunch(sid)
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"})

	s1cPoll(svc, sess, sid, "codex")

	rec, _, ok := svc.statusStore.Current(sid)
	if !ok {
		t.Fatal("no activity record via processSession")
	}
	if rec.Status != agent.StatusWaitingApproval {
		t.Fatalf("status=%q, want waiting_approval", rec.Status)
	}
	if !rec.HasWinningSeq {
		t.Errorf("production winner not bound: %+v (want HasWinningSeq=true)", rec)
	}
}

// The internal winning-evidence reference must NOT leak into the public DTO. The
// DTO golden bytes are asserted byte-for-byte against the frozen S1-E shape.
func TestS11A_DTOGoldenUnchanged_NoWinnerLeak(t *testing.T) {
	dto := AgentActivityDTO{
		ContractVersion: "t0.1",
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
	// Exact same golden the S1-E cross-language test asserts.
	if string(b) != s1eDTOGolden {
		t.Errorf("DTO drift after S1.1-A:\n got %s\nwant %s", b, s1eDTOGolden)
	}
	// Defensive: neither internal field name may appear in the wire bytes.
	for _, bad := range []string{"winningSeq", "WinningSeq", "hasWinningSeq", "HasWinningSeq"} {
		if containsStr(string(b), bad) {
			t.Errorf("internal winner field %q leaked into DTO bytes: %s", bad, b)
		}
	}
}

func containsStr(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
