package coordination

import (
	"devremote/companion-daemon/internal/workspace"
	"errors"
	"testing"
	"time"
)

func envelope(now time.Time) Envelope {
	return Envelope{Version: 1, ID: "m1", TaskID: "t1", Type: Instruction, Source: Endpoint{"codex", "r1", "s1", 1}, Target: Endpoint{"claude", "r2", "s2", 2}, RepositoryID: "repo", WorkspaceMode: workspace.ModeSharedSequential, SnapshotID: "snap", BaseSHA: "base", CurrentSHA: "head", TreeHash: "tree", DiffDigest: "diff", HandoffReference: "h", EvidenceReference: "e", ContentDigest: "digest", RedactedSummary: "safe", RedactionPolicyVersion: "r1", RequiredTargetCapability: "receive", CreatedAt: now, ExpiresAt: now.Add(time.Minute), State: Queued}
}
func TestBrokerClosedTransitionsAndNoReplay(t *testing.T) {
	now := time.Unix(1, 0)
	b := NewBroker(func() time.Time { return now })
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
