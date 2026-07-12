package v2_1_202

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// newAdapter builds a healthy Claude adapter for conformance testing.
func newAdapter(t *testing.T) contract.AgentAdapter {
	t.Helper()
	return &Adapter{}
}

// failingAdapter builds an adapter whose ReadEvents always returns degraded.
func failingAdapter(t *testing.T) contract.AgentAdapter {
	t.Helper()
	return &Adapter{failRead: true}
}

// ── Fixtures ──

// claudeRec builds a RawRecord from a Claude JSONL line with native-log
// provenance and JSONL source.
func claudeRec(line string) contract.RawRecord {
	return contract.RawRecord{
		Bytes:      []byte(line),
		Source:     agent.SourceJSONL,
		Provenance: contract.ProvenanceNativeLog,
	}
}

// versionMeta2_1_202 is a realistic user record that carries the top-level
// version field "2.1.202". The batch version authority is derived from
// this record.
func versionMeta2_1_202() contract.RawRecord {
	return claudeRec(`{"parentUuid":null,"isSidechain":false,"promptId":"<UUID>","type":"user","message":{"role":"user","content":"<PROMPT>"},"uuid":"<UUID>","timestamp":"2026-07-06T23:23:31.876Z","permissionMode":"default","origin":{"kind":"human"},"promptSource":"typed","userType":"external","entrypoint":"cli","cwd":"<HOME>/<PROJECT>","sessionId":"<UUID>","version":"2.1.202"}`)
}

func claudeFixtures() contract.ConformanceFixtures {
	// Valid records: user_message + assistant thinking + assistant tool_use.
	// First record carries version 2.1.202 for version gating.
	validRecords := []contract.RawRecord{
		versionMeta2_1_202(),
		claudeRec(`{"parentUuid":"<UUID>","isSidechain":false,"message":{"id":"<UUID>","type":"message","role":"assistant","model":"<MODEL>","content":[{"type":"thinking","thinking":"<REDACTED_THINKING>","signature":"<UUID>"}],"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":99999,"output_tokens":999}},"type":"assistant","uuid":"<UUID>","timestamp":"2026-07-06T23:23:38.360Z","userType":"external","entrypoint":"cli","cwd":"<HOME>/<PROJECT>","sessionId":"<UUID>","version":"2.1.202"}`),
		claudeRec(`{"parentUuid":"<UUID>","isSidechain":false,"message":{"id":"<UUID>","type":"message","role":"assistant","model":"<MODEL>","content":[{"type":"tool_use","id":"<UUID>","name":"Bash","input":{"command":"<CMD>","description":"<REDACTED_DESCRIPTION>"}}],"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":99999,"output_tokens":999}},"type":"assistant","uuid":"<UUID>","timestamp":"2026-07-06T23:23:39.398Z","userType":"external","entrypoint":"cli","cwd":"<HOME>/<PROJECT>","sessionId":"<UUID>","version":"2.1.202"}`),
	}

	// Malformed records: unknown type, missing fields, garbage.
	malformedRecords := []contract.RawRecord{
		claudeRec(`{"type":"unknown_event_type","sessionId":"<UUID>","unexpectedField":"<REDACTED>"}`),
		claudeRec(`{"type":"user","sessionId":"<UUID>"}`),
		claudeRec(`{}`),
		{},
		{Bytes: []byte("{"), Source: agent.SourceJSONL},
		{Bytes: []byte("\x00\xff not json"), Source: agent.SourceJSONL},
		{Bytes: nil},
	}

	// Synthetic approval records (harness-only, NOT from retained evidence).
	// See §9 and the adapter package doc.  These represent what a Claude
	// permission-request cycle WOULD look like with a stable ApprovalID.
	approvalRecords := []contract.RawRecord{
		versionMeta2_1_202(),
		claudeRec(`{"parentUuid":"<UUID>","isSidechain":false,"type":"user","message":{"role":"user","content":[{"type":"permission_request","id":"appr-claude-001","message":"<REDACTED_PERMISSION_REQUEST>"}]},"uuid":"<UUID>","timestamp":"2026-07-06T23:30:00.000Z","sessionId":"<UUID>","version":"2.1.202"}`),
		claudeRec(`{"parentUuid":"<UUID>","isSidechain":false,"type":"user_resolved","message":{"role":"user","content":[{"type":"permission_result","id":"appr-claude-001","resolution":"approved"}]},"uuid":"<UUID>","timestamp":"2026-07-06T23:30:05.000Z","sessionId":"<UUID>","version":"2.1.202"}`),
	}

	// Near-miss: records that look approval-ish but must yield ZERO approvals.
	// permission-mode=ask is NOT approval evidence (§9).
	nearMissRecords := []contract.RawRecord{
		claudeRec(`{"parentUuid":"<UUID>","isSidechain":false,"type":"permission-mode","permissionMode":"ask","sessionId":"<UUID>"}`),
		claudeRec(`{"parentUuid":"<UUID>","isSidechain":false,"message":{"id":"<UUID>","type":"message","role":"assistant","model":"<MODEL>","content":[{"type":"text","text":"I need your approval to proceed"}],"stop_reason":"end_turn"},"type":"assistant","uuid":"<UUID>","timestamp":"2026-07-06T23:24:05.000Z","userType":"external","entrypoint":"cli","cwd":"<HOME>/<PROJECT>","sessionId":"<UUID>","version":"2.1.202"}`),
	}

	return contract.ConformanceFixtures{
		DetectContext:    contract.SessionContext{SessionID: "pokit:host-a", ProcessName: "claude", CWD: "/Users/dev/.claude/projects/myproject"},
		ExpectDetectKind: "claude",

		ValidRecords: validRecords,
		ExpectTypes:  []contract.AgentEventType{agent.EventUserMessage, agent.EventThinking, agent.EventToolCallStarted},

		// Distinct records for bounds/dedupe/cursor checks.
		DistinctRecord: func(i int) contract.RawRecord {
			ts := "2026-07-06T23:23:38." + nanoPad(i) + "Z"
			return claudeRec(`{"parentUuid":"<UUID>","isSidechain":false,"message":{"id":"<UUID>","type":"message","role":"assistant","model":"<MODEL>","content":[{"type":"thinking","thinking":"<REDACTED_THINKING>","signature":"<UUID>"}],"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":99999,"output_tokens":999}},"type":"assistant","uuid":"distinct-` + strconv.Itoa(i) + `","timestamp":"` + ts + `","userType":"external","entrypoint":"cli","cwd":"<HOME>/<PROJECT>","sessionId":"<UUID>","version":"2.1.202"}`)
		},

		// SizedRecord for batch-byte boundary test.
		SizedRecord: func(size int) contract.RawRecord {
			pad := size - 190
			if pad < 0 {
				pad = 0
			}
			b := make([]byte, pad)
			for i := range b {
				b[i] = byte('a' + (i % 26))
			}
			return claudeRec(`{"parentUuid":"<UUID>","isSidechain":false,"message":{"id":"<UUID>","type":"message","role":"assistant","model":"<MODEL>","content":[{"type":"thinking","thinking":"<REDACTED>","signature":"<UUID>"}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}},"type":"assistant","uuid":"sz-` + strconv.Itoa(size) + `","timestamp":"2026-07-06T23:23:38.000Z","userType":"external","entrypoint":"cli","cwd":"<HOME>/<PROJECT>","sessionId":"<UUID>","version":"2.1.202","_pad":"` + string(b) + `"}`)
		},

		FailingFactory:   failingAdapter,
		ApprovalRecords:  approvalRecords,
		NearMissRecords:  nearMissRecords,
		MalformedRecords: malformedRecords,
	}
}

