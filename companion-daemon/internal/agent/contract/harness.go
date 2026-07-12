package contract

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"devremote/companion-daemon/internal/agent"
)

// This file is the FIXED T0 conformance harness. It lives in the contract package
// — outside any version-specific adapter's write area — and is imported by every
// adapter's own test (T1 Codex, T2 Claude, ...). An adapter cannot weaken it
// without editing this reviewed package.
//
// Design goal: a no-op or cheating adapter CANNOT pass. The harness (1) requires
// capability-appropriate fixtures up front (missing fixtures fail, never skip),
// (2) uses harness-GENERATED distinct/adversarial inputs for the safety-critical
// checks (bounds, dedupe, cursor resume, approval/status authority, failure
// isolation) rather than trusting adapter-supplied fixtures, and (3) validates
// the adapter's actual RESULTS against the contract, not the adapter's internal
// gate.

// ConformanceFixtures are the adapter-declared inputs the harness needs. The
// safety-critical checks do NOT rely on these; they exist for provider-positive
// behavior and format-specific record generation.
type ConformanceFixtures struct {
	// DetectContext is a session the adapter should positively identify.
	DetectContext SessionContext
	// ExpectDetectKind is the AgentIdentity.Kind expected for DetectContext.
	ExpectDetectKind string

	// ValidRecords normalize to known (non-unknown) events. ExpectTypes lists the
	// event types that must appear. REQUIRED.
	ValidRecords []RawRecord
	ExpectTypes  []AgentEventType

	// DistinctRecord returns the i-th DISTINCT valid record (distinct id/content).
	// REQUIRED — the harness uses it to generate more records than any cap so
	// bounds/dedupe/cursor are proven with real distinct inputs, not repeats.
	DistinctRecord func(i int) RawRecord

	// FailingFactory builds an adapter whose ReadEvents fails (returns a typed
	// degraded result). REQUIRED — proves failure isolation generically.
	FailingFactory func(t *testing.T) AgentAdapter

	// ApprovalRecords must, once read, yield at least one surfaced approval.
	// REQUIRED when the descriptor declares CapApprovalDetection.
	ApprovalRecords []RawRecord
	// NearMissRecords look approval-ish but must yield ZERO approvals (optional;
	// the harness ALSO runs its own generic adversarial approval cases).
	NearMissRecords []RawRecord

	// MalformedRecords are provider-invalid inputs. Optional; the harness adds
	// generic malformed inputs regardless.
	MalformedRecords []RawRecord
}

// RunAgentContract runs the full fixed conformance suite against an adapter.
func RunAgentContract(t *testing.T, name string, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	t.Helper()
	requireFixtures(t, factory(t).Descriptor(), fx)
	t.Run(name, func(t *testing.T) {
		t.Run("Descriptor", func(t *testing.T) { testDescriptor(t, factory) })
		t.Run("Detect_EmptyIsUnknown", func(t *testing.T) { testDetect(t, factory, fx) })
		t.Run("Discover_BoundedNoInventedOwnership", func(t *testing.T) { testDiscover(t, factory, fx) })
		t.Run("Read_DeterministicNormalization", func(t *testing.T) { testDeterministic(t, factory, fx) })
		t.Run("Read_ValidateVocabularyProvenance", func(t *testing.T) { testValidate(t, factory, fx) })
		t.Run("Read_StableSeqOrdering", func(t *testing.T) { testOrdering(t, factory, fx) })
		t.Run("Read_MalformedSafe", func(t *testing.T) { testMalformed(t, factory, fx) })
		t.Run("Read_HardBoundDegrades", func(t *testing.T) { testHardBound(t, factory, fx) })
		t.Run("Read_CallerLimitHonored", func(t *testing.T) { testCallerLimit(t, factory, fx) })
		t.Run("Read_DedupStableID", func(t *testing.T) { testDedupStableID(t, factory, fx) })
		t.Run("Read_CursorBoundedResumeNoReemit", func(t *testing.T) { testCursorResume(t, factory, fx) })
		t.Run("Read_SessionIDMustMatch", func(t *testing.T) { testSessionBinding(t, factory, fx) })
		t.Run("Read_BoundsEnforced_OversizedRecord", func(t *testing.T) { testOversizedRecord(t, factory, fx) })
		t.Run("Read_BoundsEnforced_OversizedBatch", func(t *testing.T) { testOversizedBatch(t, factory, fx) })
		t.Run("Read_BoundsEnforced_InvalidCursor", func(t *testing.T) { testInvalidCursor(t, factory, fx) })
		t.Run("Normalize_NoLeakNoPanic", func(t *testing.T) { testNormalize(t, factory, fx) })
		t.Run("Approval_GenericAdversarial", func(t *testing.T) { testApprovals(t, factory, fx) })
		t.Run("Status_PrecedenceAndAdvisoryPolicy", func(t *testing.T) { testStatus(t, factory, fx) })
		t.Run("Diagnostics_SecretFree", func(t *testing.T) { testDiagnosticsClean(t, factory, fx) })
		t.Run("FailureIsolation", func(t *testing.T) { testFailureIsolation(t, factory, fx) })
	})
}

