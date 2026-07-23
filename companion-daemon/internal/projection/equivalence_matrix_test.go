package projection

import (
	"testing"

	"devremote/companion-daemon/internal/agent"
	agentcontract "devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/timeline/contract"
	"devremote/companion-daemon/internal/timeline/writer"
	"devremote/companion-daemon/internal/transcript"
)

// The names deliberately mirror STEP9_2_CONTRACT.md §4: one acceptance test
// per row, each with a clean control and a fail-closed counterexample.
func TestMatrix01EmptyWriter(t *testing.T) {
	b := matrixBinding("s", 1, "e")
	r := Compare(matrixResponse("s", 1, matrixSegment("s", "a", 1)), Snapshot{}, []FixtureEpochBinding{b})
	if r.Passed || r.Missings == 0 {
		t.Fatalf("missing verdict: %#v", r)
	}
}

func TestMatrix02KnownSequenceAndClosedDualFeed(t *testing.T) {
	w := testWriter(t)
	const sid = "matrix-known"
	cases := []struct {
		kind contract.EventKind
		typ  agent.AgentEventType
	}{
		{contract.EventProviderInvocationStarted, agent.EventAgentStarted}, {contract.EventProviderInvocationFinished, agent.EventCompleted}, {contract.EventProviderInvocationFinished, agent.EventFailed}, {contract.EventProviderInvocationFinished, agent.EventInterrupted},
		{contract.EventToolCallStarted, agent.EventToolCallStarted}, {contract.EventToolCallFinished, agent.EventToolCallFinished}, {contract.EventApprovalRequested, agent.EventApprovalRequested}, {contract.EventApprovalResolved, agent.EventApprovalResolved}, {contract.EventStreamObserved, agent.EventThinking}, {contract.EventStreamObserved, agent.EventThinking},
	}
	events := make([]agent.AgentEvent, 0, len(cases))
	ids := make([]string, 0, len(cases))
	for i, c := range cases {
		e := envelope(t, string(rune(0x4100+i)), c.kind, c.typ, sid, 1)
		if c.kind == contract.EventToolCallFinished {
			e.References.ToolCall.ID = "tool-pair"
			e.EventID = ""
			var err error
			e, err = contract.NewEnvelope(e)
			if err != nil {
				t.Fatal(err)
			}
		}
		if c.kind == contract.EventToolCallStarted {
			e.References.ToolCall.ID = "tool-pair"
			e.EventID = ""
			var err error
			e, err = contract.NewEnvelope(e)
			if err != nil {
				t.Fatal(err)
			}
		}
		if c.kind == contract.EventApprovalRequested || c.kind == contract.EventApprovalResolved {
			e.References.ApprovalRequest.ID = "approval-pair"
			e.EventID = ""
			var err error
			e, err = contract.NewEnvelope(e)
			if err != nil {
				t.Fatal(err)
			}
		}
		appendEnv(t, w, e)
		events, ids = append(events, e.T0Event), append(ids, e.EventID)
	}
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	svc.EnableQueue(sid)
	svc.SetCorrelation(sid, matrixCorrelation(sid))
	svc.ProjectAgentEvents(sid, events)
	resp := svc.BuildResponse(sid, svc.ListTranscript(sid))
	b := FixtureEpochBinding{EpochOccurrence: 1, SessionID: sid, TranscriptGeneration: resp.Generation, RuntimeID: "runtime-" + sid, LaunchGeneration: 1, TimelineEventIDs: ids}
	snap := NewProjector(w).Snapshot([]FixtureEpochBinding{b})
	if report := Compare(resp, snap, []FixtureEpochBinding{b}); len(snap.Activity) != 10 || !report.Passed {
		t.Fatalf("closed mapping control failed: %#v", report)
	}
	bad := snap
	bad.Transcript = append([]TranscriptItem(nil), snap.Transcript...)
	bad.Transcript[1].ProjectionOrder = -1
	if r := Compare(resp, bad, []FixtureEpochBinding{b}); r.Passed || r.OrderingDivergences == 0 {
		t.Fatalf("reorder verdict: %#v", r)
	}
}