func nanoPad(i int) string {
	s := strconv.Itoa(i)
	for len(s) < 9 {
		s = "0" + s
	}
	return s
}

// ── Contract conformance ──

func TestClaudeAdapter_Conformance(t *testing.T) {
	contract.RunAgentContract(t, "claude", func(t *testing.T) contract.AgentAdapter {
		return &Adapter{}
	}, claudeFixtures())
}

// ── DTO snapshot stability ──

func TestClaudeAdapter_DTOSnapshotStable(t *testing.T) {
	a := &Adapter{}
	res, err := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: claudeFixtures().ValidRecords,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) < 2 {
		t.Fatalf("expected >=2 events from valid records, got %d", len(res.Events))
	}

	got := toJSON(t, res.Events)
	checks := []string{
		`"type":"user_message"`,
		`"type":"thinking"`,
		`"type":"tool_call_started"`,
		`"provenance":"native_log"`,
		`"sessionId":"pokit:host-a"`,
		`"agentKind":"claude"`,
	}
	for _, ck := range checks {
		if !contains(got, ck) {
			t.Errorf("DTO missing %s", ck)
		}
	}

	// Second read must produce identical JSON.
	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: claudeFixtures().ValidRecords,
	})
	if toJSON(t, res.Events) != toJSON(t, res2.Events) {
		t.Error("normalization is not deterministic across fresh reads")
	}
}

// ── Version gate ──

