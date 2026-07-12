package v0_144_1

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// newAdapter builds a healthy codex adapter for conformance testing.
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

// codexRec builds a RawRecord from a Codex JSONL line with native-log provenance
// and JSONL source (preserved per-record, not overridden by batch).
func codexRec(line string) contract.RawRecord {
	return contract.RawRecord{
		Bytes:      []byte(line),
		Source:     agent.SourceJSONL,
		Provenance: contract.ProvenanceNativeLog,
	}
}

// sessionMeta0_144_1 is the version-confirming session_meta record required at
// the start of valid record batches.
func sessionMeta0_144_1() contract.RawRecord {
	return codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","id":"m1","timestamp":"2026-07-06T13:29:26.903Z","cwd":"<HOME>/<PROJECT>","originator":"codex-tui","cli_version":"0.144.1","source":"cli","model_provider":"<PROVIDER>"}}`)
}

func codexFixtures() contract.ConformanceFixtures {
	// Valid records: session_meta (with correct cli_version) + task_started + user message.
	// The session_meta is required so the version gate passes.
	validRecords := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1","started_at":1783344575,"model_context_window":353400,"collaboration_mode_kind":"default"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:38.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<PROMPT>"}]}}`),
	}

	// Approval records: waiting_for_approval + approval_resolved, each carrying
	// the same approval_id so the approval lifecycle is linked.
	approvalRecords := []contract.RawRecord{
		codexRec(`{"timestamp":"2026-07-06T13:30:00.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","turn_id":"t2","approval_id":"appr-001","message":"<REDACTED_APPROVAL_MESSAGE>"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:30:05.000Z","type":"event_msg","payload":{"type":"approval_resolved","turn_id":"t2","approval_id":"appr-001","resolution":"approved"}}`),
	}

	// Near-miss: a response_item (user message) mentioning "approve".
	nearMissRecords := []contract.RawRecord{
		codexRec(`{"timestamp":"2026-07-06T13:30:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"please approve this change"}]}}`),
	}

	// Malformed records: unknown type, missing fields, garbage.
	malformedRecords := []contract.RawRecord{
		codexRec(`{"timestamp":"2026-07-06T13:29:35.500Z","type":"unknown_msg_type","payload":{}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:36.000Z"}`),
		codexRec(`{"type":"response_item"}`),
		{},
		{Bytes: []byte("{"), Source: agent.SourceJSONL},
		{Bytes: []byte("\x00\xff not json"), Source: agent.SourceJSONL},
		{Bytes: nil},
	}

	return contract.ConformanceFixtures{
		DetectContext:    contract.SessionContext{SessionID: "pokit:host-a", ProcessName: "codex", CWD: "/Users/dev/.codex/sessions/2026/07/06"},
		ExpectDetectKind: "codex",

		ValidRecords: validRecords,
		ExpectTypes:  []contract.AgentEventType{agent.EventAgentStarted},

		// Distinct records for bounds/dedupe/cursor checks. Each has a unique
		// nanosecond timestamp so Seq values are all distinct.
		DistinctRecord: func(i int) contract.RawRecord {
			ts := "2026-07-06T13:29:35." + nanoPad(i) + "Z"
			return codexRec(`{"timestamp":"` + ts + `","type":"event_msg","payload":{"type":"task_started","turn_id":"distinct-t` + strconv.Itoa(i) + `","started_at":1783344575}}`)
		},

		// SizedRecord for batch-byte boundary test.
		SizedRecord: func(size int) contract.RawRecord {
			pad := size - 90
			if pad < 0 {
				pad = 0
			}
			b := make([]byte, pad)
			for i := range b {
				b[i] = byte('a' + (i % 26))
			}
			return codexRec(`{"timestamp":"2026-07-06T13:29:35.00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"sz-` + strconv.Itoa(size) + `","started_at":0,"_pad":"` + string(b) + `"}}`)
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

func TestCodexAdapter_Conformance(t *testing.T) {
	contract.RunAgentContract(t, "codex", func(t *testing.T) contract.AgentAdapter {
		return &Adapter{}
	}, codexFixtures())
}

// ── DTO snapshot stability ──

func TestCodexAdapter_DTOSnapshotStable(t *testing.T) {
	a := &Adapter{}
	res, err := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: codexFixtures().ValidRecords,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) < 2 {
		t.Fatalf("expected ≥2 events from valid records, got %d", len(res.Events))
	}

	got := toJSON(t, res.Events)
	checks := []string{
		`"type":"agent_started"`,
		`"type":"user_message"`,
		`"provenance":"native_log"`,
		`"sessionId":"pokit:host-a"`,
		`"agentKind":"codex"`,
	}
	for _, ck := range checks {
		if !contains(got, ck) {
			t.Errorf("DTO missing %s", ck)
		}
	}

	// Second read must produce identical JSON.
	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: codexFixtures().ValidRecords,
	})
	if toJSON(t, res.Events) != toJSON(t, res2.Events) {
		t.Error("normalization is not deterministic across fresh reads")
	}
}

