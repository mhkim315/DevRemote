package contract

import (
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
)

func validEvent() AgentEvent {
	return AgentEvent{
		ID: "e1", SessionID: "s", AgentKind: "fixture",
		Type: agent.EventThinking, Timestamp: time.Unix(0, 0),
		Confidence: 0.9, Source: agent.SourceJSONL,
	}
}

func TestProvenancePrecedence(t *testing.T) {
	if !ProvenanceRuntime.Stronger(ProvenanceHeuristic) {
		t.Error("runtime must be stronger than heuristic")
	}
	if ProvenancePromptHint.Stronger(ProvenanceNativeLog) {
		t.Error("prompt_hint must not beat native_log")
	}
	if ProvenanceRank(ProvenanceUnknown) != 0 {
		t.Error("unknown provenance must rank lowest")
	}
	// Ordering runtime > protocol > hook > native > pty > heuristic > prompt > unknown.
	order := []Provenance{
		ProvenanceRuntime, ProvenanceProviderProtocol, ProvenanceProviderHook,
		ProvenanceNativeLog, ProvenancePTYStructural, ProvenanceHeuristic,
		ProvenancePromptHint, ProvenanceUnknown,
	}
	for i := 1; i < len(order); i++ {
		if ProvenanceRank(order[i-1]) <= ProvenanceRank(order[i]) {
			t.Errorf("provenance order broken at %d: %s !> %s", i, order[i-1], order[i])
		}
	}
	// Advisory tiers cannot establish authority.
	for _, p := range []Provenance{ProvenanceHeuristic, ProvenancePromptHint, ProvenanceUnknown} {
		if !p.Advisory() {
			t.Errorf("%s must be advisory", p)
		}
	}
	if ProvenanceRuntime.Advisory() {
		t.Error("runtime must not be advisory")
	}
}

func TestConfidenceLevelFor(t *testing.T) {
	cases := []struct {
		c    float64
		want ConfidenceLevel
	}{
		{1.0, ConfidenceAuthoritative}, {0.99, ConfidenceAuthoritative},
		{0.8, ConfidenceHigh}, {0.75, ConfidenceHigh},
		{0.6, ConfidenceMedium}, {0.5, ConfidenceMedium},
		{0.4, ConfidenceLow}, {0.0, ConfidenceLow}, {-1, ConfidenceLow},
	}
	for _, c := range cases {
		if got := ConfidenceLevelFor(c.c); got != c.want {
			t.Errorf("ConfidenceLevelFor(%.2f)=%s, want %s", c.c, got, c.want)
		}
	}
}

func TestValidateEvent(t *testing.T) {
	if err := ValidateEvent(validEvent()); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	bad := map[string]AgentEvent{
		"missing id":       func() AgentEvent { e := validEvent(); e.ID = ""; return e }(),
		"missing session":  func() AgentEvent { e := validEvent(); e.SessionID = ""; return e }(),
		"bad type":         func() AgentEvent { e := validEvent(); e.Type = "made_up"; return e }(),
		"bad source":       func() AgentEvent { e := validEvent(); e.Source = "made_up"; return e }(),
		"confidence high":  func() AgentEvent { e := validEvent(); e.Confidence = 1.5; return e }(),
		"confidence low":   func() AgentEvent { e := validEvent(); e.Confidence = -0.1; return e }(),
		"low-conf not unk": func() AgentEvent { e := validEvent(); e.Confidence = 0.2; return e }(),
	}
	for name, e := range bad {
		if err := ValidateEvent(e); err == nil {
			t.Errorf("%s: expected ValidateEvent to reject", name)
		}
	}
}

func TestSafeEventCoercion(t *testing.T) {
	e := SafeEvent(AgentEvent{ID: "x", SessionID: "s", Type: "bogus", Source: "bogus", Confidence: 2})
	if e.Type != agent.EventUnknown {
		t.Errorf("bogus type not coerced to unknown: %s", e.Type)
	}
	if !IsKnownEventSource(e.Source) {
		t.Errorf("bogus source not coerced: %s", e.Source)
	}
	if e.Confidence > 1 {
		t.Errorf("confidence not clamped: %v", e.Confidence)
	}
	// Low confidence forces unknown even for a known type.
	low := SafeEvent(AgentEvent{ID: "y", SessionID: "s", Type: agent.EventThinking, Source: agent.SourceJSONL, Confidence: 0.1})
	if low.Type != agent.EventUnknown {
		t.Errorf("low-confidence typed event not degraded to unknown: %s", low.Type)
	}
}

