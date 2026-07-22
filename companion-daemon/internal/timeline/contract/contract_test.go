package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
)

func validEnvelope(t *testing.T) Envelope {
	t.Helper()
	scope := Scope{SessionID: "controlled_pty:one", RuntimeID: "runtime-1", LaunchGeneration: 3}
	e, err := NewEnvelope(Envelope{
		SchemaVersion: SchemaV1, PayloadVersion: PayloadV1,
		EventKind: EventToolCallStarted,
		SessionID: scope.SessionID, RuntimeID: scope.RuntimeID, LaunchGeneration: scope.LaunchGeneration,
		Provider: "codex", SourceIncarnation: "log-open-1",
		SourceIdentity: SourceIdentity{Kind: "jsonl", ID: "provider-session-1"}, SourcePosition: "cursor-9",
		OccurredAt: time.Unix(100, 0).UTC(), ObservedAt: time.Unix(200, 0).UTC(),
		RedactionPolicyVersion: "redaction-v1",
		Payload:                Payload{Redacted: &RedactedPayload{Summary: "completed a redacted operation"}},
		References:             References{ToolCall: &TypedReference{Kind: ReferenceToolCall, ID: "tool-1", Scope: scope}},
		T0Event:                agent.AgentEvent{ID: "t0-event-1", SessionID: scope.SessionID, AgentKind: "codex", Type: agent.EventToolCallStarted, RawRef: "safe-ref"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestEventIDReplayAppendAndTimestampInvariant(t *testing.T) {
	a := validEnvelope(t)
	b := a
	b.OccurredAt = b.OccurredAt.Add(24 * time.Hour)
	b.ObservedAt = b.ObservedAt.Add(48 * time.Hour)
	b.EventID = ""
	b, err := NewEnvelope(b)
	if err != nil {
		t.Fatal(err)
	}
	if a.EventID != b.EventID {
		t.Fatalf("timestamps changed event id: %q != %q", a.EventID, b.EventID)
	}
	da, _ := a.CanonicalDigest()
	db, _ := b.CanonicalDigest()
	if da != db {
		t.Fatal("timestamps changed canonical digest")
	}
	// Append order is deliberately absent from Envelope and cannot alter EventID.
	if a.EventID == "" {
		t.Fatal("event id missing")
	}
}

func TestSourceIncarnationSeparatesIdentity(t *testing.T) {
	a := validEnvelope(t)
	b := a
	b.SourceIncarnation = "log-open-2"
	b.EventID = ""
	b, err := NewEnvelope(b)
	if err != nil {
		t.Fatal(err)
	}
	if a.EventID == b.EventID {
		t.Fatal("source incarnation did not separate identity")
	}
}

func TestIdempotenceAndCollision(t *testing.T) {
	a := validEnvelope(t)
	same, err := SameEvidence(a, a)
	if err != nil || !same {
		t.Fatalf("same evidence: %v %v", same, err)
	}
	b := a
	b.Payload.Redacted = &RedactedPayload{Summary: "different safe summary"}
	if _, err := SameEvidence(a, b); err != ErrEventIDCollision {
		t.Fatalf("collision error = %v", err)
	}
}

func TestConditionalReferencesRequiredAndForbidden(t *testing.T) {
	base := validEnvelope(t)
	for _, tc := range []struct {
		kind EventKind
		ref  ReferenceKind
	}{
		{EventProviderInvocationStarted, ReferenceProviderInvocation}, {EventThreadObserved, ReferenceThread}, {EventTurnObserved, ReferenceTurn},
		{EventToolCallStarted, ReferenceToolCall}, {EventApprovalRequested, ReferenceApprovalRequest}, {EventCorrelationEstablished, ReferenceCorrelation},
		{EventStreamObserved, ReferenceTransport}, {EventDegraded, ReferenceDegraded}, {EventEvidenceObserved, ReferenceProvenance},
	} {
		e := base
		e.EventKind = tc.kind
		e.References = References{}
		e.EventID = ""
		if _, err := NewEnvelope(e); err == nil || !strings.Contains(err.Error(), "required") {
			t.Fatalf("%s accepted without required ref: %v", tc.kind, err)
		}
		e.References = referenceFor(tc.ref, Scope{SessionID: e.SessionID, RuntimeID: e.RuntimeID, LaunchGeneration: e.LaunchGeneration})
		if _, err := NewEnvelope(e); err != nil {
			t.Fatalf("%s valid reference: %v", tc.kind, err)
		}
	}
	for _, ref := range []ReferenceKind{ReferenceProviderInvocation, ReferenceThread, ReferenceTurn, ReferenceToolCall, ReferenceApprovalRequest, ReferenceCorrelation, ReferenceCausation, ReferenceTransport, ReferenceDegraded, ReferenceProvenance} {
		e := base
		e.EventKind = EventEvidenceObserved
		e.References = referenceFor(ReferenceProvenance, Scope{SessionID: e.SessionID, RuntimeID: e.RuntimeID, LaunchGeneration: e.LaunchGeneration})
		if ref != ReferenceProvenance {
			addReference(&e.References, ref, Scope{SessionID: e.SessionID, RuntimeID: e.RuntimeID, LaunchGeneration: e.LaunchGeneration})
		}
		e.EventID = ""
		if _, err := NewEnvelope(e); ref != ReferenceProvenance && (err == nil || !strings.Contains(err.Error(), "forbidden")) {
			t.Fatalf("unrelated %s reference accepted: %v", ref, err)
		}
	}
	// Causation is conditional, not universal: it is accepted only on a
	// resolution/correlation event alongside that event's required reference.
	e := base
	e.EventKind = EventApprovalResolved
	e.References = referenceFor(ReferenceApprovalRequest, Scope{SessionID: e.SessionID, RuntimeID: e.RuntimeID, LaunchGeneration: e.LaunchGeneration})
	addReference(&e.References, ReferenceCausation, Scope{SessionID: e.SessionID, RuntimeID: e.RuntimeID, LaunchGeneration: e.LaunchGeneration})
	e.EventID = ""
	if _, err := NewEnvelope(e); err != nil {
		t.Fatalf("allowed causation rejected: %v", err)
	}
}

func TestVersionsAndScopeRejected(t *testing.T) {
	e := validEnvelope(t)
	e.SchemaVersion++
	if err := e.Validate(); err != ErrUnsupportedSchema {
		t.Fatalf("schema: %v", err)
	}
	e = validEnvelope(t)
	e.PayloadVersion++
	if err := e.Validate(); err != ErrUnsupportedPayload {
		t.Fatalf("payload: %v", err)
	}
	e = validEnvelope(t)
	e.References.ToolCall.Scope.RuntimeID = "other"
	if err := e.Validate(); err == nil {
		t.Fatal("cross-runtime reference accepted")
	}
	e = validEnvelope(t)
	e.T0Event.SessionID = "other"
	if err := e.Validate(); err == nil {
		t.Fatal("cross-session wrapped event accepted")
	}
	e = validEnvelope(t)
	e.References.ToolCall.Scope.LaunchGeneration++
	if err := e.Validate(); err == nil {
		t.Fatal("cross-generation reference accepted")
	}
}

func TestReadBoundsAtNMinusOneNAndNPlusOne(t *testing.T) {
	for _, tc := range []struct {
		name     string
		n, limit int
		run      func(int) error
	}{
		{"events", MaxEventsPerRead, MaxEventsPerRead, func(n int) error { return ValidateReadBounds(n, nil, nil) }},
		{"cursor", MaxCursorBytes, MaxCursorBytes, func(n int) error { return ValidateReadBounds(0, make([]byte, n), nil) }},
		{"raw", MaxRawRecordBytes, MaxRawRecordBytes, func(n int) error { return ValidateReadBounds(0, nil, make([]byte, n)) }},
	} {
		for _, n := range []int{tc.n - 1, tc.n, tc.n + 1} {
			err := tc.run(n)
			if (n <= tc.limit) != (err == nil) {
				t.Fatalf("%s at %d: %v", tc.name, n, err)
			}
		}
	}
}

func TestRedactionFixtures(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "redaction_cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Summary string `json:"summary"`
		Valid   bool   `json:"valid"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		e := validEnvelope(t)
		e.Payload = Payload{Redacted: &RedactedPayload{Summary: tc.Summary}}
		e.EventID = ""
		_, err := NewEnvelope(e)
		if (err == nil) != tc.Valid {
			t.Fatalf("%q valid=%v err=%v", tc.Summary, tc.Valid, err)
		}
	}
	if _, err := NewEnvelope(func() Envelope {
		e := validEnvelope(t)
		e.Payload = Payload{Opaque: &OpaqueReferencePayload{Reference: "pty://digest-only"}}
		e.EventID = ""
		return e
	}()); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	if _, err := NewEnvelope(func() Envelope {
		e := validEnvelope(t)
		e.Payload = Payload{Digest: &DigestReferencePayload{Digest: digest, Bytes: 7}}
		e.EventID = ""
		return e
	}()); err != nil {
		t.Fatal(err)
	}
}

func addReference(refs *References, kind ReferenceKind, scope Scope) {
	r := &TypedReference{Kind: kind, ID: string(kind) + "-1", Scope: scope}
	switch kind {
	case ReferenceProviderInvocation:
		refs.ProviderInvocation = r
	case ReferenceThread:
		refs.Thread = r
	case ReferenceTurn:
		refs.Turn = r
	case ReferenceToolCall:
		refs.ToolCall = r
	case ReferenceApprovalRequest:
		refs.ApprovalRequest = r
	case ReferenceCorrelation:
		refs.Correlation = r
	case ReferenceCausation:
		refs.Causation = r
	case ReferenceTransport:
		refs.Transport = r
	case ReferenceDegraded:
		refs.Degraded = r
	case ReferenceProvenance:
		refs.Provenance = r
	}
}

func FuzzEnvelopeValidator(f *testing.F) {
	f.Add("{}")
	f.Fuzz(func(t *testing.T, raw string) {
		var e Envelope
		if json.Unmarshal([]byte(raw), &e) == nil {
			_ = e.Validate()
		}
	})
}

func FuzzComputedEventIDStable(f *testing.F) {
	f.Add("source", "position")
	f.Fuzz(func(t *testing.T, incarnation, position string) {
		if len(incarnation) > MaxReferenceBytes || len(position) > MaxReferenceBytes || incarnation == "" || position == "" {
			return
		}
		e := validEnvelope(t)
		e.SourceIncarnation, e.SourcePosition, e.EventID = incarnation, position, ""
		a, err := NewEnvelope(e)
		if err != nil {
			return
		}
		b, err := NewEnvelope(e)
		if err != nil {
			t.Fatal(err)
		}
		if a.EventID != b.EventID {
			t.Fatal("non-deterministic event id")
		}
	})
}

func referenceFor(kind ReferenceKind, scope Scope) References {
	r := &TypedReference{Kind: kind, ID: string(kind) + "-1", Scope: scope}
	switch kind {
	case ReferenceProviderInvocation:
		return References{ProviderInvocation: r}
	case ReferenceThread:
		return References{Thread: r}
	case ReferenceTurn:
		return References{Turn: r}
	case ReferenceToolCall:
		return References{ToolCall: r}
	case ReferenceApprovalRequest:
		return References{ApprovalRequest: r}
	case ReferenceCorrelation:
		return References{Correlation: r}
	case ReferenceTransport:
		return References{Transport: r}
	case ReferenceDegraded:
		return References{Degraded: r}
	case ReferenceProvenance:
		return References{Provenance: r}
	}
	return References{}
}
