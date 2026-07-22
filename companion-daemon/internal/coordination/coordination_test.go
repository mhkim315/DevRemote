package coordination

import (
	"devremote/companion-daemon/internal/workspace"
	"errors"
	"strings"
	"testing"
	"time"
)

func envelope(now time.Time) Envelope {
	return Envelope{Version: 1, ID: "m1", TaskID: "t1", Type: Instruction, Source: Endpoint{"codex", "r1", "s1", 1}, Target: Endpoint{"claude", "r2", "s2", 2}, RepositoryID: "repo", WorkspaceMode: workspace.ModeSharedSequential, SnapshotID: "snap", BaseSHA: "base", CurrentSHA: "head", TreeHash: "tree", DiffDigest: "diff", HandoffReference: "h", EvidenceReference: "e", ContentDigest: "digest", RedactedSummary: "safe", RedactionPolicyVersion: "r1", RequiredTargetCapability: "receive", CreatedAt: now, ExpiresAt: now.Add(time.Minute), State: Queued}
}
func TestBrokerClosedTransitionsAndNoReplay(t *testing.T) {
	now := time.Unix(1, 0)
	b := NewBroker(func() time.Time { return now })
	b.SetCapabilityChecker(&testAuth{allow: map[string]bool{"s2:receive": true}})
	e := envelope(now)
	if err := b.Enqueue(e); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Transition(e.ID, DeliveryAccepted); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Transition(e.ID, Queued); !errors.Is(err, ErrTransition) {
		t.Fatalf("replay=%v", err)
	}
	if _, err := b.Transition(e.ID, RuntimeAcknowledged); err != nil {
		t.Fatal(err)
	}
}
func TestBrokerExpiryAndSecretRejection(t *testing.T) {
	now := time.Unix(1, 0)
	b := NewBroker(func() time.Time { return now })
	b.SetCapabilityChecker(&testAuth{allow: map[string]bool{"s2:receive": true}})
	e := envelope(now)
	if err := b.Enqueue(e); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := b.Transition(e.ID, DeliveryAccepted); !errors.Is(err, ErrExpired) {
		t.Fatal(err)
	}
	e = envelope(time.Unix(1, 0))
	e.ID = "m2"
	e.RedactedSummary = "Bearer token"
	if err := b.Enqueue(e); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

// ── STEP6: behavioral bounds tests ──

func validHandoff(t *testing.T) Handoff {
	t.Helper()
	return Handoff{
		Objective: "obj", AcceptanceCriteria: "acc", Constraints: "con",
		Workspace:              workspace.Identity{RepositoryID: "repo", Mode: workspace.ModeSharedSequential, BaseSHA: "base", CurrentSHA: "cur", TreeHash: "tree", SnapshotID: "snap", IsolationProfile: workspace.IsolationRepoOnly},
		ChangedFiles:           []string{"f1"},
		TestCommands:           []string{"t1"},
		TestResults:            []string{"ok"},
		UnresolvedFindings:     []string{},
		ApprovalState:          "pending",
		EvidenceProvenance:     "prov",
		ArtifactHash:           "hash",
		ArtifactType:           "binary",
		RedactionPolicyVersion: "r1",
		ArtifactBytes:          0,
		ExpiresAt:              time.Unix(100, 0),
		SourceProvider:         "codex",
		SourceRuntimeID:        "r1",
		SourceSessionID:        "s1",
		SourceGeneration:       1,
		DiffDigest:             "dd",
	}
}

func TestHandoff_OversizedRefsRejected(t *testing.T) {
	big := strings.Repeat("x", MaxReferenceBytes+1)
	for _, tc := range []struct {
		name string
		mut  func(e *Envelope)
	}{
		{"HandoffReference", func(e *Envelope) { e.HandoffReference = big }},
		{"EvidenceReference", func(e *Envelope) { e.EvidenceReference = big }},
		{"ReplyToID", func(e *Envelope) { e.ReplyToID = big }},
		{"CausationID", func(e *Envelope) { e.CausationID = big }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := envelope(time.Unix(1, 0))
			tc.mut(&e)
			if err := e.Validate(); err == nil {
				t.Errorf("%s > MaxReferenceBytes accepted", tc.name)
			}
		})
	}
}

func TestHandoff_OversizedListCountsRejected(t *testing.T) {
	big := make([]string, MaxHandoffItems+1)
	for i := range big {
		big[i] = "x"
	}
	for _, tc := range []struct {
		name string
		mut  func(h *Handoff)
	}{
		{"ChangedFiles", func(h *Handoff) { h.ChangedFiles = big }},
		{"TestCommands", func(h *Handoff) { h.TestCommands = big }},
		{"TestResults", func(h *Handoff) { h.TestResults = big }},
		{"UnresolvedFindings", func(h *Handoff) { h.UnresolvedFindings = big }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := validHandoff(t)
			tc.mut(&h)
			if err := h.Validate(); err == nil {
				t.Errorf("%s > MaxHandoffItems accepted", tc.name)
			}
		})
	}
}