func TestMatrix03ExactReplay(t *testing.T) {
	w := testWriter(t)
	e := envelope(t, "replay-m", contract.EventProviderInvocationStarted, agent.EventAgentStarted, "s", 1)
	appendEnv(t, w, e)
	appendEnv(t, w, e)
	if s := NewProjector(w).Snapshot(nil); len(s.Activity) != 1 || s.Collisions != 0 {
		t.Fatalf("replay control: %#v", s)
	}
}
func TestMatrix04Collision(t *testing.T) {
	w := testWriter(t)
	e := envelope(t, "collision-m", contract.EventProviderInvocationStarted, agent.EventAgentStarted, "s", 1)
	appendEnv(t, w, e)
	x := e
	x.Payload.Redacted = &contract.RedactedPayload{Summary: "other"}
	x.EventID = ""
	x, _ = contract.NewEnvelope(x)
	appendEnv(t, w, x)
	if s := NewProjector(w).Snapshot(nil); s.Collisions != 1 {
		t.Fatalf("collision verdict: %#v", s)
	}
}
func TestMatrix05RingWrap(t *testing.T) {
	w := testWriter(t)
	ids := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		e := envelope(t, string(rune(0x4200+i)), contract.EventStreamObserved, agent.EventThinking, "s", 1)
		ids = append(ids, e.EventID)
		appendEnv(t, w, e)
	}
	b := FixtureEpochBinding{EpochOccurrence: 1, SessionID: "s", RuntimeID: "runtime-s", LaunchGeneration: 1, TimelineEventIDs: ids}
	if s := NewProjector(w).Snapshot([]FixtureEpochBinding{b}); s.RingOverwritten != 72 || len(s.Gaps) != 1 {
		t.Fatalf("wrap control: %#v", s)
	}
	if NewProjector(w).Snapshot(nil).Unexplained == 0 {
		t.Fatal("unscoped wrap passed")
	}
}
func TestMatrix06NewWriter(t *testing.T) {
	w := testWriter(t)
	b := matrixBinding("new", 1, "e")
	if r := Compare(matrixResponse("new", 1, matrixSegment("new", "a", 1)), NewProjector(w).Snapshot([]FixtureEpochBinding{b}), []FixtureEpochBinding{b}); r.Passed || r.Missings == 0 {
		t.Fatalf("new writer verdict: %#v", r)
	}
}
func TestMatrix07GenerationRestore(t *testing.T) {
	bs := []FixtureEpochBinding{{EpochOccurrence: 1, SessionID: "s", TranscriptGeneration: 1, RuntimeID: "r", LaunchGeneration: 1}, {EpochOccurrence: 2, SessionID: "s", TranscriptGeneration: 2, RuntimeID: "r2", LaunchGeneration: 2}, {EpochOccurrence: 3, SessionID: "s", TranscriptGeneration: 3, RuntimeID: "r", LaunchGeneration: 1}}
	if !strictOccurrences(bs) {
		t.Fatal("N→N+1→restored-N rejected")
	}
	bad := append([]FixtureEpochBinding(nil), bs...)
	bad[2].EpochOccurrence = 1
	if strictOccurrences(bad) {
		t.Fatal("merged restored N accepted")
	}
}
func TestMatrix08Missing(t *testing.T) {
	b := matrixBinding("s", 1, "e")
	if r := Compare(matrixResponse("s", 1, matrixSegment("s", "a", 1)), Snapshot{}, []FixtureEpochBinding{b}); r.Passed || r.Missings != 1 {
		t.Fatalf("missing verdict: %#v", r)
	}
}
func TestMatrix09ApprovalPairOrder(t *testing.T) {
	good := []TranscriptItem{{EventType: "approval_requested", PairID: "a"}, {EventType: "approval_resolved", PairID: "a"}}
	if ValidatePairOrder(good) != nil {
		t.Fatal("approval control")
	}
	if ValidatePairOrder([]TranscriptItem{good[1], good[0]}) == nil {
		t.Fatal("approval reorder accepted")
	}
}
func TestMatrix10ToolPairOrder(t *testing.T) {
	good := []TranscriptItem{{EventType: "tool_call_started", PairID: "t"}, {EventType: "tool_call_finished", PairID: "t"}}
	if ValidatePairOrder(good) != nil {
		t.Fatal("tool control")
	}
	if ValidatePairOrder([]TranscriptItem{{EventType: "tool_call_started", PairID: "t"}, {EventType: "tool_call_finished", PairID: "x"}}) == nil {
		t.Fatal("unmatched tool accepted")
	}
}
func TestMatrix11ApprovalBinding(t *testing.T) {
	b := matrixBinding("s", 1, "e")
	item := TranscriptItem{EventID: "e", AgentEventRef: "a", SessionID: "s", AgentKind: "codex", EventType: "approval_requested", Text: "Approval requested", RuntimeID: "runtime-s", LaunchGeneration: 1}
	r := Compare(matrixResponse("s", 1, transcript.TranscriptSegment{SessionID: "wrong", Kind: transcript.KindAgentEvent, Source: transcript.SourceAgentEvent, AgentEventRef: "a", AgentKind: "codex", EventType: "approval_requested", Text: "Approval requested", Seq: 1}), Snapshot{Transcript: []TranscriptItem{item}}, []FixtureEpochBinding{b})
	if r.MisboundApprovals == 0 {
		t.Fatalf("approval binding verdict: %#v", r)
	}
}
func TestMatrix12Degradation(t *testing.T) {
	b := matrixBinding("s", 1, "lost")
	g := GapMarker{SessionID: "s", RuntimeID: "runtime-s", LaunchGeneration: 1, EpochOccurrence: 1, GlobalDroppedAfter: 1, DegradedReason: "write", Reason: "writer_drop"}
	s := Snapshot{Gaps: []GapMarker{g}, Stats: writerStats(1), Degraded: true, DegradedReason: "write"}
	good := matrixResponse("s", 1, matrixSegment("s", "a", 1), transcript.TranscriptSegment{SessionID: "s", Kind: transcript.KindDegraded, DegradedReason: "writer_drop"})
	if !Compare(good, s, []FixtureEpochBinding{b}).Passed {
		t.Fatal("gap control")
	}
	if Compare(matrixResponse("s", 1, matrixSegment("s", "a", 1)), s, []FixtureEpochBinding{b}).Passed {
		t.Fatal("unmatched gap passed")
	}
}
func TestMatrix13UnknownEventKind(t *testing.T) {
	w := testWriter(t)
	e := envelope(t, "unknown-boundary", contract.EventProviderInvocationStarted, agent.EventAgentStarted, "s", 1)
	e.EventKind = contract.EventKind("unknown_boundary")
	if w.Append(e) || w.Stats().Dropped == 0 || len(w.ReadRecent(1)) != 0 {
		t.Fatal("writer accepted unknown event kind")
	}
}

func matrixBinding(s string, gen int64, id string) FixtureEpochBinding {
	return FixtureEpochBinding{EpochOccurrence: 1, SessionID: s, TranscriptGeneration: gen, RuntimeID: "runtime-" + s, LaunchGeneration: 1, TimelineEventIDs: []string{id}}
}
func matrixSegment(s, ref string, seq int64) transcript.TranscriptSegment {
	return transcript.TranscriptSegment{SessionID: s, Kind: transcript.KindAgentEvent, Source: transcript.SourceAgentEvent, AgentEventRef: ref, AgentKind: "codex", EventType: "agent_started", Text: "Agent started", Seq: seq}
}
func matrixResponse(s string, gen int64, segs ...transcript.TranscriptSegment) transcript.TranscriptResponse {
	return transcript.TranscriptResponse{SessionID: s, Generation: gen, ContractVersion: transcript.ContractVersion, Semantic: segs}
}
func matrixCorrelation(s string) transcript.CorrelationState {
	return transcript.CorrelationState{SessionID: s, Correlation: agentcontract.CorrelationProven, Provider: "codex"}
}
func writerStats(d uint64) writer.Stats { return writer.Stats{Dropped: d} }
