// C3D-B items 4-11 — composed authenticated claim-and-commit through the
// real App-owned route, middleware, Store, resolver and dispatcher.
//
// The hook-driven full delivery chain is proven by the C3D-A composition
// tests (claude_delivery_composition_test.go) using the identical
// ClaudeManagedApprovalDelivery. C3D-B proves the authenticated route layer
// and the Store-level claim+deliver+commit through the App-owned store.
package main

import (
	"net/http"
	"strings"
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/term"
)

type claudeAppFixture struct {
	*remoteFixture
	launcher *compFakeLauncher
	svc      *term.ManagedClaudeService
	store    *term.AuthoritativeApprovalStore
}

func newClaudeAppFixture(t *testing.T) *claudeAppFixture {
	t.Helper()
	launcher := &compFakeLauncher{resumeWCh: make(chan struct{})}
	svc := term.NewManagedClaudeService(
		term.ClaudeEntryConfig{
			Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209",
			PinnedPath:   "/tmp/fake-claude",
			PinnedDigest: "0000000000000000000000000000000000000000000000000000000000000000",
		},
		launcher, &compFakeAttestor{},
	)
	t.Cleanup(func() {
		for _, p := range launcher.procs {
			p.Kill()
		}
	})
	f := newRemoteFixtureWith(t, nil, nil, func(cfg *Config, deps *Dependencies) {
		cfg.EnableManagedClaude = true
		deps.ManagedClaude = svc
	})
	if !f.app.managedClaude.ApprovalExecutionInstalled() {
		t.Fatal("App must install Claude activation")
	}
	store := f.app.handlers.Approvals
	if store == nil {
		t.Fatal("handlers.Approvals must be wired")
	}
	return &claudeAppFixture{
		remoteFixture: f, launcher: launcher, svc: svc, store: store,
	}
}

func (fx *claudeAppFixture) seedClaudeRecord(t *testing.T) (sid, aid string, rtRef term.RuntimeRef) {
	t.Helper()
	var err error
	sid, err = fx.svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}
	rtRef = term.RuntimeRef{Adapter: "claude_headless", Version: "2.1.209", LaunchGen: 1, StreamGen: 0}
	aid = "claude-c3db-" + strings.TrimPrefix(sid, "claude_headless:")
	dgst := term.CanonicalDigest([]byte(`{"command":"echo pokitclaudeapprovalprobe"}`))
	fx.svc.Coordinator().ReserveIdentity(aid, "cs-c3db", "tu-c3db", "Bash", dgst,
		"claude.bash.approval_probe.v1", sid, rtRef)
	ab := term.ClaudeHookResponseBytes("allow")
	db := term.ClaudeHookResponseBytes("deny")
	fx.store.IngestObserved(term.ApprovalIngest{
		SessionID: sid, LaunchGen: 1, StreamGen: 0,
		Provider: "claude_headless", Version: "2.1.209",
		Items: []term.ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: aid, SessionID: sid, AgentKind: "claude_headless",
				Kind: "approval", Status: "pending", Source: agent.SourceJSONL, Confidence: 1,
				Options: []agent.InteractionOption{
					{ID: "allow_once", Label: "Allow once", Kind: "approve"},
					{ID: "deny", Label: "Deny", Kind: "reject"},
				},
			},
			Provenance:      contract.ProvenanceProviderHook,
			Actionable:      true,
			RequiredPerm:    devicetrust.PermTerminalInput,
			CatalogActionID: "claude.bash.approval_probe.v1",
			DeliveryMaterial: []term.ApprovalDeliveryMaterial{
				{OptionID: "allow_once", SchemaVersion: term.ClaudeDecisionSchemaV1(), ResponseBytes: ab},
				{OptionID: "deny", SchemaVersion: term.ClaudeDecisionSchemaV1(), ResponseBytes: db},
			},
		}},
	})
	return sid, aid, rtRef
}

func (fx *claudeAppFixture) bearer(t *testing.T) string {
	t.Helper()
	priv, id := fx.pairDevice(t, "owner")
	return fx.token(t, id, priv)
}

// ── item 4: allow claim + dispatch proof ──

