package projection

import (
	"testing"

	"devremote/companion-daemon/internal/agent"
	agentcontract "devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/timeline/contract"
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
	s := NewProjector(w).Snapshot(nil)
	if s.Collisions != 1 {
		t.Fatalf("collision verdict: %#v", s)
	}
	b := matrixBinding("s", 1, e.EventID)
	if r := Compare(matrixResponse("s", 1, matrixSegment("s", e.T0Event.ID, 1)), s, []FixtureEpochBinding{b}); r.Passed || r.Collisions != 1 {
		t.Fatalf("collision report: %#v", r)
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
	w := testWriter(t)
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	sid := "restore-m"
	bs := make([]FixtureEpochBinding, 0, 3)
	for i, gen := range []int64{1, 2, 1} {
		if i == 0 {
			svc.EnableQueue(sid)
		} else {
			svc.ReplaceTranscript(sid)
		}
		svc.SetCorrelation(sid, matrixCorrelationProvider(sid, "claude"))
		e := envelope(t, string(rune(0x4300+i)), contract.EventProviderInvocationStarted, agent.EventAgentStarted, sid, gen)
		e.Provider, e.T0Event.AgentKind, e.EventID = "claude", "claude", ""
		e, _ = contract.NewEnvelope(e)
		if i == 2 {
			e.SourceIncarnation = "incarnation-" + string(rune(0x4300))
			e.EventID = ""
			e, _ = contract.NewEnvelope(e)
		}
		svc.ProjectAgentEvents(sid, []agent.AgentEvent{e.T0Event})
		appendEnv(t, w, e)
		resp := svc.BuildResponse(sid, svc.ListTranscript(sid))
		b := FixtureEpochBinding{EpochOccurrence: uint64(i + 1), SessionID: sid, TranscriptGeneration: resp.Generation, RuntimeID: "runtime-" + sid, LaunchGeneration: gen, TimelineEventIDs: []string{e.EventID}}
		bs = append(bs, b)
		if r := Compare(resp, NewProjector(w).Snapshot(bs), bs); !r.Passed {
			t.Fatalf("epoch %d: %#v", i, r)
		}
	}
	if !strictOccurrences(bs) || bs[0].RuntimeID != bs[2].RuntimeID || bs[0].LaunchGeneration != bs[2].LaunchGeneration || bs[0].TranscriptGeneration == bs[2].TranscriptGeneration {
		t.Fatal("restore binding invalid")
	}
}
func TestMatrix08Missing(t *testing.T) {
	b := matrixBinding("s", 1, "e")
	if r := Compare(matrixResponse("s", 1, matrixSegment("s", "a", 1)), Snapshot{}, []FixtureEpochBinding{b}); r.Passed || r.Missings != 1 {
		t.Fatalf("missing verdict: %#v", r)
	}
}
func TestMatrix09ApprovalPairOrder(t *testing.T) {
	b := FixtureEpochBinding{EpochOccurrence: 1, SessionID: "s", TranscriptGeneration: 1, RuntimeID: "r", LaunchGeneration: 1, TimelineEventIDs: []string{"a1", "a2"}}
	items := []TranscriptItem{{EventID: "a1", AgentEventRef: "a1", SessionID: "s", AgentKind: "codex", EventType: "approval_requested", Text: "Approval requested", PairID: "a", RuntimeID: "r", LaunchGeneration: 1, ProjectionOrder: 0}, {EventID: "a2", AgentEventRef: "a2", SessionID: "s", AgentKind: "codex", EventType: "approval_resolved", Text: "Approval resolved", PairID: "a", RuntimeID: "r", LaunchGeneration: 1, ProjectionOrder: 1}}
	resp := matrixResponse("s", 1, transcript.TranscriptSegment{SessionID: "s", Kind: transcript.KindAgentEvent, Source: transcript.SourceAgentEvent, AgentEventRef: "a1", AgentKind: "codex", EventType: "approval_requested", Text: "Approval requested", Seq: 1}, transcript.TranscriptSegment{SessionID: "s", Kind: transcript.KindAgentEvent, Source: transcript.SourceAgentEvent, AgentEventRef: "a2", AgentKind: "codex", EventType: "approval_resolved", Text: "Approval resolved", Seq: 2})
	if !Compare(resp, Snapshot{Transcript: items}, []FixtureEpochBinding{b}).Passed {
		t.Fatal("approval control")
	}
	badResp := resp
	badResp.Semantic = append([]transcript.TranscriptSegment(nil), resp.Semantic...)
	badResp.Semantic[1].Seq = 1
	if r := Compare(badResp, Snapshot{Transcript: items}, []FixtureEpochBinding{b}); r.Passed || r.OrderingDivergences == 0 {
		t.Fatalf("approval transcript seq: %#v", r)
	}
}
func TestMatrix10ToolPairOrder(t *testing.T) {
	b := FixtureEpochBinding{EpochOccurrence: 1, SessionID: "s", TranscriptGeneration: 1, RuntimeID: "r", LaunchGeneration: 1, TimelineEventIDs: []string{"t1", "t2"}}
	items := []TranscriptItem{{EventID: "t1", AgentEventRef: "t1", SessionID: "s", AgentKind: "codex", EventType: "tool_call_started", ToolName: "x", PairID: "t", RuntimeID: "r", LaunchGeneration: 1, ProjectionOrder: 0}, {EventID: "t2", AgentEventRef: "t2", SessionID: "s", AgentKind: "codex", EventType: "tool_call_finished", ToolName: "x", PairID: "t", RuntimeID: "r", LaunchGeneration: 1, ProjectionOrder: 1}}
	resp := matrixResponse("s", 1, transcript.TranscriptSegment{SessionID: "s", Kind: transcript.KindAgentEvent, Source: transcript.SourceAgentEvent, AgentEventRef: "t1", AgentKind: "codex", EventType: "tool_call_started", ToolName: "x", Seq: 1}, transcript.TranscriptSegment{SessionID: "s", Kind: transcript.KindAgentEvent, Source: transcript.SourceAgentEvent, AgentEventRef: "t2", AgentKind: "codex", EventType: "tool_call_finished", ToolName: "x", Seq: 2})
	if !Compare(resp, Snapshot{Transcript: items}, []FixtureEpochBinding{b}).Passed {
		t.Fatal("tool control")
	}
	badResp := resp
	badResp.Semantic = append([]transcript.TranscriptSegment(nil), resp.Semantic...)
	badResp.Semantic[0], badResp.Semantic[1] = badResp.Semantic[1], badResp.Semantic[0]
	badResp.Semantic[0].Seq, badResp.Semantic[1].Seq = 2, 1
	if r := Compare(badResp, Snapshot{Transcript: items}, []FixtureEpochBinding{b}); r.Passed || r.OrderingDivergences == 0 || r.Unexplained == 0 {
		t.Fatalf("tool transcript reorder: %#v", r)
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
func TestMatrix13UnknownEventKind(t *testing.T) {
	w := testWriter(t)
	e := envelope(t, "unknown-boundary", contract.EventProviderInvocationStarted, agent.EventAgentStarted, "s", 1)
	e.EventKind = contract.EventKind("unknown_boundary")
	if w.Append(e) || w.Stats().Dropped == 0 || len(w.ReadRecent(1)) != 0 {
		t.Fatal("writer accepted unknown event kind")
	}
	b := matrixBinding("s", 1, "unknown")
	invalid := TranscriptItem{EventID: "unknown", AgentEventRef: "agent-unknown-boundary", SessionID: "s", AgentKind: "codex", EventType: "unknown_event", RuntimeID: "runtime-s", LaunchGeneration: 1}
	if r := Compare(matrixResponse("s", 1, matrixSegment("s", "agent-unknown-boundary", 1)), Snapshot{Transcript: []TranscriptItem{invalid}}, []FixtureEpochBinding{b}); r.Passed || r.Unexplained == 0 || r.Missings != 0 || r.GenerationMismatches != 0 {
		t.Fatalf("unknown oracle boundary: %#v", r)
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
func matrixCorrelationProvider(s, provider string) transcript.CorrelationState {
	return transcript.CorrelationState{SessionID: s, Correlation: agentcontract.CorrelationProven, Provider: provider}
}