func TestHandoff_OversizedPerItemSizeRejected(t *testing.T) {
	big := strings.Repeat("x", MaxBytes+1)
	for _, tc := range []struct {
		name string
		mut  func(h *Handoff)
	}{
		{"ChangedFiles item", func(h *Handoff) { h.ChangedFiles = []string{big} }},
		{"TestCommands item", func(h *Handoff) { h.TestCommands = []string{big} }},
		{"TestResults item", func(h *Handoff) { h.TestResults = []string{big} }},
		{"UnresolvedFindings item", func(h *Handoff) { h.UnresolvedFindings = []string{big} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := validHandoff(t)
			tc.mut(&h)
			if err := h.Validate(); err == nil {
				t.Errorf("%s > MaxBytes accepted", tc.name)
			}
		})
	}
}

func TestEnvelope_ClosedVariantsAccepted(t *testing.T) {
	for _, typ := range []MessageType{Question, Finding, RevisionRequest} {
		e := envelope(time.Unix(1, 0))
		e.ID = "m-" + string(typ)
		e.Type = typ
		if err := e.Validate(); err != nil {
			t.Errorf("%s rejected: %v", typ, err)
		}
	}
}

func TestEnvelope_UnknownTypeRejected(t *testing.T) {
	e := envelope(time.Unix(1, 0))
	e.ID = "m-unknown"
	e.Type = "unknown_type"
	if err := e.Validate(); err == nil {
		t.Error("unknown type accepted")
	}
}

func TestHandoff_SourceBindingsRequired(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(h *Handoff)
	}{
		{"SourceProvider", func(h *Handoff) { h.SourceProvider = "" }},
		{"SourceRuntimeID", func(h *Handoff) { h.SourceRuntimeID = "" }},
		{"SourceSessionID", func(h *Handoff) { h.SourceSessionID = "" }},
		{"SourceGeneration", func(h *Handoff) { h.SourceGeneration = -1 }},
		{"DiffDigest", func(h *Handoff) { h.DiffDigest = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := validHandoff(t)
			tc.mut(&h)
			if err := h.Validate(); err == nil {
				t.Errorf("%s missing accepted", tc.name)
			}
		})
	}
}

// ── STEP6: authorization enforcement ──

type testAuth struct{ allow map[string]bool }

func (a *testAuth) HasCapability(src Endpoint, cap string) bool {
	return a.allow[src.SessionID+":"+cap]
}

func TestBroker_AuthorizationEnforced(t *testing.T) {
	now := time.Unix(1, 0)
	b := NewBroker(func() time.Time { return now })
	// Target endpoint: {"claude", "r2", "s2", 2}. Check target has capability.
	auth := &testAuth{allow: map[string]bool{
		"s2:receive": true,
		"s3:receive": false,
	}}
	b.SetCapabilityChecker(auth)

	// Source has capability → accepted.
	e1 := envelope(now)
	e1.ID = "auth-ok"
	if err := b.Enqueue(e1); err != nil {
		t.Fatalf("authorized source rejected: %v", err)
	}

	// Target lacks capability → rejected.
	e2 := envelope(now)
	e2.ID = "auth-fail"
	e2.Target = Endpoint{"claude", "r3", "s3", 3}
	if err := b.Enqueue(e2); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unauthorized target accepted: %v", err)
	}

	// Target not in allow map → rejected.
	e3 := envelope(now)
	e3.ID = "auth-missing"
	e3.Target = Endpoint{"claude", "r4", "s4", 4}
	if err := b.Enqueue(e3); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown target accepted: %v", err)
	}

	// Broker without auth hook → fail-closed (nil CapabilityChecker rejects all).
	b2 := NewBroker(func() time.Time { return now })
	e4 := envelope(now)
	e4.ID = "no-auth"
	if err := b2.Enqueue(e4); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no-auth broker accepted (want fail-closed): %v", err)
	}
}

// ── Envelope bounds at N-1/N/N+1 for reference fields ──

func TestEnvelope_RefBoundsAtNMinusOneNAndNPlusOne(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(e *Envelope, n int)
	}{
		{"HandoffReference", func(e *Envelope, n int) { e.HandoffReference = strings.Repeat("x", n) }},
		{"EvidenceReference", func(e *Envelope, n int) { e.EvidenceReference = strings.Repeat("x", n) }},
	} {
		for _, n := range []int{MaxReferenceBytes - 1, MaxReferenceBytes, MaxReferenceBytes + 1} {
			e := envelope(time.Unix(1, 0))
			tc.mut(&e, n)
			err := e.Validate()
			if (n <= MaxReferenceBytes) != (err == nil) {
				t.Errorf("%s at %d: %v", tc.name, n, err)
			}
		}
	}
}
