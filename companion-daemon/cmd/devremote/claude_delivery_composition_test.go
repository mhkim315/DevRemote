package main

import (
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/term"
)

// compFakeLauncher records all launches. Supports multiple launches.
type compFakeLauncher struct {
	mu      sync.Mutex
	allArgs [][]string
}

func (l *compFakeLauncher) Launch(_ string, argv []string) (term.ManagedProcess, error) {
	l.mu.Lock()
	l.allArgs = append(l.allArgs, append([]string(nil), argv...))
	l.mu.Unlock()
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	return &compFakeProc{
		stdinR: stdinR, stdinW: stdinW, stdoutR: stdoutR, stdoutW: stdoutW,
		killed: make(chan struct{}),
	}, nil
}

type compFakeProc struct {
	stdinR  *io.PipeReader
	stdinW  *io.PipeWriter
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	killed  chan struct{}
	once    sync.Once
}

func (p *compFakeProc) Stdin() io.Writer  { return p.stdinW }
func (p *compFakeProc) Stdout() io.Reader { return p.stdoutR }
func (p *compFakeProc) Term() error       { return p.Kill() }
func (p *compFakeProc) Kill() error {
	p.once.Do(func() {
		close(p.killed)
		p.stdinR.Close()
		p.stdoutW.Close()
	})
	return nil
}
func (p *compFakeProc) Wait() error      { <-p.killed; return nil }
func (p *compFakeProc) PID() int         { return 0 }
func (p *compFakeProc) OpaqueID() string { return "comp-fake" }

type compFakeAttestor struct{}

func (a *compFakeAttestor) Certify(_ string) error { return nil }

func TestClaudeDelivery_CompositionAllow(t *testing.T) {
	launcher := &compFakeLauncher{}
	cfg := term.ClaudeEntryConfig{
		Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209",
		PinnedPath: "/tmp/fake-claude",
		PinnedDigest: "0000000000000000000000000000000000000000000000000000000000000000",
	}
	svc := term.NewManagedClaudeService(cfg, launcher, &compFakeAttestor{})
	store := term.NewApprovalStore()
	svc.SetApprovalStore(store)

	sessionID, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}
	rt := term.RuntimeRef{Adapter: "claude_headless", Version: "2.1.209", LaunchGen: 1, StreamGen: 0}
	approvalID := "claude-comp-allow"

	if !svc.Coordinator().ReserveIdentity(approvalID, "csess", "call_test", "Bash",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", sessionID, rt) {
		t.Fatal("ReserveIdentity failed")
	}

	// Use canonical response bytes so payload matches delivery validation.
	respBytes := term.ClaudeHookResponseBytes("allow")
	denyBytes := term.ClaudeHookResponseBytes("deny")
	items := []term.ApprovalIngestItem{{
		Approval: agent.AgentApproval{
			ID: approvalID, SessionID: sessionID, AgentKind: "claude_headless",
			Kind: "approval", Status: "pending", Source: agent.SourceJSONL, Confidence: 1,
			Options: []agent.InteractionOption{
				{ID: "allow_once", Label: "Allow once", Kind: "approve"},
				{ID: "deny", Label: "Deny", Kind: "reject"},
			},
		},
		Provenance: contract.ProvenanceProviderHook, Actionable: true,
		DeliveryMaterial: []term.ApprovalDeliveryMaterial{
			{OptionID: "allow_once", SchemaVersion: term.ClaudeDecisionSchemaV1(), ResponseBytes: respBytes},
			{OptionID: "deny", SchemaVersion: term.ClaudeDecisionSchemaV1(), ResponseBytes: denyBytes},
		},
	}}
	store.IngestObserved(term.ApprovalIngest{
		SessionID: sessionID, LaunchGen: 1, StreamGen: 0,
		Provider: "claude_headless", Version: "2.1.209", Items: items,
	})

	reqCtx := term.RequesterContext{
		DeviceID: "dev-1", HostID: "host-1", BearerSessionID: "bearer-1",
		BootID: "boot-1", Permissions: []string{"session:approve"},
	}
	claim := store.ClaimForExecution(term.ClaimRequest{
		SessionID: sessionID, ApprovalID: approvalID, OptionID: "allow_once",
		Input: "", Runtime: rt, Requester: reqCtx, IdempotencyKey: "test.comp.allow",
	})
	if claim.Outcome != term.ClaimGranted {
		t.Fatalf("ClaimForExecution: %s", claim.Outcome)
	}

	d := term.NewClaudeManagedApprovalDelivery(svc)
	d.SetPollTimeout(100 * time.Millisecond)
	req := term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload}
	receipt := d.Deliver(req)

	if receipt.Outcome != term.DeliveryConflict {
		t.Fatalf("expected DeliveryConflict (timeout, no hook), got %s", receipt.Outcome)
	}
	if svc.Coordinator().IdentityCount() != 0 || svc.Coordinator().PendingCount() != 0 {
		t.Fatal("cleanup should remove identity and entries")
	}

	if len(launcher.allArgs) < 2 {
		t.Fatal("expected at least 2 launches")
	}
	resumeArgs := launcher.allArgs[1]
	hasResume := false
	for _, a := range resumeArgs {
		if a == "--resume" {
			hasResume = true
			break
		}
	}
	if !hasResume {
		t.Fatal("resume launch missing --resume flag")
	}

	svc.Stop(sessionID, 1)
	ctx := t.Context()
	svc.Shutdown(ctx)
}

