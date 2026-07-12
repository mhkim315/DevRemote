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
	// the same approval_id so the approval lifecycle is linked.  Must include
	// session_meta so the version gate passes.
	approvalRecords := []contract.RawRecord{
		sessionMeta0_144_1(),
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
	// Without session_meta, no version-specific typed events may be emitted.
	// The event is still returned, but forced to EventUnknown + degraded.
	rec := codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`)
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: []contract.RawRecord{rec},
	})
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(res.Events))
	}
	if res.Events[0].Type != agent.EventUnknown {
		t.Errorf("no-meta: type=%q, want EventUnknown", res.Events[0].Type)
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
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:30:00.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","turn_id":"t2","approval_id":"appr-001","message":"approve?"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:30:05.000Z","type":"event_msg","payload":{"type":"approval_resolved","turn_id":"t2","approval_id":"appr-001","resolution":"approved"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) < 2 {
		t.Fatalf("got %d events, want ≥2", len(res.Events))
	}
	// Both carry the same ApprovalID.
	var reqEv, resEv *contract.AgentEvent
	for i := range res.Events {
		switch res.Events[i].Type {
		case agent.EventApprovalRequested:
			reqEv = &res.Events[i]
		case agent.EventApprovalResolved:
			resEv = &res.Events[i]
		}
	}
	if reqEv == nil || resEv == nil {
		t.Fatal("missing approval_requested or approval_resolved in linked pair")
	}
	if reqEv.ApprovalID != "appr-001" {
		t.Errorf("request ApprovalID=%q", reqEv.ApprovalID)
	}
	if resEv.ApprovalID != "appr-001" {
		t.Errorf("resolved ApprovalID=%q", resEv.ApprovalID)
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

// ── BLOCKER 1 (round 3): Position-based Seq + cursor append ──

func TestCodexAdapter_Seq_PositionBased_Monotonic(t *testing.T) {
	// Seq is position-based: strictly increasing, stable on re-read from empty.
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t3","started_at":1}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t51","started_at":1}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) != 3 {
		t.Fatalf("got %d events, want 3", len(res.Events))
	}
	// Seq must be strictly increasing in input order.
	for i := 1; i < len(res.Events); i++ {
		if res.Events[i].Seq <= res.Events[i-1].Seq {
			t.Errorf("Seq not strictly increasing: %d <= %d", res.Events[i].Seq, res.Events[i-1].Seq)
		}
	}
	// All Seq values must be distinct.
	seqs := map[int64]bool{}
	for _, e := range res.Events {
		if seqs[e.Seq] {
			t.Errorf("Seq collision: %d", e.Seq)
		}
		seqs[e.Seq] = true
	}
}

func TestCodexAdapter_Cursor_AppendAfterRead(t *testing.T) {
	// First read, then append a record whose content-hash is "lower" than
	// previous records. Position-based Seq guarantees it is still emitted.
	a := &Adapter{}
	first := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"first"}}`),
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: first,
	})
	if len(res1.Events) != 2 {
		t.Fatalf("first read: got %d events, want 2", len(res1.Events))
	}
	cursor := res1.NextCursor

	// Append a new record — any content hash.  Must be emitted with Seq > previous.
	second := []contract.RawRecord{
		codexRec(`{"timestamp":"2026-07-06T13:29:40.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"second"}}`),
	}
	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: second,
		Cursor:  cursor,
	})
	if len(res2.Events) != 1 {
		t.Fatalf("append read: got %d events, want 1 (appended record emitted)", len(res2.Events))
	}
	// The appended record's Seq must be greater than all first-read Seqs.
	for _, e := range res1.Events {
		if res2.Events[0].Seq <= e.Seq {
			t.Errorf("appended Seq %d not > previous Seq %d", res2.Events[0].Seq, e.Seq)
		}
	}
}

func TestCodexAdapter_Cursor_ResumeSamePrefix(t *testing.T) {
	// After reading a prefix, re-reading the SAME records with the cursor
	// must emit zero events.
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	cursor := res1.NextCursor

	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  cursor,
	})
	if len(res2.Events) != 0 {
		t.Errorf("re-read same prefix: got %d events, want 0", len(res2.Events))
	}
}