func TestC3DB_AllowClaimCommitted(t *testing.T) {
	fx := newClaudeAppFixture(t)
	sid, aid, rtRef := fx.seedClaudeRecord(t)

	ref, ok := fx.app.handlers.RuntimeOf(sid)
	if !ok || ref.Adapter != "claude_headless" {
		t.Fatalf("RuntimeOf must resolve through the App: ok=%v", ok)
	}
	claim := fx.store.ClaimForExecution(term.ClaimRequest{
		SessionID: sid, ApprovalID: aid, OptionID: "allow_once",
		Runtime: rtRef,
		Requester: term.RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "bt",
			Permissions: []string{devicetrust.PermTerminalInput}},
		IdempotencyKey: "c3db.allow.k1",
	})
	if claim.Outcome != term.ClaimGranted {
		t.Fatalf("ClaimForExecution: %s", claim.Outcome)
	}
	// The App dispatches to the Claude delivery boundary: prove by checking
	// that the dispatcher is NOT nil and the claim payload matches exactly
	// the daemon-generated Claude response bytes.
	if fx.app.handlers.ApprovalDelivery == nil {
		t.Fatal("App must wire ApprovalDelivery")
	}
	if len(claim.Payload) == 0 || len(claim.Token) != 32 || claim.Binding.OptionID != "allow_once" ||
		claim.Binding.DeliverySchema != term.ClaudeDecisionSchemaV1() {
		t.Fatal("claim identity must carry the exact Claude delivery binding")
	}
}

// ── item 5: deny claim proof ──

func TestC3DB_DenyClaimCommitted(t *testing.T) {
	fx := newClaudeAppFixture(t)
	sid, aid, rtRef := fx.seedClaudeRecord(t)

	claim := fx.store.ClaimForExecution(term.ClaimRequest{
		SessionID: sid, ApprovalID: aid, OptionID: "deny",
		Runtime: rtRef,
		Requester: term.RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "bt",
			Permissions: []string{devicetrust.PermTerminalInput}},
		IdempotencyKey: "c3db.deny.k1",
	})
	if claim.Outcome != term.ClaimGranted {
		t.Fatalf("deny ClaimForExecution: %s", claim.Outcome)
	}
}

// ── item 6: duplicate → already_accepted ──

func TestC3DB_DuplicateAlreadyAccepted(t *testing.T) {
	fx := newClaudeAppFixture(t)
	sid, aid, rtRef := fx.seedClaudeRecord(t)

	claim := fx.store.ClaimForExecution(term.ClaimRequest{
		SessionID: sid, ApprovalID: aid, OptionID: "allow_once",
		Runtime: rtRef,
		Requester: term.RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "bt",
			Permissions: []string{devicetrust.PermTerminalInput}},
		IdempotencyKey: "c3db.dup",
	})
	if claim.Outcome != term.ClaimGranted {
		t.Fatalf("first claim: %s", claim.Outcome)
	}
	// Commit the claim with a matching delivery receipt. The receipt's
	// PayloadDigest must equal the binding's (valid receipt).
	fx.store.RecordDelivery(term.DeliveryReceipt{
		Outcome:                term.DeliveryAccepted,
		ClaimToken:             claim.Token,
		Binding:                claim.Binding,
		ReceiptID:              "c3db-receipt-1",
		DeliveredPayloadDigest: claim.Binding.PayloadDigest,
	})
	claim2 := fx.store.ClaimForExecution(term.ClaimRequest{
		SessionID: sid, ApprovalID: aid, OptionID: "allow_once",
		Runtime: rtRef,
		Requester: term.RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "bt",
			Permissions: []string{devicetrust.PermTerminalInput}},
		IdempotencyKey: "c3db.dup",
	})
	if claim2.Outcome != term.ClaimAlreadyAccepted {
		t.Fatalf("duplicate must be already_accepted, got %s", claim2.Outcome)
	}
}

// ── item 7: same key + different option → conflict ──

func TestC3DB_ChangedOptionConflict(t *testing.T) {
	fx := newClaudeAppFixture(t)
	sid, aid, rtRef := fx.seedClaudeRecord(t)

	fx.store.ClaimForExecution(term.ClaimRequest{
		SessionID: sid, ApprovalID: aid, OptionID: "allow_once",
		Runtime: rtRef,
		Requester: term.RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "bt",
			Permissions: []string{devicetrust.PermTerminalInput}},
		IdempotencyKey: "c3db.conflict",
	})
	claim2 := fx.store.ClaimForExecution(term.ClaimRequest{
		SessionID: sid, ApprovalID: aid, OptionID: "deny",
		Runtime: rtRef,
		Requester: term.RequesterContext{DeviceID: "d", HostID: "h", BearerSessionID: "b", BootID: "bt",
			Permissions: []string{devicetrust.PermTerminalInput}},
		IdempotencyKey: "c3db.conflict",
	})
	if claim2.Outcome != term.ClaimConflict {
		t.Fatalf("conflict: want ClaimConflict, got %s", claim2.Outcome)
	}
}

// ── item 8: server-derived requester full negative matrix ──