func TestClaudeAdapter_VersionGate_ExactMatch(t *testing.T) {
	a := &Adapter{}
	rec := versionMeta2_1_202()
	ev, deg := a.NormalizeEvent(context.Background(), rec)
	if deg.Degraded {
		t.Errorf("exact version 2.1.202 must not degrade: %+v", deg)
	}
	if ev.Type != agent.EventUserMessage {
		t.Errorf("exact version: got %q, want user_message", ev.Type)
	}
	if ev.Confidence < 0.8 {
		t.Errorf("exact version confidence too low: %.2f", ev.Confidence)
	}
}

func TestClaudeAdapter_VersionGate_NewerVersion(t *testing.T) {
	a := &Adapter{}
	rec := claudeRec(`{"type":"user","message":{"role":"user","content":"x"},"uuid":"u1","timestamp":"2026-07-06T23:23:31.876Z","sessionId":"s1","version":"9.9.9"}`)
	ev, deg := a.NormalizeEvent(context.Background(), rec)
	if ev.Type != agent.EventUnknown {
		t.Errorf("newer version: got %q, want EventUnknown", ev.Type)
	}
	if !deg.Degraded {
		t.Error("newer version must degrade")
	}
}

func TestClaudeAdapter_VersionGate_MissingVersion(t *testing.T) {
	a := &Adapter{}
	// Record with NO version field.  NormalizeEvent alone does not see
	// batch context and can produce a typed event (matching the Codex
	// adapter pattern).  Version enforcement is at the ReadEvents batch
	// level, proved by TestClaudeAdapter_NoVersion_AllUnknown.
	rec := claudeRec(`{"type":"user","message":{"role":"user","content":"x"},"uuid":"u1","timestamp":"2026-07-06T23:23:31.876Z","sessionId":"s1"}`)
	ev, _ := a.NormalizeEvent(context.Background(), rec)
	// NormalizeEvent on a record without version info: the adapter
	// classifies it by structure alone.  Version gating is the caller's
	// responsibility to enforce via ReadEvents batch context.
	if ev.Type != agent.EventUserMessage {
		t.Errorf("missing version (NormalizeEvent): got %q, want user_message (version gate is batch-level)", ev.Type)
	}
	if err := contract.ValidateEvent(ev); err != nil {
		t.Errorf("emitted event fails ValidateEvent: %v", err)
	}
	// ReadEvents with the same record must produce EventUnknown (no version
	// confirmation at batch level).
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: []contract.RawRecord{rec},
	})
	if len(res.Events) != 1 {
		t.Fatalf("ReadEvents: got %d events, want 1", len(res.Events))
	}
	if res.Events[0].Type != agent.EventUnknown {
		t.Errorf("ReadEvents no-version: got %q, want EventUnknown", res.Events[0].Type)
	}
	if !res.Degraded.Degraded {
		t.Error("ReadEvents no-version must degrade")
	}
}

func TestClaudeAdapter_VersionGate_NonStringVersion(t *testing.T) {
	a := &Adapter{}
	// Version is a number, not a string — must not match "2.1.202".
	rec := claudeRec(`{"type":"user","message":{"role":"user","content":"x"},"uuid":"u1","timestamp":"2026-07-06T23:23:31.876Z","sessionId":"s1","version":2}`)
	ev, deg := a.NormalizeEvent(context.Background(), rec)
	if ev.Type != agent.EventUnknown {
		t.Errorf("non-string version: got %q, want EventUnknown", ev.Type)
	}
	if !deg.Degraded {
		t.Error("non-string version must degrade")
	}
}

func TestClaudeAdapter_VersionGate_BatchMismatch(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		claudeRec(`{"type":"user","message":{"role":"user","content":"x"},"uuid":"u1","timestamp":"2026-07-06T23:23:31.876Z","sessionId":"s1","version":"9.9.9"}`),
		claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"s"}]},"uuid":"u2","timestamp":"2026-07-06T23:23:38.360Z","sessionId":"s1","version":"2.1.202"}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	for _, e := range res.Events {
		if e.Type != agent.EventUnknown {
			t.Errorf("version-mismatch batch: event %q, want EventUnknown", e.Type)
		}
	}
	if !res.Degraded.Degraded {
		t.Error("version-mismatch batch must be degraded")
	}
}

