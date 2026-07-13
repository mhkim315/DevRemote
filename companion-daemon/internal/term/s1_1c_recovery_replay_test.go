package term

import (
	"strconv"
	"sync"
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/transcript"
)

// S1.1-C — recovery, replay, and bounded-state hardening. Builds on S1.1-A's
// winning-evidence Seq and S1.1-B's (launch, stream) epoch. The invariant: within
// one epoch the winning revision is monotonic — a same-generation replay, cursor
// rewind, or repeated batch cannot move it backward or restore an older positive
// status after a downgrade; recovery needs FRESH evidence or a NEW epoch. Eviction
// and delete drop ALL associated internal evidence state.

// upd is a positive Update at (launchGen, streamGen) whose winning event has Seq.
func updSeq(sid string, launchGen int64, streamGen int, t agent.AgentEventType, seq int64) AgentStatusUpdate {
	return AgentStatusUpdate{SessionID: sid, LaunchGen: launchGen, Generation: streamGen, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{evSeq(sid, t, contract.ProvenanceNativeLog, 0.9, seq)}}
}

// C1: newest Seq wins, then a replay of an OLDER Seq in the same epoch is rejected
// (the winner does not move backward).
func TestS11C_ReplayOfOlderSeqRejected(t *testing.T) {
	s := NewAgentStatusStore()
	sid := "controlled_pty:c1"
	s.Update(updSeq(sid, 1, 1, agent.EventThinking, 10))        // winner Seq 10
	s.Update(updSeq(sid, 1, 1, agent.EventToolCallStarted, 20)) // winner Seq 20 (working)
	if rec, _, _ := s.Current(sid); rec.Status != agent.StatusWorking || rec.WinningSeq != 20 {
		t.Fatalf("after seq 20: %+v, want working/20", rec)
	}
	// Replay the older Seq-10 thinking event in the same epoch → rejected.
	got := s.Update(updSeq(sid, 1, 1, agent.EventThinking, 10))
	if got.Status != agent.StatusWorking || got.WinningSeq != 20 {
		t.Errorf("replay of older Seq moved the winner: %+v, want working/20 (unchanged)", got)
	}
}

// C2: a cursor rewind / repeated batch (same Seq re-delivered) does not regress
// the winner and is idempotent.
func TestS11C_RepeatedBatchIdempotent(t *testing.T) {
	s := NewAgentStatusStore()
	sid := "controlled_pty:c2"
	s.Update(updSeq(sid, 1, 1, agent.EventToolCallStarted, 5))
	first, _, _ := s.Current(sid)
	// Re-deliver the exact same batch three times.
	for i := 0; i < 3; i++ {
		s.Update(updSeq(sid, 1, 1, agent.EventToolCallStarted, 5))
	}
	again, _, _ := s.Current(sid)
	if again.Status != agent.StatusWorking || again.WinningSeq != 5 || again.WinnerHighWater != first.WinnerHighWater {
		t.Errorf("repeated batch changed state: first=%+v again=%+v", first, again)
	}
}

// C3: correlation loss → non-current; recovery requires FRESH evidence (a higher
// Seq). A downgrade preserves the epoch winner high-water, so replaying the SAME
// pre-loss Seq after recovery does NOT restore the old positive status, but a
// strictly newer Seq does.
func TestS11C_CorrelationLossThenFreshRecovery(t *testing.T) {
	s := NewAgentStatusStore()
	sid := "controlled_pty:c3"
	s.Update(updSeq(sid, 1, 1, agent.EventToolCallStarted, 7)) // working, winner 7
	// Correlation loss (same epoch, launchGen 0 downgrade as production does).
	if rec, ok := s.RevokeIfPresent(sid, 0, 1, "", "correlation unavailable"); !ok || rec.Status != agent.StatusUnknown || !rec.Degraded {
		t.Fatalf("after loss: %+v ok=%v, want unknown/degraded", rec, ok)
	}
	// Stale re-delivery of the SAME Seq 7 must NOT restore working.
	if got := s.Update(updSeq(sid, 1, 1, agent.EventToolCallStarted, 7)); got.Status == agent.StatusWorking {
		t.Errorf("stale Seq-7 re-delivery restored working after loss: %+v", got)
	}
	// FRESH evidence at a higher Seq recovers a positive status.
	if got := s.Update(updSeq(sid, 1, 1, agent.EventThinking, 12)); got.Status != agent.StatusThinking || got.WinningSeq != 12 {
		t.Errorf("fresh Seq-12 recovery: %+v, want thinking/12", got)
	}
}

