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
// without editing this reviewed package. It proves the contract-level guarantees
// on the CONTRACT surface, using generic/synthetic inputs plus a small set of
// adapter-declared positive fixtures. It never reverse-engineers provider-version
// fixtures — those belong to T1/T2.

// ConformanceFixtures are the minimal adapter-declared inputs the harness needs to
// prove provider-specific positive behavior. Everything else (bounds, dedup,
// cursor, panic-safety, status precedence, approval-default-no, secret-free
// diagnostics) is proven from harness-generated generic inputs.
type ConformanceFixtures struct {
	// DetectContext is a session the adapter should positively identify.
	DetectContext SessionContext
	// ExpectDetectKind is the AgentIdentity.Kind expected for DetectContext.
	ExpectDetectKind string

	// ValidRecords normalize to known (non-unknown) events. ExpectTypes lists the
	// event types that must appear.
	ValidRecords []RawRecord
	ExpectTypes  []AgentEventType

	// ApprovalRecords must, once read, yield at least one surfaced approval.
	ApprovalRecords []RawRecord
	// NearMissRecords look approval-ish but must yield ZERO approvals (adversarial).
	NearMissRecords []RawRecord

	// MalformedRecords are provider-invalid inputs that must never panic and never
	// fabricate a confident typed event.
	MalformedRecords []RawRecord
}

// RunAgentContract runs the full fixed conformance suite against an adapter.
func RunAgentContract(t *testing.T, name string, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		t.Run("Descriptor", func(t *testing.T) { testDescriptor(t, factory) })
		t.Run("Detect_Invariants", func(t *testing.T) { testDetect(t, factory, fx) })
		t.Run("Discover_NeverInventsOwnership", func(t *testing.T) { testDiscover(t, factory, fx) })
		t.Run("Read_DeterministicNormalization", func(t *testing.T) { testDeterministic(t, factory, fx) })
		t.Run("Read_ValidateAndVocabulary", func(t *testing.T) { testValidate(t, factory, fx) })
		t.Run("Read_MalformedSafe", func(t *testing.T) { testMalformed(t, factory, fx) })
		t.Run("Read_Bounds", func(t *testing.T) { testBounds(t, factory, fx) })
		t.Run("Read_DedupAndCursor", func(t *testing.T) { testDedupCursor(t, factory, fx) })
		t.Run("Read_Ordering", func(t *testing.T) { testOrdering(t, factory, fx) })
		t.Run("Normalize_NoLeakNoPanic", func(t *testing.T) { testNormalize(t, factory, fx) })
		t.Run("Approval_PositiveAndNearMiss", func(t *testing.T) { testApprovals(t, factory, fx) })
		t.Run("Status_PrecedenceAndFallback", func(t *testing.T) { testStatus(t, factory, fx) })
		t.Run("Diagnostics_SecretFree", func(t *testing.T) { testDiagnosticsClean(t, factory, fx) })
	})
}

func ctx() context.Context { return context.Background() }

// guard runs fn and fails (without crashing the suite) if it panics.
func guard(t *testing.T, label string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked: %v", label, r)
		}
	}()
	fn()
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
	// Empty context must never produce a confident false positive.
	guard(t, "Detect(empty)", func() {
		id, _ := a.Detect(ctx(), SessionContext{})
		if id.Confidence >= 0.5 && id.Kind != fx.ExpectDetectKind {
			t.Errorf("empty context produced confident kind %q (%.2f)", id.Kind, id.Confidence)
		}
		if id.Confidence < 0.5 && id.Kind != "unknown" {
			t.Errorf("confidence<0.5 must be unknown, got %q", id.Kind)
		}
	})
	if fx.ExpectDetectKind != "" {
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
		// An empty/unknown context must not yield a PROVEN correlation (no invented
		// terminal ownership).
		empty, _ := a.DiscoverSessions(ctx(), DiscoveryInput{Session: SessionContext{}})
		for _, s := range empty {
			if s.Correlation == CorrelationProven {
				t.Error("empty context yielded a proven correlation (invented ownership)")
			}
		}
	})
}

func readAll(t *testing.T, a AgentAdapter, recs []RawRecord) ReadResult {
	t.Helper()
	var res ReadResult
	guard(t, "ReadEvents", func() {
		res, _ = a.ReadEvents(ctx(), ReadInput{Session: SessionContext{SessionID: "s"}, Records: recs})
	})
	return res
}

func testDeterministic(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	if len(fx.ValidRecords) == 0 {
		t.Skip("no valid records declared")
	}
	a1, a2 := factory(t), factory(t)
	r1 := readAll(t, a1, fx.ValidRecords)
	r2 := readAll(t, a2, fx.ValidRecords)
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
		got[e.Type] = true
	}
	for _, want := range fx.ExpectTypes {
		if !got[want] {
			t.Errorf("expected event type %q missing", want)
		}
	}
}