func TestClaudeAdapter_VersionGate_ConflictingMidStream(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		versionMeta2_1_202(),
		claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"s"}]},"uuid":"u2","timestamp":"2026-07-06T23:23:38.360Z","sessionId":"s1","version":"2.1.202"}`),
		claudeRec(`{"type":"user","message":{"role":"user","content":"x"},"uuid":"u3","timestamp":"2026-07-06T23:23:50.000Z","sessionId":"s1","version":"9.9.9"}`),
		claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]},"uuid":"u4","timestamp":"2026-07-06T23:23:55.000Z","sessionId":"s1","version":"2.1.202"}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	// Events before conflict (pos 0, 1) emitted. Cursor stops at conflict pos 2.
	if len(res.Events) != 2 {
		t.Errorf("conflicting mid-stream: got %d events, want 2 (events before conflict)", len(res.Events))
	}
	if !res.Degraded.Degraded {
		t.Error("conflicting mid-stream must degrade")
	}
	cur, _ := parseCursor(res.NextCursor)
	if cur.nextPos != 2 {
		t.Errorf("cursor at %d, want 2 (stopped at conflict)", cur.nextPos)
	}
	// Re-read at conflict: still degraded, 0 events.
	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  res.NextCursor,
	})
	if len(res2.Events) != 0 {
		t.Errorf("re-read at conflict: got %d events, want 0", len(res2.Events))
	}
	if !res2.Degraded.Degraded {
		t.Error("re-read at conflict must remain degraded")
	}
}

// ── No-meta / no-version stream ──

func TestClaudeAdapter_NoVersion_AllUnknown(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		claudeRec(`{"type":"user","message":{"role":"user","content":"<PROMPT>"},"uuid":"u1","timestamp":"2026-07-06T23:23:31.876Z","sessionId":"s1"}`),
		claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"s"}]},"uuid":"u2","timestamp":"2026-07-06T23:23:38.360Z","sessionId":"s1"}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	for _, e := range res.Events {
		if e.Type != agent.EventUnknown {
			t.Errorf("no-version batch: event %s type=%q, want EventUnknown", e.ID, e.Type)
		}
	}
	if !res.Degraded.Degraded {
		t.Error("no-version batch must be degraded")
	}
}

// ── Hook/session shape not conflated ──

func TestClaudeAdapter_HookShapeNotConflated(t *testing.T) {
	a := &Adapter{}
	// A 2.1.206 hook record (system/hook_started) must NOT be classified
	// as a typed event. The adapter only supports 2.1.202 session JSONL.
	hookRec := claudeRec(`{"type":"system","subtype":"hook_started","hook_name":"UserPromptSubmit","hook_event":"UserPromptSubmit","hook_id":"<redacted>","session_id":"<redacted>"}`)
	ev, deg := a.NormalizeEvent(context.Background(), hookRec)
	if ev.Type != agent.EventUnknown {
		t.Errorf("hook record: got %q, want EventUnknown", ev.Type)
	}
	if !deg.Degraded {
		t.Error("hook record must degrade (unsupported shape)")
	}
}

// ── One-shot / paged tuple equality ──

func TestClaudeAdapter_OneShot_Equals_Paged(t *testing.T) {
	a := &Adapter{}
	rec1 := claudeRec(`{"type":"user","message":{"role":"user","content":"x"},"uuid":"u1","timestamp":"2026-07-06T23:23:31.000Z","sessionId":"s1","version":"2.1.202"}`)
	rec2 := claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"s"}]},"uuid":"u2","timestamp":"2026-07-06T23:23:32.000Z","sessionId":"s1","version":"2.1.202"}`)
	rec3 := claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tid","name":"Read","input":{}}]},"uuid":"u3","timestamp":"2026-07-06T23:23:33.000Z","sessionId":"s1","version":"2.1.202"}`)
	records := []contract.RawRecord{rec1, rec2, rec3}

	oneShot := readAllEvents(t, a, records, 0)
	// Verify all MaxEvents produce identical (ID, Seq, Type) tuples.
	for _, maxEv := range []int{1, 2, 3} {
		paged := readAllEvents(t, a, records, maxEv)
		if len(oneShot) != len(paged) {
			t.Errorf("MaxEvents=%d: one-shot=%d events, paged=%d", maxEv, len(oneShot), len(paged))
			continue
		}
		for i := range oneShot {
			if oneShot[i] != paged[i] {
				t.Errorf("MaxEvents=%d event %d mismatch: %+v vs %+v", maxEv, i, oneShot[i], paged[i])
			}
		}
	}
}

type eventKey struct {
	ID   string
	Seq  int64
	Type contract.AgentEventType
}

func readAllEvents(t *testing.T, a *Adapter, records []contract.RawRecord, maxEvents int) []eventKey {
	t.Helper()
	var out []eventKey
	var cursor contract.Cursor
	for {
		res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
			Session:   contract.SessionContext{SessionID: "pokit:host-a"},
			Records:   records,
			Cursor:    cursor,
			MaxEvents: maxEvents,
		})
		for _, e := range res.Events {
			out = append(out, eventKey{ID: e.ID, Seq: e.Seq, Type: e.Type})
		}
		cursor = res.NextCursor
		if len(res.Events) == 0 {
			break
		}
	}
	return out
}