// C4: loss followed by INCOMPATIBLE recovery (uncorrelated downgrade repeated)
// remains non-current — no positive status is fabricated.
func TestS11C_LossThenIncompatibleStaysNonCurrent(t *testing.T) {
	s := NewAgentStatusStore()
	sid := "controlled_pty:c4"
	s.Update(updSeq(sid, 1, 1, agent.EventToolCallStarted, 7))
	s.RevokeIfPresent(sid, 0, 1, "", "correlation unavailable")
	// Repeated incompatible (still-uncorrelated) polls: stays unknown+degraded.
	for i := 0; i < 3; i++ {
		s.RevokeIfPresent(sid, 0, 1, "", "correlation unavailable")
	}
	if rec, _, _ := s.Current(sid); rec.Status != agent.StatusUnknown || !rec.Degraded {
		t.Errorf("incompatible recovery: %+v, want unknown/degraded (non-current)", rec)
	}
}

// C5: a NEW epoch (higher stream generation) resets the winner high-water, so an
// event whose Seq is below the prior epoch's winner is accepted in the new epoch.
// This models a truncation/relink that legitimately restarts the event stream.
func TestS11C_NewEpochResetsWinnerHighWater(t *testing.T) {
	s := NewAgentStatusStore()
	sid := "controlled_pty:c5"
	s.Update(updSeq(sid, 1, 1, agent.EventToolCallStarted, 50)) // epoch(1,1) winner 50
	// New stream generation: the stream restarts; low Seq is legitimate again.
	s.Invalidate(sid, 1, 2, "stream generation changed")
	got := s.Update(updSeq(sid, 1, 2, agent.EventThinking, 3))
	if got.Status != agent.StatusThinking || got.WinningSeq != 3 {
		t.Errorf("new epoch rejected a legitimate low Seq: %+v, want thinking/3", got)
	}
}

// C6: a new LAUNCH epoch likewise resets the winner high-water.
func TestS11C_NewLaunchResetsWinnerHighWater(t *testing.T) {
	s := NewAgentStatusStore()
	sid := "controlled_pty:c6"
	s.Update(updSeq(sid, 1, 5, agent.EventToolCallStarted, 50))
	s.Invalidate(sid, 2, 0, "launch binding replaced") // new launch epoch
	got := s.Update(updSeq(sid, 2, 1, agent.EventThinking, 3))
	if got.Status != agent.StatusThinking || got.WinningSeq != 3 {
		t.Errorf("new launch epoch rejected a legitimate low Seq: %+v, want thinking/3", got)
	}
}

// C7: eviction under churn removes ALL associated internal evidence state, and a
// same-ID reuse after eviction carries no old winner/high-water metadata.
func TestS11C_EvictionThenReuseNoStaleEvidence(t *testing.T) {
	s := NewAgentStatusStore()
	sid := "controlled_pty:evict"
	// Seed one record with a high winner, then evict it by exceeding the bound.
	s.Update(updSeq(sid, 1, 1, agent.EventToolCallStarted, 999))
	for i := 0; i < maxSessions+5; i++ {
		other := "controlled_pty:o" + strconv.Itoa(i)
		s.Update(updSeq(other, 1, 1, agent.EventToolCallStarted, 1))
	}
	if _, _, ok := s.Current(sid); ok {
		t.Fatal("target should have been evicted under churn")
	}
	// Reuse the same id: it must start with NO carried high-water — a low Seq wins.
	rec := s.Update(updSeq(sid, 1, 1, agent.EventThinking, 2))
	if rec.Status != agent.StatusThinking || rec.WinningSeq != 2 || rec.WinnerHighWater != 2 {
		t.Errorf("reused id inherited stale evidence: %+v, want thinking/2/hw2", rec)
	}
}