// requireFixtures fails (never skips) when a capability's mandatory fixtures are
// absent, so an under-specified suite cannot hide behind t.Skip.
func requireFixtures(t *testing.T, d AgentAdapterDescriptor, fx ConformanceFixtures) {
	t.Helper()
	if fx.DistinctRecord == nil {
		t.Fatal("ConformanceFixtures.DistinctRecord is required")
	}
	if fx.FailingFactory == nil {
		t.Fatal("ConformanceFixtures.FailingFactory is required")
	}
	if len(fx.ValidRecords) == 0 || len(fx.ExpectTypes) == 0 {
		t.Fatal("ConformanceFixtures.ValidRecords and ExpectTypes are required")
	}
	if fx.ExpectDetectKind == "" {
		t.Fatal("ConformanceFixtures.ExpectDetectKind is required")
	}
	for _, c := range d.Capabilities {
		if c == CapApprovalDetection && len(fx.ApprovalRecords) == 0 {
			t.Fatal("CapApprovalDetection declared but no ApprovalRecords fixture")
		}
	}
}

func ctx() context.Context { return context.Background() }

func guard(t *testing.T, label string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked: %v", label, r)
		}
	}()
	fn()
}

func distinct(fx ConformanceFixtures, n int) []RawRecord {
	recs := make([]RawRecord, 0, n)
	for i := 0; i < n; i++ {
		recs = append(recs, fx.DistinctRecord(i))
	}
	return recs
}

func readWith(t *testing.T, a AgentAdapter, in ReadInput) ReadResult {
	t.Helper()
	var res ReadResult
	guard(t, "ReadEvents", func() { res, _ = a.ReadEvents(ctx(), in) })
	return res
}

func readAll(t *testing.T, a AgentAdapter, recs []RawRecord) ReadResult {
	return readWith(t, a, ReadInput{Session: SessionContext{SessionID: "s"}, Records: recs})
}

func testDescriptor(t *testing.T, factory func(*testing.T) AgentAdapter) {
	d := factory(t).Descriptor()
	if d.Name == "" {
		t.Error("Descriptor.Name empty")
	}
	if d.ContractVersion != ContractVersion {
		t.Errorf("Descriptor.ContractVersion=%q, want %q", d.ContractVersion, ContractVersion)
	}
	for _, c := range d.Capabilities {
		switch c {
		case CapEvents, CapStatus, CapToolCallDetection, CapApprovalDetection,
			CapIncrementalRead, CapScreenFallback, CapProcessDetection, CapLogDetection:
		default:
			t.Errorf("unknown capability %q", c)
		}
	}
}

func testDetect(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	a := factory(t)
	// Empty context has NO evidence: it must ALWAYS be unknown AND low confidence.
	guard(t, "Detect(empty)", func() {
		id, _ := a.Detect(ctx(), SessionContext{})
		if id.Kind != "unknown" {
			t.Errorf("empty context kind=%q, want unknown", id.Kind)
		}
		if id.Confidence >= 0.5 {
			t.Errorf("empty context confidence=%.2f, want < 0.5", id.Confidence)
		}
	})
	guard(t, "Detect(fixture)", func() {
		id, err := a.Detect(ctx(), fx.DetectContext)
		if err != nil {
			t.Fatalf("Detect fixture: %v", err)
		}
		if id.Kind != fx.ExpectDetectKind {
			t.Errorf("Detect kind=%q, want %q", id.Kind, fx.ExpectDetectKind)
		}
	})
}