// ── Cursor: tamper/loss/rotation fail-closed ──

func TestClaudeAdapter_Cursor_LargeFullPrefix_ZeroReemission(t *testing.T) {
	a := &Adapter{}
	var records []contract.RawRecord
	records = append(records, versionMeta2_1_202())
	for i := 0; i < 299; i++ {
		records = append(records, claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"s"}]},"uuid":"u`+strconv.Itoa(i)+`","timestamp":"2026-07-06T23:23:38.`+nanoPad(i)+`Z","sessionId":"s1","version":"2.1.202"}`))
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res1.Events) != 300 {
		t.Fatalf("first read: got %d events, want 300", len(res1.Events))
	}
	if len(res1.NextCursor) > contract.MaxCursorBytes {
		t.Errorf("cursor too large: %d bytes", len(res1.NextCursor))
	}
	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  res1.NextCursor,
	})
	if len(res2.Events) != 0 {
		t.Fatalf("re-read: got %d events, want 0", len(res2.Events))
	}
}

func TestClaudeAdapter_Cursor_AnchorTamper_ZeroEvents(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		versionMeta2_1_202(),
		claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"s"}]},"uuid":"u1","timestamp":"2026-07-06T23:23:38.000Z","sessionId":"s1","version":"2.1.202"}`),
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	cur, _ := parseCursor(res1.NextCursor)
	cur.anchor = "deadbeefcafebabe"
	tampered := encodeCursor(cur)

	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  tampered,
	})
	if len(res2.Events) != 0 {
		t.Errorf("tampered anchor: got %d events, want 0", len(res2.Events))
	}
	if !res2.Degraded.Degraded {
		t.Error("tampered anchor must degrade")
	}
}

func TestClaudeAdapter_Cursor_AnchorLoss_ZeroEvents(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		versionMeta2_1_202(),
		claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"s"}]},"uuid":"u1","timestamp":"2026-07-06T23:23:38.000Z","sessionId":"s1","version":"2.1.202"}`),
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	cur, _ := parseCursor(res1.NextCursor)
	badCursor := contract.Cursor(strconv.FormatInt(cur.nextPos, 10) + ":")
	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  badCursor,
	})
	if len(res2.Events) != 0 {
		t.Errorf("anchor loss: got %d events, want 0", len(res2.Events))
	}
	if !res2.Degraded.Degraded {
		t.Error("anchor loss must degrade")
	}
}

func TestClaudeAdapter_Cursor_StreamRotation_ZeroEvents(t *testing.T) {
	a := &Adapter{}
	streamA := []contract.RawRecord{
		versionMeta2_1_202(),
		claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"s"}]},"uuid":"streamA","timestamp":"2026-07-06T23:23:38.000Z","sessionId":"s1","version":"2.1.202"}`),
	}
	resA, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: streamA,
	})
	streamB := []contract.RawRecord{
		claudeRec(`{"type":"user","message":{"role":"user","content":"x"},"uuid":"sb1","timestamp":"2026-07-06T23:23:31.876Z","sessionId":"s1","version":"2.1.202"}`),
	}
	resB, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: streamB,
		Cursor:  resA.NextCursor,
	})
	if len(resB.Events) != 0 {
		t.Errorf("stream rotation: got %d events, want 0", len(resB.Events))
	}
	if !resB.Degraded.Degraded {
		t.Error("stream rotation must degrade")
	}
}

// ── Source-position ordering ──

func TestClaudeAdapter_SameTimestamp_BothPreserved(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		claudeRec(`{"type":"user","message":{"role":"user","content":"x"},"uuid":"u1","timestamp":"2026-07-06T23:23:31.876Z","sessionId":"s1","version":"2.1.202"}`),
		claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"s"}]},"uuid":"u2","timestamp":"2026-07-06T23:23:31.876Z","sessionId":"s1","version":"2.1.202"}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) != 2 {
		t.Fatalf("same-timestamp: got %d events, want 2", len(res.Events))
	}
	if res.Events[0].Seq >= res.Events[1].Seq {
		t.Errorf("Seq must be strictly increasing: %d >= %d", res.Events[0].Seq, res.Events[1].Seq)
	}
}