// ── BLOCKER 1: Version gate ──

func TestCodexAdapter_VersionGate_ExactMatch(t *testing.T) {
	a := &Adapter{}
	rec := sessionMeta0_144_1()
	ev, deg := a.NormalizeEvent(context.Background(), rec)
	if deg.Degraded {
		t.Errorf("exact version 0.144.1 must not degrade: %+v", deg)
	}
	if ev.Type != agent.EventAgentStarted {
		t.Errorf("exact version: got %q, want agent_started", ev.Type)
	}
	if ev.Confidence < 0.8 {
		t.Errorf("exact version confidence too low: %.2f", ev.Confidence)
	}
}

func TestCodexAdapter_VersionGate_NewerVersion(t *testing.T) {
	a := &Adapter{}
	rec := codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"cli_version":"9.9.9","session_id":"s1"}}`)
	ev, deg := a.NormalizeEvent(context.Background(), rec)
	if ev.Type != agent.EventUnknown {
		t.Errorf("newer version: got %q, want EventUnknown", ev.Type)
	}
	if !deg.Degraded {
		t.Error("newer version must degrade")
	}
}

func TestCodexAdapter_VersionGate_MissingVersion(t *testing.T) {
	a := &Adapter{}
	rec := codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1"}}`)
	ev, deg := a.NormalizeEvent(context.Background(), rec)
	if ev.Type != agent.EventUnknown {
		t.Errorf("missing version: got %q, want EventUnknown", ev.Type)
	}
	if !deg.Degraded {
		t.Error("missing version must degrade")
	}
}

func TestCodexAdapter_VersionGate_MalformedPayload(t *testing.T) {
	a := &Adapter{}
	// Version is a number, not a string — must not match "0.144.1".
	rec := codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"cli_version":1}}`)
	ev, deg := a.NormalizeEvent(context.Background(), rec)
	if ev.Type != agent.EventUnknown {
		t.Errorf("non-string version: got %q, want EventUnknown", ev.Type)
	}
	if !deg.Degraded {
		t.Error("non-string version must degrade")
	}
}

func TestCodexAdapter_VersionGate_BatchMismatch(t *testing.T) {
	a := &Adapter{}
	// First record: session_meta with wrong version. All subsequent records
	// in this batch must be EventUnknown.
	records := []contract.RawRecord{
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"cli_version":"9.9.9"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:38.000Z","type":"response_item","payload":{"type":"message","role":"user"}}`),
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