// C8: Clear (lifecycle/history delete) drops all evidence; a recreated session at
// a fresh epoch carries no old winner high-water.
func TestS11C_ClearDropsAllEvidence(t *testing.T) {
	s := NewAgentStatusStore()
	sid := "controlled_pty:clr"
	s.Update(updSeq(sid, 1, 1, agent.EventToolCallStarted, 500))
	s.Clear(sid)
	if _, _, ok := s.Current(sid); ok {
		t.Fatal("Clear did not remove the record")
	}
	rec := s.Update(updSeq(sid, 1, 1, agent.EventThinking, 1))
	if rec.Status != agent.StatusThinking || rec.WinningSeq != 1 || rec.WinnerHighWater != 1 {
		t.Errorf("recreated after Clear inherited evidence: %+v, want thinking/1/hw1", rec)
	}
}

// C9: concurrent update / revoke / invalidate / clear / recreate across a small id
// set stays race-free and bounded (run with -race).
func TestS11C_ConcurrentEpochChurnRace(t *testing.T) {
	s := NewAgentStatusStore()
	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			sid := "controlled_pty:cc" + strconv.Itoa(w%4)
			for j := 0; j < 80; j++ {
				launch := int64(j/10 + 1)
				s.Update(updSeq(sid, launch, j, agent.EventToolCallStarted, int64(j)))
				s.Revoke(sid, launch, j, "0.144.1", "conflict")
				s.RevokeIfPresent(sid, 0, j, "0.144.1", "loss")
				s.Invalidate(sid, launch+1, 0, "launch replaced")
				s.Current(sid)
				if j%15 == 0 {
					s.Clear(sid)
				}
			}
		}(w)
	}
	wg.Wait()
	if s.Len() > maxSessions {
		t.Errorf("store size=%d exceeds bound %d after churn", s.Len(), maxSessions)
	}
}

// C10 (restart): a fresh TelemetryService (as after a daemon restart) begins with
// an EMPTY status store and cannot reconstruct a confident current status from
// stale process/session identity alone — it stays absent until a NEW poll produces
// fresh correlated evidence for the current launch. The store is in-memory by
// design; no persistence reconstructs a prior positive status.
func TestS11C_RestartBeginsAbsentUntilFreshEvidence(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/codex.jsonl"
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
		`{"timestamp":"2026-07-06T13:29:37.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","approval_id":"appr-1"}}`,
	})
	sid := "controlled_pty:restart"

	// "After restart": a brand-new service with a fresh (empty) store.
	svc, sess := s1cSvc(t, "codex", logPath, sid)
	defer transcript.RemoveLaunch(sid)

	// Before any poll, the store is absent — no confident status survives a restart.
	if _, _, ok := svc.statusStore.Current(sid); ok {
		t.Fatal("fresh (post-restart) store must be absent before any poll")
	}

	// The launch is re-registered for the current instance and a poll runs; only
	// then does fresh correlated evidence produce a status.
	transcript.RegisterLaunch(transcript.LaunchSpec{
		SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1",
	})
	s1cPoll(svc, sess, sid, "codex")
	rec, _, ok := svc.statusStore.Current(sid)
	if !ok || rec.Status != agent.StatusWaitingApproval {
		t.Errorf("post-restart fresh evidence: %+v ok=%v, want waiting_approval", rec, ok)
	}
}
