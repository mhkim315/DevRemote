package term

import (
	"context"
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// PA3 Step 2 R5: focused Ingest return-value tests.
// provenActionMapping returns actionable=false for all providers (B5),
// so notification is dormant. These tests verify the Ingest return value
// — the exact IDs admitted by the locked transaction — which is the
// authoritative input to any future notification hook.

// TestIngestReturn_NewAdmittedReturnsID verifies Ingest returns the ID
// of a newly admitted actionable approval.
func TestIngestReturn_NewAdmittedReturnsID(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	in := ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 7, StreamGen: 3,
		Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:     agent.AgentApproval{ID: "AP-NEW", SessionID: "codex:s1", Kind: "approval"},
			Provenance:   contract.ProvenanceNativeLog,
			Actionable:   true,
			RequiredPerm: "terminal:input",
		}},
	}
	admitted := store.Ingest(in)
	if len(admitted) != 1 || admitted[0] != "AP-NEW" {
		t.Fatalf("new: admitted=%v, want [AP-NEW]", admitted)
	}
}

// TestIngestReturn_IdenticalReofferReturnsEmpty verifies an idempotent
// re-offer returns empty from Ingest.
func TestIngestReturn_IdenticalReofferReturnsEmpty(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	in := ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 7, StreamGen: 3,
		Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:     agent.AgentApproval{ID: "AP-DUP", SessionID: "codex:s1", Kind: "approval"},
			Provenance:   contract.ProvenanceNativeLog,
			Actionable:   true,
			RequiredPerm: "terminal:input",
		}},
	}
	// First offer.
	if len(store.Ingest(in)) != 1 {
		t.Fatal("first offer should admit")
	}
	// Identical re-offer.
	admitted := store.Ingest(in)
	if len(admitted) != 0 {
		t.Fatalf("re-offer: admitted=%v, want empty", admitted)
	}
}

// TestIngestReturn_NonActionableAdmittedButNotNotifiable verifies that
// non-actionable items are admitted to the store (so they are observable)
// but would not trigger notification (Actionable=false).
func TestIngestReturn_NonActionableAdmittedButNotNotifiable(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	in := ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 7, StreamGen: 3,
		Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:     agent.AgentApproval{ID: "AP-NA", SessionID: "codex:s1", Kind: "approval"},
			Provenance:   contract.ProvenanceNativeLog,
			Actionable:   false,
			RequiredPerm: "terminal:input",
		}},
	}
	admitted := store.Ingest(in)
	if len(admitted) != 1 || admitted[0] != "AP-NA" {
		t.Fatalf("non-actionable: admitted=%v, want [AP-NA]", admitted)
	}
	// Store has the record but Actionable=false — notification loop skips it.
	snap, _ := store.LookupRecord("codex:s1", "AP-NA")
	if snap.Actionable {
		t.Error("non-actionable stored as actionable")
	}
}

// TestIngestReturn_MixedExistingNew_ReturnsNewOnly verifies that a batch
// with existing A + new B returns only B from Ingest.
func TestIngestReturn_MixedExistingNew_ReturnsNewOnly(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	makeItem := func(id string) ApprovalIngestItem {
		return ApprovalIngestItem{
			Approval:     agent.AgentApproval{ID: id, SessionID: "codex:s1", Kind: "approval"},
			Provenance:   contract.ProvenanceNativeLog,
			Actionable:   true,
			RequiredPerm: "terminal:input",
		}
	}
	// First: admit A.
	if len(store.Ingest(ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 7, StreamGen: 3,
		Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{makeItem("AP-A")},
	})) != 1 {
		t.Fatal("A should admit")
	}

	// Second: batch with existing A + new B.
	admitted := store.Ingest(ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 7, StreamGen: 3,
		Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{makeItem("AP-A"), makeItem("AP-B")},
	})
	if len(admitted) != 1 || admitted[0] != "AP-B" {
		t.Fatalf("mixed: admitted=%v, want [AP-B]", admitted)
	}
}

var _ = context.Background