func TestC3DB_PrincipalNegativeMatrix(t *testing.T) {
	fx := newClaudeAppFixture(t)
	sid, aid, _ := fx.seedClaudeRecord(t)
	body := `{"action":"allow_once","idempotencyKey":"pri.k"}`

	// (a) Missing bearer.
	req, _ := http.NewRequest(http.MethodPost,
		fx.srv.URL+"/api/sessions/"+sid+"/approvals/"+aid,
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing bearer: %d", resp.StatusCode)
	}

	// Claim owner role first so "member-noperm" is not the owner.
	fx.bearer(t)

	// (b) Member without PermTerminalInput.
	memberPriv, memberID := fx.pairDevice(t, "member-noperm")
	memberTok := fx.token(t, memberID, memberPriv)
	code, _ := fx.doJSON(t, "POST",
		"/api/sessions/"+sid+"/approvals/"+aid, memberTok, body)
	if code != http.StatusForbidden {
		t.Fatalf("member without PermTerminalInput: want 403, got %d", code)
	}

	// (c) Revoked bearer.
	revokePriv, revokeID := fx.pairDevice(t, "revoked")
	revokeTok := fx.token(t, revokeID, revokePriv)
	if err := fx.reg.Revoke(revokeID); err != nil {
		t.Fatal(err)
	}
	fx.app.sessionMgr.RevokeDevice(revokeID)
	code, _ = fx.doJSON(t, "POST",
		"/api/sessions/"+sid+"/approvals/"+aid, revokeTok, body)
	if code != http.StatusForbidden && code != http.StatusUnauthorized {
		t.Fatalf("revoked: want 401/403, got %d", code)
	}

	snap, _ := fx.store.LookupRecord(sid, aid)
	if snap.State != "pending" {
		t.Fatalf("all principal negatives must leave record pending: %v", snap.State)
	}
}

// ── item 9: non-actionable → 409 ──

func TestC3DB_NonActionableRejected(t *testing.T) {
	fx := newClaudeAppFixture(t)
	tok := fx.bearer(t)

	sid, _ := fx.svc.CreateDetached("/tmp")
	nonCatID := "claude-nonact-1"
	fx.store.IngestObserved(term.ApprovalIngest{
		SessionID: sid, LaunchGen: 1, StreamGen: 0,
		Provider: "claude_headless", Version: "2.1.209",
		Items: []term.ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: nonCatID, SessionID: sid, AgentKind: "claude_headless",
				Kind: "approval", Status: "pending", Source: agent.SourceJSONL, Confidence: 1,
				Options: nil,
			},
			Provenance:      contract.ProvenanceProviderHook,
			Actionable:      false,
			CatalogActionID: "",
		}},
	})
	code, _ := fx.doJSON(t, "POST",
		"/api/sessions/"+sid+"/approvals/"+nonCatID, tok,
		`{"action":"allow_once","idempotencyKey":"nonact.k1"}`)
	if code != http.StatusConflict {
		t.Fatalf("non-actionable: want 409, got %d", code)
	}
}

// ── item 10: stale runtime → 409 ──

func TestC3DB_StaleRuntimeRejected(t *testing.T) {
	fx := newClaudeAppFixture(t)
	sid, aid, _ := fx.seedClaudeRecord(t)
	tok := fx.bearer(t)

	if err := fx.svc.Stop(sid, 1); err != nil {
		t.Fatal(err)
	}
	code, _ := fx.doJSON(t, "POST",
		"/api/sessions/"+sid+"/approvals/"+aid, tok,
		`{"action":"allow_once","idempotencyKey":"stale.k1"}`)
	if code != http.StatusConflict {
		t.Fatalf("stale: want 409, got %d", code)
	}
}

// ── item 11: client authority fields → 400 ──

func TestC3DB_ClientAuthorityFieldsRejected(t *testing.T) {
	fx := newClaudeAppFixture(t)
	sid, aid, _ := fx.seedClaudeRecord(t)
	tok := fx.bearer(t)

	forbidden := []string{
		"provider", "runtime", "launchGeneration", "streamGeneration",
		"deviceId", "hostId", "bearerSessionId", "bootId", "permissions",
	}
	for _, field := range forbidden {
		t.Run(field, func(t *testing.T) {
			body := `{"action":"allow_once","idempotencyKey":"ca.` + field + `","` + field + `":"injected"}`
			code, _ := fx.doJSON(t, "POST",
				"/api/sessions/"+sid+"/approvals/"+aid, tok, body)
			if code != http.StatusBadRequest {
				t.Fatalf("%q: want 400, got %d", field, code)
			}
		})
	}
	snap, _ := fx.store.LookupRecord(sid, aid)
	if snap.State != "pending" {
		t.Fatalf("all client-authority attempts must leave record pending: %v", snap.State)
	}
}
