package term

import (
	"bytes"
	"io"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// multiLaunchLauncher supports multiple launches (unlike fakeClaudeLauncher).
type multiLaunchLauncher struct {
	procs []*fakeClaudeProcess
}

func (l *multiLaunchLauncher) Launch(_ string, _ []string) (ManagedProcess, error) {
	pr, pw := io.Pipe()
	p := &fakeClaudeProcess{stdin: new(bytes.Buffer), stdout: pr, pipeW: pw}
	l.procs = append(l.procs, p)
	return p, nil
}

func TestClaudeDelivery_FullChain(t *testing.T) {
	store := NewApprovalStore()
	launcher := &multiLaunchLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)

	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}
	rt := RuntimeRef{Adapter: claudeHeadlessAdapter, Version: "2.1.209", LaunchGen: 1, StreamGen: 0}
	approvalID := "claude-test-del"

	ok := svc.Coordinator().ReserveIdentity(approvalID, "sess", "tu", "Bash",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", id, rt)
	if !ok {
		t.Fatal("ReserveIdentity")
	}

	items := []ApprovalIngestItem{{
		Approval: agent.AgentApproval{
			ID: approvalID, SessionID: id, AgentKind: claudeHeadlessAdapter,
			Kind: "approval", Status: "pending", Source: agent.SourceJSONL, Confidence: 1,
			Options: []agent.InteractionOption{
				{ID: "allow_once", Label: "Allow", Kind: "approve"},
			},
		},
		Provenance: contract.ProvenanceProviderHook, Actionable: true,
		DeliveryMaterial: []ApprovalDeliveryMaterial{
			{OptionID: "allow_once", SchemaVersion: claudeDecisionSchemaV1, ResponseBytes: []byte(`{"permissionDecision":"allow"}`)},
		},
	}}
	store.IngestObserved(ApprovalIngest{SessionID: id, LaunchGen: 1, StreamGen: 0, Provider: claudeHeadlessAdapter, Version: "2.1.209", Items: items})

	reqCtx := RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "b2", Permissions: []string{"x"}}
	claim := store.ClaimForExecution(ClaimRequest{SessionID: id, ApprovalID: approvalID, OptionID: "allow_once", Input: "", Runtime: rt, Requester: reqCtx, IdempotencyKey: "k"})
	if claim.Outcome != ClaimGranted {
		t.Fatalf("ClaimForExecution: %s", claim.Outcome)
	}

	// Short timeout — the fake process never fires the hook, so we test the
	// full chain up to spawn. For actual hook testing, see the composition test.
	d := &ClaudeManagedApprovalDelivery{svc: svc, timeout: 100 * time.Millisecond}
	req := ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload}
	receipt := d.Deliver(req)

	// With no hook firing, we expect timeout (DeliveryConflict). The key proof
	// is that the full A1 chain works: ingest → claim → delivery → cleanup.
	if receipt.Outcome != DeliveryConflict {
		t.Fatalf("expected DeliveryConflict (timeout, no hook), got %s", receipt.Outcome)
	}

	// Cleanup verified: no identity or pending entries remain.
	if svc.Coordinator().IdentityCount() != 0 || svc.Coordinator().PendingCount() != 0 {
		t.Fatal("cleanup should remove identity and entries after failure")
	}

	svc.Stop(id, 1)
}

func TestClaudeDelivery_NilDeps(t *testing.T) {
	d := &ClaudeManagedApprovalDelivery{timeout: time.Second}
	r := d.Deliver(ApprovalDeliveryRequest{})
	if r.Outcome != DeliveryUnavailable {
		t.Fatalf("expected DeliveryUnavailable, got %s", r.Outcome)
	}
}