func TestClaudeDelivery_CompositionDeny(t *testing.T) {
	launcher := &compFakeLauncher{}
	cfg := term.ClaudeEntryConfig{
		Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209",
		PinnedPath: "/tmp/fake-claude",
		PinnedDigest: "0000000000000000000000000000000000000000000000000000000000000000",
	}
	svc := term.NewManagedClaudeService(cfg, launcher, &compFakeAttestor{})
	store := term.NewApprovalStore()
	svc.SetApprovalStore(store)

	sessionID, _ := svc.CreateDetached("/tmp")
	rt := term.RuntimeRef{Adapter: "claude_headless", Version: "2.1.209", LaunchGen: 1, StreamGen: 0}
	approvalID := "claude-comp-deny"

	svc.Coordinator().ReserveIdentity(approvalID, "csess", "call_deny", "Bash",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", sessionID, rt)

	respBytes := term.ClaudeHookResponseBytes("allow")
	denyBytes := term.ClaudeHookResponseBytes("deny")
	items := []term.ApprovalIngestItem{{
		Approval: agent.AgentApproval{
			ID: approvalID, SessionID: sessionID, AgentKind: "claude_headless",
			Kind: "approval", Status: "pending", Source: agent.SourceJSONL, Confidence: 1,
			Options: []agent.InteractionOption{
				{ID: "allow_once", Label: "Allow once", Kind: "approve"},
				{ID: "deny", Label: "Deny", Kind: "reject"},
			},
		},
		Provenance: contract.ProvenanceProviderHook, Actionable: true,
		DeliveryMaterial: []term.ApprovalDeliveryMaterial{
			{OptionID: "allow_once", SchemaVersion: term.ClaudeDecisionSchemaV1(), ResponseBytes: respBytes},
			{OptionID: "deny", SchemaVersion: term.ClaudeDecisionSchemaV1(), ResponseBytes: denyBytes},
		},
	}}
	store.IngestObserved(term.ApprovalIngest{
		SessionID: sessionID, LaunchGen: 1, StreamGen: 0,
		Provider: "claude_headless", Version: "2.1.209", Items: items,
	})

	reqCtx := term.RequesterContext{
		DeviceID: "dev-1", HostID: "host-1", BearerSessionID: "bearer-1",
		BootID: "boot-1", Permissions: []string{"session:approve"},
	}
	claim := store.ClaimForExecution(term.ClaimRequest{
		SessionID: sessionID, ApprovalID: approvalID, OptionID: "deny",
		Input: "", Runtime: rt, Requester: reqCtx, IdempotencyKey: "test.comp.deny",
	})
	if claim.Outcome != term.ClaimGranted {
		t.Fatalf("ClaimForExecution: %s", claim.Outcome)
	}

	d := term.NewClaudeManagedApprovalDelivery(svc)
	d.SetPollTimeout(100 * time.Millisecond)
	req := term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload}
	receipt := d.Deliver(req)

	if receipt.Outcome != term.DeliveryConflict {
		t.Fatalf("expected DeliveryConflict (timeout, no hook), got %s", receipt.Outcome)
	}
	if svc.Coordinator().IdentityCount() != 0 || svc.Coordinator().PendingCount() != 0 {
		t.Fatal("cleanup should remove identity and entries")
	}

	if len(launcher.allArgs) < 2 {
		t.Fatal("expected at least 2 launches")
	}

	svc.Stop(sessionID, 1)
	ctx := t.Context()
	svc.Shutdown(ctx)
}

