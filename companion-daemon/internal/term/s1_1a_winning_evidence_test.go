package term

import (
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// S1.1-A — bounded winning-evidence metadata. The store binds each resolved
// positive status to the exact accepted AgentEvent.Seq that won, located from the
// resolver's OWN output over the accepted latest-Seq ordering (never a second
// precedence implementation). unknown/degraded/revoked results carry NO winner.
//
// These tests exercise the store's production Update/Revoke/Invalidate API — the
// same entry points processSession calls — and the parallel S1C/S1E production
// path tests below prove the wired path preserves the winner.

// A1: stronger authoritative evidence wins and the bound WinningSeq identifies
// that exact winner (higher Seq, same strong provenance, latest wins on tie).
func TestS11A_WinnerSeqIdentifiesExactWinner(t *testing.T) {
	s := NewAgentStatusStore()
	rec := s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{
			evSeq("a:1", agent.EventThinking, contract.ProvenanceNativeLog, 0.85, 10),
			evSeq("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.85, 20),
		}})
	if rec.Status != agent.StatusWorking {
		t.Fatalf("status=%q, want working (latest Seq)", rec.Status)
	}
	if !rec.HasWinningSeq || rec.WinningSeq != 20 {
		t.Errorf("winningSeq=%d has=%v, want 20/true (the working event at seq 20)", rec.WinningSeq, rec.HasWinningSeq)
	}
}

// A2: equal precedence/confidence uses the accepted latest-Seq rule, and the
// winner reference points at the latest-Seq candidate (not the earlier one).
func TestS11A_EqualPrecedenceBindsLatestSeq(t *testing.T) {
	s := NewAgentStatusStore()
	// Two working events, identical provenance+confidence, different Seq.
	rec := s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{
			evSeq("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.8, 4),
			evSeq("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.8, 7),
		}})
	if rec.Status != agent.StatusWorking || !rec.HasWinningSeq {
		t.Fatalf("rec=%+v, want working with a winner", rec)
	}
	if rec.WinningSeq != 7 {
		t.Errorf("winningSeq=%d, want 7 (latest under the accepted tie rule)", rec.WinningSeq)
	}
}

// A3: input order and one-shot vs incremental presentation do not change the
// winning reference — both converge on the same (status, winningSeq).
func TestS11A_OrderIndependentWinner(t *testing.T) {
	mk := func() []agent.AgentEvent {
		return []agent.AgentEvent{
			evSeq("a:1", agent.EventThinking, contract.ProvenanceNativeLog, 0.85, 1),
			evSeq("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.85, 2),
		}
	}
	// Forward order.
	fwd := NewAgentStatusStore().Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1,
		Adapter: resolvingAdapter{}, Events: mk()})
	// Reversed batch order — same events, listed newest-first.
	rev := mk()
	rev[0], rev[1] = rev[1], rev[0]
	revRec := NewAgentStatusStore().Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1,
		Adapter: resolvingAdapter{}, Events: rev})
	// Incremental: thinking then tool_use in two polls.
	inc := NewAgentStatusStore()
	inc.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{evSeq("a:1", agent.EventThinking, contract.ProvenanceNativeLog, 0.85, 1)}})
	incRec := inc.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{evSeq("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.85, 2)}})

	for name, r := range map[string]AgentActivityRecord{"forward": fwd, "reversed": revRec, "incremental": incRec} {
		if r.Status != agent.StatusWorking || !r.HasWinningSeq || r.WinningSeq != 2 {
			t.Errorf("%s: status=%q winningSeq=%d has=%v, want working/2/true", name, r.Status, r.WinningSeq, r.HasWinningSeq)
		}
	}
}

// A4: cross-session and unknown-provenance events cannot become a winning
// reference — they are filtered/dropped before evidence and produce no record.
func TestS11A_CrossSessionAndUnknownNeverWin(t *testing.T) {
	// Cross-session: no record at all.
	cs := NewAgentStatusStore()
	cs.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{evSeq("b:2", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9, 5)}})
	if _, _, ok := cs.Current("a:1"); ok {
		t.Error("cross-session event produced a record/winner")
	}
	// Unknown provenance: dropped, no typed status, no winner.
	up := NewAgentStatusStore()
	up.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{evSeq("a:1", agent.EventToolCallStarted, contract.Provenance("bogus"), 0.9, 5)}})
	if _, _, ok := up.Current("a:1"); ok {
		t.Error("unknown-provenance event produced a record/winner")
	}
}