func TestClaudeAdapter_SeqFromAbsolutePosition(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		claudeRec(`{"type":"user","message":{"role":"user","content":"x"},"uuid":"u1","timestamp":"2026-07-06T23:23:31.000Z","sessionId":"s1","version":"2.1.202"}`),
		claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"s"}]},"uuid":"u2","timestamp":"2026-07-06T23:23:32.000Z","sessionId":"s1","version":"2.1.202"}`),
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "pokit:host-a"},
		Records:   records,
		MaxEvents: 1,
	})
	if len(res1.Events) != 1 {
		t.Fatalf("limited read: got %d events, want 1", len(res1.Events))
	}
	firstSeq := res1.Events[0].Seq

	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  res1.NextCursor,
	})
	if len(res2.Events) != 1 {
		t.Fatalf("continuation: got %d events, want 1", len(res2.Events))
	}
	if res2.Events[0].Seq != firstSeq+1 {
		t.Errorf("continuation Seq=%d, want %d", res2.Events[0].Seq, firstSeq+1)
	}
}

// ── Malformed + oversized mixed batches ──

func TestClaudeAdapter_MixedBatch_DegradedAccumulates(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		versionMeta2_1_202(),
		claudeRec(`{"type":"unknown_event_type","sessionId":"<UUID>","unexpectedField":"<REDACTED>"}`),
		claudeRec(`{"type":"user","sessionId":"<UUID>"}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	foundUser := false
	for _, e := range res.Events {
		if e.Type == agent.EventUserMessage {
			foundUser = true
		}
	}
	if !foundUser {
		t.Error("mixed batch lost the valid user event")
	}
	if !res.Degraded.Degraded {
		t.Error("mixed valid+malformed batch must be degraded")
	}
}

func TestClaudeAdapter_Oversized_ValidSeq(t *testing.T) {
	big := make([]byte, contract.MaxRecordBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	records := []contract.RawRecord{
		versionMeta2_1_202(),                    // pos 0: valid
		{Bytes: big, Source: agent.SourceJSONL}, // pos 1: oversized
		claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"s"}]},"uuid":"u2","timestamp":"2026-07-06T23:23:38.000Z","sessionId":"s1","version":"2.1.202"}`), // pos 2: valid
	}
	res, _ := (&Adapter{}).ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) != 2 {
		t.Fatalf("got %d events, want 2", len(res.Events))
	}
	if res.Events[1].Seq != 2 {
		t.Errorf("valid-after-oversized Seq=%d, want 2 (absolute pos)", res.Events[1].Seq)
	}
	if err := contract.ValidateCursor(res.NextCursor); err != nil {
		t.Errorf("cursor invalid after oversized: %v", err)
	}
}

// ── Leak prevention ──

func TestClaudeAdapter_NoSecretLeak(t *testing.T) {
	secret := `"sk-` + `SUPERSECRETdeadbeef0123456789"`
	rec := contract.RawRecord{
		Bytes:      []byte(`{"type":"user","message":{"role":"user","content":"` + secret + ` at /Users/victim/x"},"uuid":"u1","timestamp":"2026-07-06T23:23:31.876Z","sessionId":"s1","version":"2.1.202"}`),
		Source:     agent.SourceJSONL,
		Provenance: contract.ProvenanceNativeLog,
	}

	a := &Adapter{}
	ev, deg := a.NormalizeEvent(context.Background(), rec)

	if contract.ContainsSensitive(ev.Text) {
		t.Errorf("event Text leaked sensitive content: %q", ev.Text)
	}
	for _, v := range ev.Metadata {
		if contract.ContainsSensitive(v) {
			t.Errorf("event Metadata leaked sensitive content: %q", v)
		}
	}
	if contract.ContainsSensitive(deg.Reason) {
		t.Errorf("degraded reason leaked sensitive content: %q", deg.Reason)
	}
	for _, d := range deg.Diagnostics {
		if contract.ContainsSensitive(d) {
			t.Errorf("degraded diagnostic leaked sensitive content: %q", d)
		}
	}

	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: []contract.RawRecord{rec},
	})
	for _, diag := range res.Degraded.Diagnostics {
		if contract.ContainsSensitive(diag) {
			t.Errorf("ReadEvents diagnostic leaked sensitive content: %q", diag)
		}
	}
}

func TestClaudeAdapter_NoThinkingLeak(t *testing.T) {
	a := &Adapter{}
	rec := claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"I should use Bash tool to run ls","signature":"sig123"}],"stop_reason":"tool_use"},"uuid":"u1","timestamp":"2026-07-06T23:23:38.360Z","sessionId":"s1","version":"2.1.202"}`)
	ev, _ := a.NormalizeEvent(context.Background(), rec)
	if ev.Type != agent.EventThinking {
		t.Errorf("thinking record: got %q, want thinking", ev.Type)
	}
	// Thinking text must NOT appear in event Text.
	if strings.Contains(ev.Text, "Bash") || strings.Contains(ev.Text, "ls") {
		t.Errorf("thinking text leaked into event Text: %q", ev.Text)
	}
	// Thinking signature must NOT leak.
	if strings.Contains(ev.Text, "sig123") {
		t.Errorf("thinking signature leaked: %q", ev.Text)
	}
}