func testDiscover(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	a := factory(t)
	guard(t, "DiscoverSessions", func() {
		got, _ := a.DiscoverSessions(ctx(), DiscoveryInput{Session: fx.DetectContext, Limit: MaxDiscoveredSessions})
		if len(got) > MaxDiscoveredSessions {
			t.Errorf("discovery returned %d > cap %d", len(got), MaxDiscoveredSessions)
		}
		for _, s := range got {
			switch s.Correlation {
			case CorrelationProven, CorrelationManagedLaunch, CorrelationUnavailable:
			default:
				t.Errorf("invalid correlation %q", s.Correlation)
			}
		}
		// caller Limit must be honored.
		limited, _ := a.DiscoverSessions(ctx(), DiscoveryInput{Session: fx.DetectContext, Limit: 1})
		if len(limited) > 1 {
			t.Errorf("caller Limit=1 not honored: got %d", len(limited))
		}
		// Empty/unknown context must yield ZERO results OR everything CorrelationUnavailable.
		// Neither Proven nor ManagedLaunch is legitimate without evidence.
		empty, _ := a.DiscoverSessions(ctx(), DiscoveryInput{Session: SessionContext{}})
		for _, s := range empty {
			if s.Correlation == CorrelationProven || s.Correlation == CorrelationManagedLaunch {
				t.Errorf("empty context yielded %s correlation — must be Unavailable or return nothing", s.Correlation)
			}
		}
	})
}

func testDeterministic(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	r1 := readAll(t, factory(t), fx.ValidRecords)
	r2 := readAll(t, factory(t), fx.ValidRecords)
	if toJSON(t, r1.Events) != toJSON(t, r2.Events) {
		t.Error("normalization is not deterministic across fresh adapters")
	}
}

func testValidate(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	res := readAll(t, factory(t), fx.ValidRecords)
	got := map[AgentEventType]bool{}
	for _, e := range res.Events {
		if err := ValidateEvent(e); err != nil {
			t.Errorf("emitted event fails ValidateEvent: %v (%+v)", err, e)
		}
		if !IsKnownProvenance(Provenance(e.Provenance)) {
			t.Errorf("event has no valid provenance: %+v", e)
		}
		got[e.Type] = true
	}
	for _, want := range fx.ExpectTypes {
		if !got[want] {
			t.Errorf("expected event type %q missing", want)
		}
	}
}

func testOrdering(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	// Distinct records with distinct ordering keys → strictly increasing Seq
	// (stable ordering, disambiguating equal timestamps).
	res := readAll(t, factory(t), distinct(fx, 8))
	for i := 1; i < len(res.Events); i++ {
		if res.Events[i].Seq <= res.Events[i-1].Seq {
			t.Errorf("Seq not strictly increasing at %d: %d <= %d", i, res.Events[i].Seq, res.Events[i-1].Seq)
		}
	}
}

func testMalformed(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	recs := append([]RawRecord{
		{Bytes: []byte("{"), Source: agent.SourceJSONL},
		{Bytes: []byte("\x00\xff not json")},
		{Bytes: nil},
	}, fx.MalformedRecords...)
	res := readAll(t, factory(t), recs)
	for _, e := range res.Events {
		if e.Type != agent.EventUnknown && e.Confidence >= 0.4 {
			t.Errorf("malformed input fabricated a confident typed event: %+v", e)
		}
		if err := ValidateEvent(e); err != nil {
			t.Errorf("malformed-derived event invalid: %v", err)
		}
	}
}

// ── B1: bound enforcement — the harness forces the adapter to honour AcceptRecord,
//     BoundBatch, and ValidateCursor; silent acceptance is a contract violation.