func TestDedupeAndBound(t *testing.T) {
	events := []AgentEvent{
		{ID: "a"}, {ID: "b"}, {ID: "a"}, {ID: ""}, {ID: "c"},
	}
	d := DedupeEvents(events)
	if len(d) != 3 {
		t.Errorf("dedupe len=%d, want 3 (a,b,c)", len(d))
	}
	bounded, trunc := BoundEvents(d, 2)
	if len(bounded) != 2 || !trunc {
		t.Errorf("bound len=%d trunc=%v, want 2,true", len(bounded), trunc)
	}
	if _, trunc := BoundEvents(d, 10); trunc {
		t.Error("no truncation expected when under cap")
	}
}

func TestResolveStatusPrecedence(t *testing.T) {
	// Runtime beats a higher-confidence heuristic.
	r := ResolveStatus([]StatusEvidence{
		{Status: agent.StatusIdle, Provenance: ProvenanceHeuristic, Confidence: 0.95},
		{Status: agent.StatusWorking, Provenance: ProvenanceRuntime, Confidence: 0.55},
	})
	if r.Status != agent.StatusWorking {
		t.Errorf("precedence: got %s, want working", r.Status)
	}
	// Same provenance → higher confidence wins.
	r2 := ResolveStatus([]StatusEvidence{
		{Status: agent.StatusThinking, Provenance: ProvenanceNativeLog, Confidence: 0.6},
		{Status: agent.StatusCompleted, Provenance: ProvenanceNativeLog, Confidence: 0.9},
	})
	if r2.Status != agent.StatusCompleted {
		t.Errorf("same-provenance tiebreak: got %s, want completed", r2.Status)
	}
	// No evidence → unknown.
	if ResolveStatus(nil).Status != agent.StatusUnknown {
		t.Error("empty evidence must resolve to unknown")
	}
	// Out-of-vocabulary status is ignored.
	r3 := ResolveStatus([]StatusEvidence{{Status: "made_up", Provenance: ProvenanceRuntime, Confidence: 1}})
	if r3.Status != agent.StatusUnknown {
		t.Errorf("invalid status must be ignored, got %s", r3.Status)
	}
}

func TestSafeApprovalGate(t *testing.T) {
	yes := AgentEvent{Type: agent.EventApprovalRequested, Confidence: 0.6}
	if !SafeApprovalGate(yes) {
		t.Error("valid approval_requested should gate open")
	}
	for _, e := range []AgentEvent{
		{Type: agent.EventApprovalRequested, Confidence: 0.3}, // too low
		{Type: agent.EventAssistantMessage, Confidence: 0.99}, // near-miss content
		{Type: agent.EventUnknown, Confidence: 0.99},
	} {
		if SafeApprovalGate(e) {
			t.Errorf("ambiguous/low event must not gate approval: %+v", e)
		}
	}
}

func TestDiagnosticSanitization(t *testing.T) {
	// Built from fragments so no literal credential appears in source; the runtime
	// strings still exercise the real redactor.
	cases := []string{
		"token " + "sk" + "-abcdef0123456789",
		"leaked " + "ghp" + "_ABCDEFGHIJKLMNOP",
		"Authorization: " + "Bearer " + "eyJhbGciOiJIUzI1NiIsInR5cCI6",
		"path /Users/victim/.ssh/id_rsa",
		"home /home/victim/secret",
	}
	for _, c := range cases {
		if !ContainsSensitive(c) {
			t.Errorf("expected sensitive: %q", c)
		}
		if ContainsSensitive(SanitizeDiagnostic(c)) {
			t.Errorf("sanitized still sensitive: %q -> %q", c, SanitizeDiagnostic(c))
		}
	}
	clean := "read truncated at bound"
	if ContainsSensitive(clean) {
		t.Errorf("clean string flagged: %q", clean)
	}
	// Bounded length.
	long := make([]byte, MaxDiagnosticBytes*2)
	for i := range long {
		long[i] = 'a'
	}
	if len(SanitizeDiagnostic(string(long))) > MaxDiagnosticBytes {
		t.Error("diagnostic not length-bounded")
	}
}

func TestDegradeIsBoundedAndClean(t *testing.T) {
	many := make([]string, MaxDiagnostics*2)
	for i := range many {
		many[i] = "diag"
	}
	d := Degrade("reason "+"sk"+"-secret1234567890", many...)
	if !d.Degraded {
		t.Error("Degrade must set Degraded=true")
	}
	if ContainsSensitive(d.Reason) {
		t.Errorf("degrade reason not sanitized: %q", d.Reason)
	}
	if len(d.Diagnostics) > MaxDiagnostics {
		t.Errorf("diagnostics not bounded: %d", len(d.Diagnostics))
	}
}

func TestReadLimitBounds(t *testing.T) {
	if EffectiveReadLimit(0) != MaxEventsPerRead {
		t.Error("zero caller max should use cap")
	}
	if EffectiveReadLimit(10) != 10 {
		t.Error("small caller max should be honored")
	}
	if EffectiveReadLimit(MaxEventsPerRead+100) != MaxEventsPerRead {
		t.Error("oversized caller max must be capped")
	}
}