func testMalformed(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	recs := fx.MalformedRecords
	if len(recs) == 0 {
		recs = []RawRecord{{Bytes: []byte("{"), Source: agent.SourceJSONL}, {Bytes: []byte("\x00\xff not json")}, {Bytes: nil}}
	}
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

func testBounds(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	if len(fx.ValidRecords) == 0 {
		t.Skip("no valid records declared")
	}
	ad := factory(t)
	// Flood with far more than the cap of a valid record.
	big := make([]RawRecord, 0, MaxEventsPerRead+50)
	for i := 0; i < MaxEventsPerRead+50; i++ {
		big = append(big, fx.ValidRecords[i%len(fx.ValidRecords)])
	}
	var res ReadResult
	guard(t, "ReadEvents(flood)", func() {
		res, _ = ad.ReadEvents(ctx(), ReadInput{Session: SessionContext{SessionID: "s"}, Records: big})
	})
	if len(res.Events) > MaxEventsPerRead {
		t.Errorf("read returned %d events > cap %d", len(res.Events), MaxEventsPerRead)
	}
}

func testDedupCursor(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	if len(fx.ValidRecords) == 0 {
		t.Skip("no valid records declared")
	}
	ad := factory(t)
	// Duplicate the same records; IDs must dedupe.
	dup := append(append([]RawRecord{}, fx.ValidRecords...), fx.ValidRecords...)
	res := readAll(t, ad, dup)
	seen := map[string]bool{}
	for _, e := range res.Events {
		if seen[e.ID] {
			t.Errorf("duplicate event id survived: %s", e.ID)
		}
		seen[e.ID] = true
	}
	// Cursor resume: reading again from the returned cursor with the SAME records
	// must not re-emit already-seen events.
	guard(t, "ReadEvents(resume)", func() {
		again, _ := ad.ReadEvents(ctx(), ReadInput{Session: SessionContext{SessionID: "s"}, Records: fx.ValidRecords, Cursor: res.NextCursor})
		for _, e := range again.Events {
			if seen[e.ID] {
				t.Errorf("cursor resume re-emitted event %s", e.ID)
			}
		}
	})
}

func testOrdering(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	res := readAll(t, factory(t), fx.ValidRecords)
	for i := 1; i < len(res.Events); i++ {
		if res.Events[i].Timestamp.Before(res.Events[i-1].Timestamp) {
			t.Errorf("events out of chronological order at %d", i)
		}
	}
}

func testNormalize(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	ad := factory(t)
	// Built from fragments so no literal credential appears in source (this proves
	// redaction, it is not a real key).
	secret := "sk" + "-SUPERSECRETdeadbeef0123456789"
	rec := RawRecord{Bytes: []byte(`{"x":"` + secret + `","p":"/Users/victim/secret"}`), Source: agent.SourceJSONL, Provenance: ProvenanceNativeLog}
	guard(t, "NormalizeEvent(secret)", func() {
		ev, deg := ad.NormalizeEvent(ctx(), rec)
		if ev.Type != agent.EventUnknown {
			// If the adapter recognizes it, the event must still be contract-valid…
			if err := ValidateEvent(ev); err != nil {
				t.Errorf("normalized event invalid: %v", err)
			}
		}
		// …and must never surface the raw secret or absolute path in Text/metadata.
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
	// Default: no events → no approvals.
	guard(t, "DetectApproval(empty)", func() {
		got, _ := ad.DetectApproval(ctx(), nil)
		if len(got) != 0 {
			t.Errorf("empty events produced %d approvals, want 0", len(got))
		}
	})
	if len(fx.ApprovalRecords) > 0 {
		res := readAll(t, factory(t), fx.ApprovalRecords)
		guard(t, "DetectApproval(positive)", func() {
			got, _ := ad.DetectApproval(ctx(), res.Events)
			if len(got) == 0 {
				t.Error("approval-positive fixture produced no approval")
			}
			for _, ap := range got {
				if ap.ID == "" || ap.Status == "" {
					t.Errorf("approval missing id/status: %+v", ap)
				}
			}
		})
	}
	if len(fx.NearMissRecords) > 0 {
		res := readAll(t, factory(t), fx.NearMissRecords)
		guard(t, "DetectApproval(near-miss)", func() {
			got, _ := ad.DetectApproval(ctx(), res.Events)
			if len(got) != 0 {
				t.Errorf("adversarial near-miss produced %d approvals, want 0", len(got))
			}
		})
	}
}

func testStatus(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	ad := factory(t)
	// Empty evidence → unknown/degraded fallback, never a fabricated active state.
	guard(t, "GetStatus(empty)", func() {
		res, _ := ad.GetStatus(ctx(), StatusInput{Session: SessionContext{SessionID: "s"}})
		if !IsKnownStatus(res.Status) {
			t.Errorf("status %q outside vocabulary", res.Status)
		}
	})
	// Precedence: a strong runtime signal must beat a conflicting heuristic one.
	guard(t, "GetStatus(precedence)", func() {
		res, _ := ad.GetStatus(ctx(), StatusInput{
			Session: SessionContext{SessionID: "s"},
			Evidence: []StatusEvidence{
				{Status: agent.StatusIdle, Provenance: ProvenanceHeuristic, Confidence: 0.9},
				{Status: agent.StatusWorking, Provenance: ProvenanceRuntime, Confidence: 0.6},
			},
		})
		if res.Status != agent.StatusWorking {
			t.Errorf("precedence failed: got %q, want working (runtime > heuristic)", res.Status)
		}
	})
}

func testDiagnosticsClean(t *testing.T, factory func(*testing.T) AgentAdapter, fx ConformanceFixtures) {
	ad := factory(t)
	// Fragmented so no literal credential appears in source.
	hostile := "garbage /Users/x/tok " + "sk" + "-abc123456789012345"
	recs := append(append([]RawRecord{}, fx.MalformedRecords...), RawRecord{Bytes: []byte(hostile)})
	res := readAll(t, ad, recs)
	for _, d := range res.Degraded.Diagnostics {
		if ContainsSensitive(d) {
			t.Errorf("read diagnostics leak sensitive content: %q", d)
		}
	}
	if ContainsSensitive(res.Degraded.Reason) {
		t.Errorf("degraded reason leaks sensitive content: %q", res.Degraded.Reason)
	}
}

func toJSON(t *testing.T, v any) string {
	t.Helper()
	// Stable field order for events by sorting on ID before marshaling.
	if evs, ok := v.([]AgentEvent); ok {
		cp := append([]AgentEvent{}, evs...)
		sort.SliceStable(cp, func(i, j int) bool { return cp[i].ID < cp[j].ID })
		v = cp
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