// A5: degraded / adapter-error / version-conflict / revoke results do not
// fabricate a winner (HasWinningSeq stays false).
func TestS11A_DegradedAndRevokeCarryNoWinner(t *testing.T) {
	// Adapter error → unknown+degraded, no winner.
	er := NewAgentStatusStore()
	rec := er.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: errorAdapter{},
		Events: []agent.AgentEvent{evSeq("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9, 9)}})
	if rec.Status != agent.StatusUnknown || !rec.Degraded || rec.HasWinningSeq {
		t.Errorf("adapter error: %+v, want unknown/degraded and NO winner", rec)
	}
	// Advisory terminal claim downgraded to unknown by ResolveStatus → no winner.
	adv := NewAgentStatusStore()
	arec := adv.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{evSeq("a:1", agent.EventCompleted, contract.ProvenanceHeuristic, 0.9, 3)}})
	if arec.Status != agent.StatusUnknown || arec.HasWinningSeq {
		t.Errorf("advisory-terminal downgrade: %+v, want unknown and NO winner", arec)
	}
	// Explicit Revoke → unknown+degraded, no winner.
	rv := NewAgentStatusStore()
	rv.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 2, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{evSeq("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9, 8)}})
	rrec := rv.Revoke("a:1", 2, "2.1.202", "accepted version conflict")
	if rrec.HasWinningSeq {
		t.Errorf("revoke fabricated a winner: %+v", rrec)
	}
	// Invalidate (stream-generation change) → no winner either.
	irec := rv.Invalidate("a:1", 3, "stream generation changed")
	if irec.HasWinningSeq {
		t.Errorf("invalidate fabricated a winner: %+v", irec)
	}
}

// A6: a no-status-evidence poll leaves the prior record — including its winning
// reference — untouched (no downgrade, no winner loss).
func TestS11A_NoEvidencePollPreservesWinner(t *testing.T) {
	s := NewAgentStatusStore()
	first := s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{evSeq("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.9, 42)}})
	if !first.HasWinningSeq || first.WinningSeq != 42 {
		t.Fatalf("precondition: winningSeq=%d has=%v, want 42/true", first.WinningSeq, first.HasWinningSeq)
	}
	// Poll with only a non-status event.
	s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{evSeq("a:1", agent.EventAgentStarted, contract.ProvenanceNativeLog, 0.9, 43)}})
	cur, _, ok := s.Current("a:1")
	if !ok || cur.Status != agent.StatusWorking || !cur.HasWinningSeq || cur.WinningSeq != 42 {
		t.Errorf("no-evidence poll changed winner: %+v", cur)
	}
}

// A7: a strong winner supersedes a prior winner and updates the reference to the
// new winning Seq (records the exact latest authoritative event).
func TestS11A_StrongerEvidenceUpdatesWinnerSeq(t *testing.T) {
	s := NewAgentStatusStore()
	s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{evSeq("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.8, 5)}})
	// Later completed event at a higher Seq wins and rebinds.
	rec := s.Update(AgentStatusUpdate{SessionID: "a:1", Generation: 1, Adapter: resolvingAdapter{},
		Events: []agent.AgentEvent{
			evSeq("a:1", agent.EventToolCallStarted, contract.ProvenanceNativeLog, 0.8, 5),
			evSeq("a:1", agent.EventCompleted, contract.ProvenanceNativeLog, 0.8, 9),
		}})
	if rec.Status != agent.StatusCompleted || !rec.HasWinningSeq || rec.WinningSeq != 9 {
		t.Errorf("rebind: %+v, want completed/9/true", rec)
	}
}