func testSessionBinding(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	// Every emitted event MUST carry the EXACT requested session ID.
	reqSession := "controlled_pty:host-a"
	res := readWith(t, factory(t), ReadInput{Session: SessionContext{SessionID: reqSession}, Records: fx.ValidRecords})
	for _, e := range res.Events {
		if e.SessionID != reqSession {
			t.Errorf("event SessionID=%q, want %q (cross-session evidence)", e.SessionID, reqSession)
		}
	}
}

func testOversizedRecord(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	// A record exceeding MaxRecordBytes must produce ZERO events and either a
	// degraded result or an error. Silently normalizing / dropping it is not enough.
	big := RawRecord{Bytes: make([]byte, MaxRecordBytes+1), Source: agent.SourceJSONL}
	var res ReadResult
	var err error
	guard(t, "ReadEvents(oversized-record)", func() {
		res, err = factory(t).ReadEvents(ctx(), ReadInput{Session: SessionContext{SessionID: "s"}, Records: []RawRecord{big}})
	})
	if err != nil {
		return // error is an acceptable fail-closed path
	}
	if len(res.Events) != 0 {
		t.Errorf("oversized record produced %d events, want 0", len(res.Events))
	}
	if !res.Degraded.Degraded {
		t.Error("oversized record must set Degraded=true")
	}
}

func testOversizedBatch(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	// A batch exceeding MaxBatchRecords must be capped and must report a degraded
	// result. The output event cap is tested separately (testHardBound); this
	// proves the adapter honours the INPUT batch cap.
	many := make([]RawRecord, MaxBatchRecords+25)
	for i := range many {
		many[i] = fx.DistinctRecord(i)
	}
	var res ReadResult
	var err error
	guard(t, "ReadEvents(oversized-batch)", func() {
		res, err = factory(t).ReadEvents(ctx(), ReadInput{Session: SessionContext{SessionID: "s"}, Records: many})
	})
	if err != nil {
		t.Fatalf("oversized batch must not error, it degrades: %v", err)
	}
	if !res.Degraded.Degraded {
		t.Error("oversized batch must set Degraded=true")
	}
	if len(res.Events) > MaxEventsPerRead {
		t.Errorf("oversized batch leaked %d events > hard cap %d", len(res.Events), MaxEventsPerRead)
	}
}

func testInvalidCursor(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	a := factory(t)
	// Oversized cursor → must fail-closed (no events, no full replay), either via
	// an error or a degraded result.
	in := ReadInput{Session: SessionContext{SessionID: "s"}, Records: fx.ValidRecords, Cursor: Cursor(string(make([]byte, MaxCursorBytes+1)))}
	var res ReadResult
	var err error
	guard(t, "ReadEvents(oversized-cursor)", func() {
		res, err = a.ReadEvents(ctx(), in)
	})
	if err == nil && !res.Degraded.Degraded {
		t.Error("oversized cursor must degrade or error (not silently accepted)")
	}
	// An oversized cursor that still emits events is replaying history — that is
	// a bypass of the bounded-read guarantee.
	if len(res.Events) != 0 {
		t.Errorf("oversized cursor replayed %d events, want 0 (fail-closed)", len(res.Events))
	}
	// Invalid-UTF8 cursor must also not silently replay.
	in.Cursor = Cursor("\xff\xfe")
	guard(t, "ReadEvents(invalid-utf8-cursor)", func() {
		res, err = a.ReadEvents(ctx(), in)
	})
	if err == nil && !res.Degraded.Degraded {
		t.Error("invalid-UTF8 cursor must degrade or error")
	}
	if len(res.Events) != 0 {
		t.Errorf("invalid-UTF8 cursor replayed %d events, want 0", len(res.Events))
	}
}

func testHardBound(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	// DISTINCT records well beyond the cap: the adapter must truncate to the cap
	// AND report degraded (never silent).
	res := readAll(t, factory(t), distinct(fx, MaxEventsPerRead+37))
	if len(res.Events) > MaxEventsPerRead {
		t.Fatalf("read returned %d events > cap %d", len(res.Events), MaxEventsPerRead)
	}
	if !res.Degraded.Degraded {
		t.Error("truncation at the hard cap must set Degraded=true")
	}
}