func TestClaudeAdapter_NoCommandLeak(t *testing.T) {
	a := &Adapter{}
	rec := claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tid1","name":"Bash","input":{"command":"rm -rf /","description":"cleanup"}}],"stop_reason":"tool_use"},"uuid":"u1","timestamp":"2026-07-06T23:23:39.398Z","sessionId":"s1","version":"2.1.202"}`)
	ev, _ := a.NormalizeEvent(context.Background(), rec)
	if ev.Type != agent.EventToolCallStarted {
		t.Errorf("tool_use record: got %q, want tool_call_started", ev.Type)
	}
	// Command text must NOT leak into event Text.
	if strings.Contains(ev.Text, "rm") || strings.Contains(ev.Text, "-rf") {
		t.Errorf("command leaked into event Text: %q", ev.Text)
	}
	// Tool name IS safe identity metadata.
	if ev.Text != "Bash" {
		t.Errorf("tool name not surfaced: %q", ev.Text)
	}
}

func TestClaudeAdapter_NoToolResultLeak(t *testing.T) {
	a := &Adapter{}
	token := "sk" + "-abc123"
	rec := claudeRec(`{"parentUuid":"<UUID>","isSidechain":false,"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"secret output with token ` + token + `","is_error":false,"tool_use_id":"tid1"}]},"uuid":"u1","timestamp":"2026-07-06T23:23:39.486Z","sessionId":"s1","version":"2.1.202"}`)
	ev, _ := a.NormalizeEvent(context.Background(), rec)
	if ev.Type != agent.EventToolCallFinished {
		t.Errorf("tool_result record: got %q, want tool_call_finished", ev.Type)
	}
	// Tool result output must NOT leak.
	if strings.Contains(ev.Text, "secret") || strings.Contains(ev.Text, "token") {
		t.Errorf("tool result output leaked into event Text: %q", ev.Text)
	}
}

// ── Approval: permission-mode is NOT approval evidence ──

func TestClaudeAdapter_PermissionMode_NotApproval(t *testing.T) {
	a := &Adapter{}
	rec := claudeRec(`{"parentUuid":"<UUID>","isSidechain":false,"type":"permission-mode","permissionMode":"ask","sessionId":"<UUID>"}`)
	ev, deg := a.NormalizeEvent(context.Background(), rec)
	if ev.Type != agent.EventUnknown {
		t.Errorf("permission-mode: got %q, want EventUnknown (not approval evidence)", ev.Type)
	}
	if !deg.Degraded {
		t.Error("permission-mode must degrade")
	}

	// DetectApproval must return nil even when fed a permission-mode event.
	approvals, _ := a.DetectApproval(context.Background(), []contract.AgentEvent{ev})
	if len(approvals) != 0 {
		t.Errorf("permission-mode produced %d approvals, want 0", len(approvals))
	}
}

func TestClaudeAdapter_ApprovalLikeText_NotApproval(t *testing.T) {
	a := &Adapter{}
	rec := claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"please approve this change"}],"stop_reason":"end_turn"},"uuid":"u1","timestamp":"2026-07-06T23:23:38.360Z","sessionId":"s1","version":"2.1.202"}`)
	ev, _ := a.NormalizeEvent(context.Background(), rec)
	approvals, _ := a.DetectApproval(context.Background(), []contract.AgentEvent{ev})
	if len(approvals) != 0 {
		t.Errorf("approval-like text produced %d approvals, want 0", len(approvals))
	}
}

func TestClaudeAdapter_ToolUse_NotApproval(t *testing.T) {
	a := &Adapter{}
	rec := claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tid1","name":"AskUserQuestion","input":{"question":"Approve?"}}],"stop_reason":"tool_use"},"uuid":"u1","timestamp":"2026-07-06T23:23:39.398Z","sessionId":"s1","version":"2.1.202"}`)
	ev, _ := a.NormalizeEvent(context.Background(), rec)
	approvals, _ := a.DetectApproval(context.Background(), []contract.AgentEvent{ev})
	if len(approvals) != 0 {
		t.Errorf("tool_use produced %d approvals, want 0", len(approvals))
	}
}

// ── Descriptor: no CapLogDetection ──

