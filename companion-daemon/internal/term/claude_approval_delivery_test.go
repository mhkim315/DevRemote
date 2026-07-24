package term

import (
	"bytes"
	"fmt"
	"io"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

func TestClaudeDelivery_FullChain(t *testing.T) {
	store := NewApprovalStore()
	launcher := &multiLaunchLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)
	t.Cleanup(func() {
		for _, p := range launcher.procs {
			p.Kill()
		}
	})

	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}
	rt := RuntimeRef{Adapter: claudeHeadlessAdapter, Version: "2.1.209", LaunchGen: 1, StreamGen: 0}
	approvalID := "claude-test-del"

	// C3D-A fixture migration: the Store admission boundary now re-verifies
	// the exact certified catalog tuple for every actionable claude_headless
	// item, so the controlled setup uses the catalog probe input. The
	// assertions below are unchanged.
	probeDigest := CanonicalDigest([]byte(`{"command":"echo pokitclaudeapprovalprobe"}`))
	if !svc.Coordinator().ReserveIdentity(approvalID, "sess", "tu", "Bash",
		probeDigest, "claude.bash.approval_probe.v1", id, rt) {
		t.Fatal("ReserveIdentity")
	}

	items := []ApprovalIngestItem{{
		Approval: agent.AgentApproval{
			ID: approvalID, SessionID: id, AgentKind: claudeHeadlessAdapter,
			Kind: "approval", Status: "pending", Source: agent.SourceJSONL, Confidence: 1,
			Options: claudeCertifiedOptions(),
		},
		Provenance: contract.ProvenanceProviderHook, Actionable: true,
		RequiredPerm:     claudeRequiredPerm,
		CatalogActionID:  "claude.bash.approval_probe.v1",
		DeliveryMaterial: claudeCertifiedDeliveryMaterial(),
	}}
	store.IngestObserved(ApprovalIngest{SessionID: id, LaunchGen: 1, StreamGen: 0, Provider: claudeHeadlessAdapter, Version: "2.1.209", Items: items})

	reqCtx := RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "b2", Permissions: []string{claudeRequiredPerm}}
	claim := store.ClaimForExecution(ClaimRequest{SessionID: id, ApprovalID: approvalID, OptionID: "allow_once", Input: "", Runtime: rt, Requester: reqCtx, IdempotencyKey: "k"})
	if claim.Outcome != ClaimGranted {
		t.Fatalf("ClaimForExecution: %s", claim.Outcome)
	}

	d := &ClaudeManagedApprovalDelivery{svc: svc, timeout: 100 * time.Millisecond}
	req := ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload}
	receipt := d.Deliver(req)
	if receipt.Outcome != DeliveryConflict {
		t.Fatalf("expected DeliveryConflict (timeout, no hook), got %s", receipt.Outcome)
	}
	if svc.Coordinator().IdentityCount() != 0 || svc.Coordinator().PendingCount() != 0 {
		t.Fatal("cleanup should remove identity and entries")
	}
	svc.Stop(id, 1, "", 0)
}

func TestClaudeDelivery_NilDeps(t *testing.T) {
	d := &ClaudeManagedApprovalDelivery{timeout: time.Second}
	if r := d.Deliver(ApprovalDeliveryRequest{}); r.Outcome != DeliveryUnavailable {
		t.Fatalf("got %s", r.Outcome)
	}
}

type multiLaunchLauncher struct {
	procs []*fakeClaudeProcess
	args  [][]string
}

func (l *multiLaunchLauncher) Launch(_ string, argv []string) (ManagedProcess, error) {
	pr, pw := io.Pipe()
	// Unique per-incarnation opaque identity, mirroring the production
	// launcher's per-spawn OpaqueID (C3D §12 tests rely on it).
	p := &fakeClaudeProcess{stdin: new(bytes.Buffer), stdout: pr, pipeW: pw,
		opaque: fmt.Sprintf("multi-proc-%d", len(l.procs)+1)}
	l.procs = append(l.procs, p)
	l.args = append(l.args, append([]string(nil), argv...))
	return p, nil
}