func testCallerLimit(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	res := readWith(t, factory(t), ReadInput{Session: SessionContext{SessionID: "s"}, Records: distinct(fx, 20), MaxEvents: 5})
	if len(res.Events) > 5 {
		t.Errorf("caller MaxEvents=5 not honored: got %d", len(res.Events))
	}
}

func testDedupStableID(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	// The SAME distinct record fed twice must collapse to ONE event. An adapter
	// that re-issues a fresh id per identical record fails here.
	r := fx.DistinctRecord(0)
	res := readAll(t, factory(t), []RawRecord{r, r})
	if len(res.Events) != 1 {
		t.Errorf("identical record yielded %d events, want 1 (unstable id / no dedupe)", len(res.Events))
	}
}

func testCursorResume(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	ad := factory(t)
	recs := distinct(fx, 12)
	res := readAll(t, ad, recs)
	if err := ValidateCursor(res.NextCursor); err != nil {
		t.Errorf("NextCursor invalid: %v", err)
	}
	if len(res.NextCursor) > MaxCursorBytes {
		t.Errorf("NextCursor exceeds MaxCursorBytes: %d", len(res.NextCursor))
	}
	// Re-reading the SAME records from the returned cursor must re-emit NOTHING.
	guard(t, "ReadEvents(resume)", func() {
		again, _ := ad.ReadEvents(ctx(), ReadInput{Session: SessionContext{SessionID: "s"}, Records: recs, Cursor: res.NextCursor})
		if len(again.Events) != 0 {
			t.Errorf("cursor resume re-emitted %d events, want 0", len(again.Events))
		}
	})
	// A large read must still yield a bounded cursor (no unbounded id accumulation).
	big := readAll(t, factory(t), distinct(fx, MaxEventsPerRead))
	if len(big.NextCursor) > MaxCursorBytes {
		t.Errorf("cursor grew unbounded on a large read: %d bytes", len(big.NextCursor))
	}
}

func testNormalize(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	ad := factory(t)
	secret := "sk" + "-SUPERSECRETdeadbeef0123456789"
	rec := RawRecord{Bytes: []byte(`{"x":"` + secret + `","p":"/Users/victim/secret"}`), Source: agent.SourceJSONL, Provenance: ProvenanceNativeLog}
	guard(t, "NormalizeEvent(secret)", func() {
		ev, deg := ad.NormalizeEvent(ctx(), rec)
		if ev.Type != agent.EventUnknown {
			if err := ValidateEvent(ev); err != nil {
				t.Errorf("normalized event invalid: %v", err)
			}
		}
		if ContainsSensitive(ev.Text) {
			t.Error("normalized event Text leaks a secret/path")
		}
		for _, v := range ev.Metadata {
			if ContainsSensitive(v) {
				t.Error("normalized event Metadata leaks a secret/path")
			}
		}
		for _, d := range deg.Diagnostics {
			if ContainsSensitive(d) {
				t.Error("degraded diagnostics leak a secret/path")
			}
		}
	})
}