func TestCodexAdapter_Cursor_CallerLimit_Continuation(t *testing.T) {
	// Read with caller limit, then read remaining records from cursor.
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:38.000Z","type":"response_item","payload":{"type":"message","role":"user"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:40.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t2"}}`),
	}
	res1, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session:   contract.SessionContext{SessionID: "pokit:host-a"},
		Records:   records,
		MaxEvents: 2,
	})
	if len(res1.Events) != 2 {
		t.Fatalf("first limited read: got %d events, want 2", len(res1.Events))
	}

	// Continuation: cursor prevents re-emission of first 2 records; the
	// remaining 2 NOT in cursor are emitted.
	res2, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
		Cursor:  res1.NextCursor,
	})
	if len(res2.Events) != 2 {
		t.Fatalf("continuation read: got %d events, want 2 (not-yet-emitted records)", len(res2.Events))
	}
	// Total across both reads covers all 4 records with no loss.
	if len(res1.Events)+len(res2.Events) != 4 {
		t.Errorf("total events across reads: %d, want 4", len(res1.Events)+len(res2.Events))
	}
}

func TestCodexAdapter_Cursor_SourceOrderPreserved(t *testing.T) {
	// Events must be emitted in input order regardless of timestamp ordering.
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T14:00:00.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"late"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:00:00.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"early"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) != 3 {
		t.Fatalf("got %d events, want 3", len(res.Events))
	}
	// "late" timestamp record was first in input, must be emitted before "early".
	if res.Events[1].ID == res.Events[2].ID {
		t.Fatal("duplicate IDs")
	}
	// Verify the order by looking at Seq — first input → lower Seq.
	if res.Events[1].Seq >= res.Events[2].Seq {
		t.Errorf("source order not preserved: Seq %d >= %d", res.Events[1].Seq, res.Events[2].Seq)
	}
}

func TestCodexAdapter_Seq_MissingTimestampEmitted(t *testing.T) {
	a := &Adapter{}
	rec := codexRec(`{"type":"event_msg","payload":{"type":"task_started","turn_id":"no-ts"}}`)
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: []contract.RawRecord{rec},
	})
	if len(res.Events) != 1 {
		t.Fatalf("missing-timestamp: got %d events, want 1 (emitted as EventUnknown)", len(res.Events))
	}
	if res.Events[0].Type != agent.EventUnknown {
		t.Errorf("missing-timestamp: type=%q, want EventUnknown", res.Events[0].Type)
	}
	if err := contract.ValidateEvent(res.Events[0]); err != nil {
		t.Errorf("emitted event fails ValidateEvent: %v", err)
	}
}

func TestCodexAdapter_ValidateEvent_AllReturnedEvents(t *testing.T) {
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:38.000Z","type":"response_item","payload":{"type":"message","role":"user"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:30:00.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","turn_id":"t2","approval_id":"appr-001"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:30:05.000Z","type":"event_msg","payload":{"type":"approval_resolved","turn_id":"t2","approval_id":"appr-001"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if len(res.Events) != 5 {
		t.Fatalf("got %d events, want 5", len(res.Events))
	}
	for _, e := range res.Events {
		if err := contract.ValidateEvent(e); err != nil {
			t.Errorf("ValidateEvent failed for %s (type=%s): %v", e.ID, e.Type, err)
		}
	}
}

// ── BLOCKER 2 (round 2): Version gate validates entire batch ──

func TestCodexAdapter_VersionGate_MixedMeta_BatchAllUnknown(t *testing.T) {
	// session_meta 0.144.1 followed by session_meta 9.9.9 — the second
	// meta poisons the ENTIRE batch. All events must be EventUnknown.
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:36.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
		codexRec(`{"timestamp":"2026-07-06T14:00:00.000Z","type":"session_meta","payload":{"cli_version":"9.9.9"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:38.000Z","type":"response_item","payload":{"type":"message","role":"user"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	for _, e := range res.Events {
		if e.Type != agent.EventUnknown {
			t.Errorf("mixed meta batch: event %s type=%q, want EventUnknown for all", e.ID, e.Type)
		}
	}
	if !res.Degraded.Degraded {
		t.Error("mixed meta batch must be degraded")
	}
}

func TestCodexAdapter_VersionGate_NoMeta_AllUnknown(t *testing.T) {
	// No session_meta at all — version unconfirmed. No version-specific
	// typed events may be emitted. Everything must be EventUnknown.
	a := &Adapter{}
	records := []contract.RawRecord{
		codexRec(`{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:29:38.000Z","type":"response_item","payload":{"type":"message","role":"user"}}`),
		codexRec(`{"timestamp":"2026-07-06T13:30:00.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","turn_id":"t2","approval_id":"appr-001"}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	for _, e := range res.Events {
		if e.Type != agent.EventUnknown {
			t.Errorf("no-meta batch: event %s type=%q, want EventUnknown for all", e.ID, e.Type)
		}
	}
	if !res.Degraded.Degraded {
		t.Error("no-meta batch must be degraded")
	}
}

// ── BLOCKER 3 (round 2): Degradation reason preservation ──

func TestCodexAdapter_MixedBatch_ReasonPreserved(t *testing.T) {
	// Mixed valid+malformed batch must preserve a specific degradation reason,
	// not just an empty degraded flag.
	a := &Adapter{}
	records := []contract.RawRecord{
		sessionMeta0_144_1(),
		codexRec(`{"timestamp":"2026-07-06T13:29:35.500Z","type":"unknown_msg_type","payload":{}}`),
	}
	res, _ := a.ReadEvents(context.Background(), contract.ReadInput{
		Session: contract.SessionContext{SessionID: "pokit:host-a"},
		Records: records,
	})
	if !res.Degraded.Degraded {
		t.Fatal("batch with unknown type must be degraded")
	}
	// The reason must contain something about the unknown type, not just be empty.
	if res.Degraded.Reason == "" {
		t.Error("degraded reason is empty — per-record reason was lost")
	}
	if !contains(res.Degraded.Reason, "unknown") && !contains(res.Degraded.Reason, "unknown_msg_type") {
		t.Errorf("degraded reason missing specific cause: %q", res.Degraded.Reason)
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