func TestClaudeAdapter_Descriptor_NoCapLogDetection(t *testing.T) {
	d := (&Adapter{}).Descriptor()
	for _, c := range d.Capabilities {
		if c == contract.CapLogDetection {
			t.Error("CapLogDetection must not be declared (no safe file discovery)")
		}
	}
	// CapApprovalDetection IS declared with a synthetic harness-only fixture.
	// See package doc and §9 for the evidence limitation.
	foundApproval := false
	for _, c := range d.Capabilities {
		if c == contract.CapApprovalDetection {
			foundApproval = true
		}
	}
	if !foundApproval {
		t.Error("CapApprovalDetection must be declared (synthetic fixture for harness conformance)")
	}
}

// ── Detection ──

func TestClaudeAdapter_Detect_EmptyIsUnknown(t *testing.T) {
	a := &Adapter{}
	id, err := a.Detect(context.Background(), contract.SessionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if id.Kind != "unknown" || id.Confidence >= 0.5 {
		t.Errorf("empty context: kind=%q conf=%.2f", id.Kind, id.Confidence)
	}
}

func TestClaudeAdapter_Detect_ClaudeProcess(t *testing.T) {
	a := &Adapter{}
	id, err := a.Detect(context.Background(), contract.SessionContext{
		SessionID: "pokit:host-a", ProcessName: "claude", CWD: "/Users/dev/project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id.Kind != "claude" {
		t.Errorf("claude process: kind=%q, want claude", id.Kind)
	}
	if id.Confidence < 0.5 {
		t.Errorf("claude process confidence %.2f < 0.5", id.Confidence)
	}
}

func TestClaudeAdapter_Detect_CWDBoost(t *testing.T) {
	a := &Adapter{}
	id, _ := a.Detect(context.Background(), contract.SessionContext{
		ProcessName: "node", CWD: "/Users/dev/.claude/projects/myproject",
	})
	// node alone isn't claude, but .claude in CWD gives a boost.
	// Still shouldn't reach 0.5 with just CWD boost (0.1+0.15=0.25).
	if id.Kind != "unknown" {
		t.Errorf("node+.claude CWD: kind=%q, want unknown", id.Kind)
	}
}

// ── GetStatus delegation ──

func TestClaudeAdapter_GetStatus_NoEvidence(t *testing.T) {
	a := &Adapter{}
	res, err := a.GetStatus(context.Background(), contract.StatusInput{
		Session: contract.SessionContext{SessionID: "s"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != agent.StatusUnknown {
		t.Errorf("no evidence: status=%q, want unknown", res.Status)
	}
	if !res.Degraded.Degraded {
		t.Error("no evidence must produce degraded result")
	}
}

// ── Discovery: always nil ──

func TestClaudeAdapter_Discovery_AlwaysEmpty(t *testing.T) {
	a := &Adapter{}
	got, _ := a.DiscoverSessions(context.Background(), contract.DiscoveryInput{
		Session: contract.SessionContext{},
	})
	if len(got) != 0 {
		t.Error("empty context must return empty")
	}
	got, _ = a.DiscoverSessions(context.Background(), contract.DiscoveryInput{
		Session: contract.SessionContext{SessionID: "s", ProcessName: "claude", CWD: "/home/dev/.claude"},
	})
	if len(got) != 0 {
		t.Errorf("process-name-only must not invent discovery: got %d sessions", len(got))
	}
}

// ── ValidateEvent on all returned events ──

func TestClaudeAdapter_ValidateEvent_AllReturnedEvents(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		versionMeta2_1_202(),
		claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"<REDACTED>","signature":"<UUID>"}],"stop_reason":"tool_use"},"uuid":"u2","timestamp":"2026-07-06T23:23:38.360Z","sessionId":"<UUID>","version":"2.1.202"}`),
		claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tid","name":"Read","input":{}}],"stop_reason":"tool_use"},"uuid":"u3","timestamp":"2026-07-06T23:23:39.398Z","sessionId":"<UUID>","version":"2.1.202"}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) != 3 {
		t.Fatalf("got %d events, want 3", len(res.Events))
	}
	for _, e := range res.Events {
		if err := contract.ValidateEvent(e); err != nil {
			t.Errorf("ValidateEvent failed for %s (type=%s): %v", e.ID, e.Type, err)
		}
	}
}

// ── Cursor: deterministic ──

func TestClaudeAdapter_Cursor_Deterministic(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		versionMeta2_1_202(),
		claudeRec(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"s"}]},"uuid":"u1","timestamp":"2026-07-06T23:23:38.000Z","sessionId":"s1","version":"2.1.202"}`),
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if string(res1.NextCursor) != string(res2.NextCursor) {
		t.Errorf("non-deterministic cursor:\n  %s\n  %s", res1.NextCursor, res2.NextCursor)
	}
	if toJSON(t, res1.Events) != toJSON(t, res2.Events) {
		t.Error("non-deterministic events")
	}
}

// ── Helpers ──

func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchSubstring(s, sub)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