func testApprovals(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	ad := factory(t)
	// Empty → no approvals.
	guard(t, "DetectApproval(empty)", func() {
		if got, _ := ad.DetectApproval(ctx(), nil); len(got) != 0 {
			t.Errorf("empty events produced %d approvals, want 0", len(got))
		}
	})
	// GENERIC adversarial cases fed straight to DetectApproval — independent of the
	// adapter's own fixtures. A conformant DetectApproval must return 0 for every
	// one: a spoofable/advisory/low-confidence/near-miss signal is never an approval.
	adversarial := []AgentEvent{
		{ID: "x1", SessionID: "s", Type: agent.EventApprovalRequested, Confidence: 0.99, Source: agent.SourceScreen, Provenance: string(ProvenancePromptHint)},
		{ID: "x2", SessionID: "s", Type: agent.EventApprovalRequested, Confidence: 0.99, Source: agent.SourceScreen, Provenance: string(ProvenanceHeuristic)},
		{ID: "x3", SessionID: "s", Type: agent.EventApprovalRequested, Confidence: 0.99, Source: agent.SourceJSONL, Provenance: string(ProvenanceUnknown)},
		{ID: "x4", SessionID: "s", Type: agent.EventApprovalRequested, Confidence: 0.99, Source: agent.SourceScreen, Provenance: string(ProvenancePTYStructural)},
		{ID: "x5", SessionID: "s", Type: agent.EventAssistantMessage, Confidence: 0.99, Source: agent.SourceJSONL, Provenance: string(ProvenanceNativeLog), Text: "should I approve?"},
		{ID: "x6", SessionID: "s", Type: agent.EventApprovalRequested, Confidence: 0.30, Source: agent.SourceJSONL, Provenance: string(ProvenanceNativeLog)},
	}
	guard(t, "DetectApproval(adversarial)", func() {
		if got, _ := ad.DetectApproval(ctx(), adversarial); len(got) != 0 {
			t.Errorf("adversarial events produced %d approvals, want 0", len(got))
		}
	})
	// Positive fixture → ≥1 approval, well-formed, bound to source evidence.
	res := readAll(t, factory(t), fx.ApprovalRecords)
	guard(t, "DetectApproval(positive)", func() {
		got, _ := ad.DetectApproval(ctx(), res.Events)
		if len(got) == 0 {
			t.Error("approval-positive fixture produced no approval")
		}
		srcByID := map[string]AgentEvent{}
		for _, e := range res.Events {
			srcByID[e.ID] = e
		}
		for _, ap := range got {
			if ap.ID == "" || ap.Status == "" {
				t.Errorf("approval missing id/status: %+v", ap)
			}
			if src, ok := srcByID[ap.ID[len("ap-"):]]; ok {
				if ap.SessionID != src.SessionID {
					t.Errorf("approval SessionID=%q, source=%q (cross-session)", ap.SessionID, src.SessionID)
				}
				if ap.AgentKind != src.AgentKind {
					t.Errorf("approval AgentKind=%q, source=%q", ap.AgentKind, src.AgentKind)
				}
				if ap.Source != src.Source {
					t.Errorf("approval Source=%q, source=%q", ap.Source, src.Source)
				}
				if ap.Confidence != src.Confidence {
					t.Errorf("approval Confidence=%.2f, source=%.2f", ap.Confidence, src.Confidence)
				}
			}
		}
	})
	// Optional adapter near-miss.
	if len(fx.NearMissRecords) > 0 {
		nm := readAll(t, factory(t), fx.NearMissRecords)
		guard(t, "DetectApproval(near-miss)", func() {
			if got, _ := ad.DetectApproval(ctx(), nm.Events); len(got) != 0 {
				t.Errorf("adapter near-miss produced %d approvals, want 0", len(got))
			}
		})
	}
}