func TestClaudeDelivery_CompositionTimeout(t *testing.T) {
	launcher := &compFakeLauncher{}
	cfg := term.ClaudeEntryConfig{
		Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209",
		PinnedPath: "/tmp/fake-claude",
		PinnedDigest: "0000000000000000000000000000000000000000000000000000000000000000",
	}
	svc := term.NewManagedClaudeService(cfg, launcher, &compFakeAttestor{})
	store := term.NewApprovalStore()
	svc.SetApprovalStore(store)

	sessionID, _ := svc.CreateDetached("/tmp")
	rt := term.RuntimeRef{Adapter: "claude_headless", Version: "2.1.209", LaunchGen: 1, StreamGen: 0}
	approvalID := "claude-comp-timeout"

	svc.Coordinator().ReserveIdentity(approvalID, "sess", "tu", "Bash",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", sessionID, rt)

	respBytes := term.ClaudeHookResponseBytes("allow")
	items := []term.ApprovalIngestItem{{
		Approval: agent.AgentApproval{
			ID: approvalID, SessionID: sessionID, AgentKind: "claude_headless",
			Kind: "approval", Status: "pending", Source: agent.SourceJSONL, Confidence: 1,
			Options: []agent.InteractionOption{{ID: "allow_once", Label: "Allow once", Kind: "approve"}},
		},
		Provenance: contract.ProvenanceProviderHook, Actionable: true,
		DeliveryMaterial: []term.ApprovalDeliveryMaterial{
			{OptionID: "allow_once", SchemaVersion: term.ClaudeDecisionSchemaV1(), ResponseBytes: respBytes},
		},
	}}
	store.IngestObserved(term.ApprovalIngest{
		SessionID: sessionID, LaunchGen: 1, StreamGen: 0,
		Provider: "claude_headless", Version: "2.1.209", Items: items,
	})

	reqCtx := term.RequesterContext{
		DeviceID: "dev-1", HostID: "host-1", BearerSessionID: "bearer-1",
		BootID: "boot-1", Permissions: []string{"session:approve"},
	}
	claim := store.ClaimForExecution(term.ClaimRequest{
		SessionID: sessionID, ApprovalID: approvalID, OptionID: "allow_once",
		Input: "", Runtime: rt, Requester: reqCtx, IdempotencyKey: "test.comp.timeout",
	})
	if claim.Outcome != term.ClaimGranted {
		t.Fatalf("ClaimForExecution: %s", claim.Outcome)
	}

	d := term.NewClaudeManagedApprovalDelivery(svc)
	d.SetPollTimeout(10 * time.Millisecond)
	req := term.ApprovalDeliveryRequest{ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload}
	receipt := d.Deliver(req)

	if receipt.Outcome == term.DeliveryAccepted {
		t.Fatal("expected non-Accepted for timeout")
	}
	if svc.Coordinator().IdentityCount() != 0 || svc.Coordinator().PendingCount() != 0 {
		t.Fatal("cleanup should remove identity and entries")
	}

	svc.Stop(sessionID, 1)
	ctx := t.Context()
	svc.Shutdown(ctx)
}

var _ = fmt.Println