func TestCodexAdapter_VersionGate_NoSessionMeta_StillWorksDegraded(t *testing.T) {
	a := &Adapter{}
	// Records without session_meta are classified but marked degraded.
	rec := codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`)
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: []contract.RawRecord{rec},
	})
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(res.Events))
	}
	if !res.Degraded.Degraded {
		t.Error("no session_meta must produce degraded (version not confirmed)")
	}
}

// ── BLOCKER 2: No invented correlation ──

func TestCodexAdapter_Discovery_AlwaysEmpty(t *testing.T) {
	a := &Adapter{}

	// Empty context.
	got, _ := a.DiscoverSessions(context.Background(), contract.DiscoveryInput{
		Session: contract.SessionContext{},
	})
	if len(got) != 0 {
		t.Error("empty context must return empty")
	}

	// Codex process name alone does NOT grant correlation.
	got, _ = a.DiscoverSessions(context.Background(), contract.DiscoveryInput{
		Session: contract.SessionContext{SessionID: "s", ProcessName: "codex", CWD: "/home/dev/.codex"},
	})
	if len(got) != 0 {
		t.Errorf("process-name-only must not invent discovery: got %d sessions", len(got))
	}
}

// ── BLOCKER 3: Same-timestamp records preserved ──

func TestCodexAdapter_SameTimestamp_BothPreserved(t *testing.T) {
	a := &Adapter{}
	// Real fixture: session_meta and task_started share the exact same timestamp
	// (13:29:35.399). Both must appear in the result.
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1","started_at":1783344575}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) != 2 {
		t.Fatalf("same-timestamp: got %d events, want 2 (both preserved)", len(res.Events))
	}
	if res.Events[0].Seq >= res.Events[1].Seq {
		t.Errorf("Seq must be strictly increasing: %d >= %d", res.Events[0].Seq, res.Events[1].Seq)
	}
}

func TestCodexAdapter_SameTimestamp_Dedupe(t *testing.T) {
	a := &Adapter{}
	// Same record twice => 1 event.
	rec := sessionMeta0_144_1()
	records := []contract.RawRecord{rec, rec}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) != 1 {
		t.Errorf("duplicate same-timestamp: got %d events, want 1", len(res.Events))
	}
}

func TestCodexAdapter_SameTimestamp_RereadNoReemit(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) != 2 {
		t.Fatalf("first read: got %d events, want 2", len(res.Events))
	}
	// Re-read with cursor — must re-emit nothing.
	again, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  res.NextCursor,
	})
	if len(again.Events) != 0 {
		t.Errorf("cursor re-read re-emitted %d events, want 0", len(again.Events))
	}
}

// ── BLOCKER 4: Degradation accumulation + deterministic timestamps ──

func TestCodexAdapter_MixedBatch_DegradedAccumulates(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.500Z","type":"unknown_msg_type","payload":{}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:36.000Z"}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	// The session_meta should still produce a valid event.
	foundStarted := false
	for _, e := range res.Events {
		if e.Type == agent.EventAgentStarted {
			foundStarted = true
		}
	}
	if !foundStarted {
		t.Error("mixed batch lost the valid session_meta event")
	}
	if !res.Degraded.Degraded {
		t.Error("mixed valid+malformed batch must be degraded")
	}
}

func TestCodexAdapter_InvalidTimestamp_Deterministic(t *testing.T) {
	a := &Adapter{}
	// Missing/invalid timestamp → zero time (never time.Now()).
	rec := codexRec(`{"type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`)
	ev1, _ := a.NormalizeEvent(context.Background(), rec)
	ev2, _ := a.NormalizeEvent(context.Background(), rec)
	if ev1.Seq != ev2.Seq {
		t.Errorf("non-deterministic Seq for same record: %d vs %d", ev1.Seq, ev2.Seq)
	}
	if toJSON(t, ev1) != toJSON(t, ev2) {
		t.Error("non-deterministic normalization for same record")
	}
}

// ── BLOCKER 5: Approval identity preserved on resolved ──

func TestCodexAdapter_Approval_ResolvedCarriesApprovalID(t *testing.T) {
	a := &Adapter{}
	// ReadEvents version gate: include session_meta with correct version so the
	// approval_resolved event is classified confidently.
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:30:05.000Z","type":"event_msg","payload":{"type":"approval_resolved","turn_id":"t2","approval_id":"appr-001","resolution":"approved"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	// Find the approval_resolved event.
	var got *contract.AgentEvent
	for i := range res.Events {
		if res.Events[i].Type == agent.EventApprovalResolved {
			got = &res.Events[i]
			break
		}
	}
	if got == nil {
		t.Fatal("approval_resolved event not found in batch")
	}
	if got.ApprovalID != "appr-001" {
		t.Errorf("approval_resolved ApprovalID=%q, want appr-001", got.ApprovalID)
	}
}

func TestCodexAdapter_Approval_ResolvedMissingID(t *testing.T) {
	a := &Adapter{}
	rec := codexRec(`{"timestamp":"2026-07-06T13:30:05.000Z","type":"event_msg","payload":{"type":"approval_resolved","turn_id":"t2","resolution":"approved"}}`)
	ev, deg := a.NormalizeEvent(context.Background(), rec)
	if ev.Type != agent.EventUnknown {
		t.Errorf("approval_resolved without approval_id: got %q, want EventUnknown", ev.Type)
	}
	if !deg.Degraded {
		t.Error("approval_resolved without approval_id must degrade")
	}
}

func TestCodexAdapter_Approval_RequestAndResolvedLinked(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		codexRec(`{"timestamp":"2026-07-06T13:30:00.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","turn_id":"t2","approval_id":"appr-001","message":"approve?"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:30:05.000Z","type":"event_msg","payload":{"type":"approval_resolved","turn_id":"t2","approval_id":"appr-001","resolution":"approved"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) != 2 {
		t.Fatalf("got %d events, want 2", len(res.Events))
	}
	// Both carry the same ApprovalID.
	if res.Events[0].ApprovalID != "appr-001" {
		t.Errorf("request ApprovalID=%q", res.Events[0].ApprovalID)
	}
	if res.Events[1].ApprovalID != "appr-001" {
		t.Errorf("resolved ApprovalID=%q", res.Events[1].ApprovalID)
	}
	// One is approval_requested, one is approval_resolved.
	types := map[contract.AgentEventType]bool{}
	for _, e := range res.Events {
		types[e.Type] = true
	}
	if !types[agent.EventApprovalRequested] || !types[agent.EventApprovalResolved] {
		t.Error("missing approval_requested or approval_resolved in linked pair")
	}
}

// ── Secret leak prevention ──

func TestCodexAdapter_NoSecretLeak(t *testing.T) {
	hostile := `{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1","message":"token ` + "sk" + `-abcd1234567890abcd at /Users/victim/x"}}`
	rec := contract.RawRecord{Bytes: []byte(hostile), Source: agent.SourceJSONL, Provenance: contract.ProvenanceNativeLog}

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

// ── Detection ──

func TestCodexAdapter_Detect_EmptyIsUnknown(t *testing.T) {
	a := &Adapter{}
	id, err := a.Detect(context.Background(), contract.SessionContext{})
	if err != nil {
		t.Fatal(err)
	}
	if id.Kind != "unknown" || id.Confidence >= 0.5 {
		t.Errorf("empty context: kind=%q conf=%.2f", id.Kind, id.Confidence)
	}
}

// ── GetStatus delegation ──

func TestCodexAdapter_GetStatus_NoEvidence(t *testing.T) {
	a := &Adapter{}
	res, err := a.GetStatus(context.Background(), contract.StatusInput{
		Session: contract.SessionContext{SessionID: "s"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != agent.StatusUnknown {
		t.Errorf("no evidence status=%q, want unknown", res.Status)
	}
	if !res.Degraded.Degraded {
		t.Error("no evidence must produce degraded result")
	}
}

// ── Additional: CapLogDetection removed ──

func TestCodexAdapter_Descriptor_NoCapLogDetection(t *testing.T) {
	d := (&Adapter{}).Descriptor()
	for _, c := range d.Capabilities {
		if c == contract.CapLogDetection {
			t.Error("CapLogDetection must not be declared (no file scanning / LogRef discovery)")
		}
	}
}

// ── Additional: source per-record preserved ──

func TestCodexAdapter_PerRecordSource(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		{Bytes: []byte(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"cli_version":"0.144.1"}}`), Source: agent.SourceJSONL},
		{Bytes: []byte(`{"timestamp":"2026-07-06T13:29:38.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`), Source: agent.SourceLogFile},
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) < 2 {
		t.Fatalf("got %d events, want 2", len(res.Events))
	}
	// Each event keeps its own source; batch-first-record override removed.
	sources := map[contract.AgentEventSource]int{}
	for _, e := range res.Events {
		sources[e.Source]++
	}
	if sources[agent.SourceJSONL] == 0 || sources[agent.SourceLogFile] == 0 {
		t.Errorf("per-record source not preserved: %v", sources)
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