func testStatus(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	ad := factory(t)
	guard(t, "GetStatus(empty)", func() {
		res, _ := ad.GetStatus(ctx(), StatusInput{Session: SessionContext{SessionID: "s"}})
		if !IsKnownStatus(res.Status) {
			t.Errorf("status %q outside vocabulary", res.Status)
		}
		if terminalStatuses[res.Status] && res.Confidence >= 0.5 {
			t.Error("empty evidence produced a confident terminal status")
		}
	})
	// Precedence: strong runtime beats a higher-confidence heuristic.
	guard(t, "GetStatus(precedence)", func() {
		res, _ := ad.GetStatus(ctx(), StatusInput{Session: SessionContext{SessionID: "s"}, Evidence: []StatusEvidence{
			{Status: agent.StatusIdle, Provenance: ProvenanceHeuristic, Confidence: 0.9},
			{Status: agent.StatusWorking, Provenance: ProvenanceRuntime, Confidence: 0.6},
		}})
		if res.Status != agent.StatusWorking {
			t.Errorf("precedence failed: got %q, want working", res.Status)
		}
	})
	// Advisory-alone policy: heuristic / prompt_hint / unknown provenance MUST NOT
	// authoritatively declare a terminal status (completed/failed/interrupted).
	for _, ev := range []StatusEvidence{
		{Status: agent.StatusCompleted, Provenance: ProvenancePromptHint, Confidence: 0.99},
		{Status: agent.StatusCompleted, Provenance: ProvenancePromptHint, Confidence: 0.50},
		{Status: agent.StatusCompleted, Provenance: ProvenanceHeuristic, Confidence: 0.99},
		{Status: agent.StatusFailed, Provenance: ProvenanceHeuristic, Confidence: 0.80},
		{Status: agent.StatusCompleted, Provenance: ProvenanceUnknown, Confidence: 0.95},
		{Status: agent.StatusInterrupted, Provenance: ProvenancePromptHint, Confidence: 0.75},
	} {
		guard(t, "GetStatus(advisory-terminal)", func() {
			res, _ := ad.GetStatus(ctx(), StatusInput{Session: SessionContext{SessionID: "s"}, Evidence: []StatusEvidence{ev}})
			if res.Status == ev.Status && res.Confidence > AdvisoryStatusConfidenceCeiling && !res.Degraded.Degraded {
				t.Errorf("advisory %s asserted %s at confidence %.2f without degraded — must be unknown+degraded", ev.Provenance, ev.Status, res.Confidence)
			}
			if res.Status == ev.Status && !res.Degraded.Degraded {
				t.Errorf("advisory %s asserted %s without degraded flag (conf=%.2f)", ev.Provenance, ev.Status, res.Confidence)
			}
			if res.Confidence > AdvisoryStatusConfidenceCeiling {
				t.Errorf("advisory %s confidence %.2f exceeds ceiling %.2f", ev.Provenance, res.Confidence, AdvisoryStatusConfidenceCeiling)
			}
			if res.Provenance != ev.Provenance {
				t.Errorf("returned provenance %q != input %q", res.Provenance, ev.Provenance)
			}
		})
	}
	// Strong evidence wins over advisory; the winning provenance+confidence must
	// reflect the strong source, not the overridden advisory.
	guard(t, "GetStatus(strong-over-advisory)", func() {
		res, _ := ad.GetStatus(ctx(), StatusInput{Session: SessionContext{SessionID: "s"}, Evidence: []StatusEvidence{
			{Status: agent.StatusCompleted, Provenance: ProvenancePromptHint, Confidence: 0.99},
			{Status: agent.StatusWorking, Provenance: ProvenanceRuntime, Confidence: 0.60},
		}})
		if res.Status != agent.StatusWorking {
			t.Errorf("strong evidence lost precedence: got %s", res.Status)
		}
		if res.Provenance != ProvenanceRuntime {
			t.Errorf("winning provenance %q, want runtime", res.Provenance)
		}
		if res.Confidence != 0.60 {
			t.Errorf("winning confidence %.2f, want 0.60", res.Confidence)
		}
	})
}

func testDiagnosticsClean(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	hostile := "garbage /Users/x/tok " + "sk" + "-abc123456789012345"
	recs := append([]RawRecord{{Bytes: []byte(hostile)}}, fx.MalformedRecords...)
	res := readAll(t, factory(t), recs)
	for _, d := range res.Degraded.Diagnostics {
		if ContainsSensitive(d) {
			t.Errorf("read diagnostics leak sensitive content: %q", d)
		}
	}
	if ContainsSensitive(res.Degraded.Reason) {
		t.Errorf("degraded reason leaks sensitive content: %q", res.Degraded.Reason)
	}
}

func testFailureIsolation(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	failing := fx.FailingFactory(t)
	var res ReadResult
	var err error
	guard(t, "ReadEvents(failing)", func() {
		res, err = failing.ReadEvents(ctx(), ReadInput{Session: SessionContext{SessionID: "s"}, Records: fx.ValidRecords})
	})
	if err != nil {
		t.Fatalf("failing adapter must degrade, not error: %v", err)
	}
	if !res.Degraded.Degraded || len(res.Events) != 0 {
		t.Errorf("failing adapter must return degraded + no events, got %+v", res)
	}
	// A healthy adapter is unaffected by the failing one.
	ok := readAll(t, factory(t), fx.ValidRecords)
	if len(ok.Events) == 0 {
		t.Error("healthy adapter affected by a separate failing adapter")
	}
}

func toJSON(t *testing.T, v any) string {
	t.Helper()
	if evs, ok := v.([]AgentEvent); ok {
		cp := append([]AgentEvent{}, evs...)
		sort.SliceStable(cp, func(i, j int) bool { return cp[i].Seq < cp[j].Seq })
		v = cp
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
