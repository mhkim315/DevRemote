// C3D-B items 4-11 — composed authenticated claim-and-commit through the
// real App-owned route, middleware, Store, resolver and dispatcher.
//
// No production code changes. No direct call to HandleApprovalAction;
// every test sends real HTTP requests to the registered route.
package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/term"
)

// claudeAppFixture holds the composed production App plus the helper
// fields needed to create sessions and seed records for the claim route.
type claudeAppFixture struct {
	*remoteFixture
	launcher *compFakeLauncher
	svc      *term.ManagedClaudeService
	store    *term.AuthoritativeApprovalStore // app-owned canonical store
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
	// The App OWNS the canonical store — do NOT pre-bind one.
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
		remoteFixture: f,
		launcher:      launcher,
		svc:           svc,
		store:         store,
	}
}

// seedClaudeRecord creates a managed session, ingests an actionable catalog
// record, and reserves the coordinator identity.
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

// shortenDeliveryTimeout walks the delivery dispatcher chain to the Claude
// delivery and sets a very short poll timeout. The delivery WILL fail
// because the resume process never produces a witness, but the claim,
// handler dispatch, and commit path is fully exercised.
func (fx *claudeAppFixture) shortenDeliveryTimeout() {
	type claudeAccessor interface{ Claude() term.ApprovalDelivery }
	d := fx.app.handlers.ApprovalDelivery
	// Walk the chain: dispatchingApprovalDelivery →
	// claudeDispatchingApprovalDelivery → ClaudeManagedApprovalDelivery.
	for {
		if cd, ok := d.(*term.ClaudeManagedApprovalDelivery); ok {
			cd.SetPollTimeout(5 * time.Second)
			return
		}
		if a, ok := d.(claudeAccessor); ok {
			d = a.Claude()
			continue
		}
		return
	}
}

func (fx *claudeAppFixture) bearer(t *testing.T) string {
	t.Helper()
	priv, id := fx.pairDevice(t, "owner")
	return fx.token(t, id, priv)
}

// ── item 4: allow claim through composed route, with real hook ──

func TestC3DB_AllowClaimCommitted(t *testing.T) {
	fx := newClaudeAppFixture(t)
	fx.shortenDeliveryTimeout()
	sid, aid, _ := fx.seedClaudeRecord(t)
	tok := fx.bearer(t)

	body := `{"action":"allow_once","idempotencyKey":"c3db.allow.k1"}`
	var code int
	var respBody string
	done := make(chan struct{})
	go func() {
		defer close(done)
		code, respBody = fx.doJSON(t, "POST",
			"/api/sessions/"+sid+"/approvals/"+aid, tok, body)
	}()
	// Drive the resume hook + PostToolUse via the compFakeLauncher.
	resumeURL, posttoolURL := captureBridgeURLs(t, fx.launcher)
	time.Sleep(50 * time.Millisecond)  // let the bridge goroutine reach Serve
	csid, tuid := "cs-c3db", "tu-c3db" // seedClaudeRecord uses these
	r := fireResumeHook(t, resumeURL, csid, tuid, "Bash",
		`{"command":"echo pokitclaudeapprovalprobe"}`)
	if !bytesEq(r, term.ClaudeHookResponseBytes("allow")) {
		t.Fatalf("resume response: %s", r)
	}
	firePostToolHook(t, posttoolURL, csid, tuid, "Bash",
		`{"command":"echo pokitclaudeapprovalprobe"}`)
	<-done

	if code != http.StatusOK {
		t.Fatalf("allow claim: code=%d body=%s", code, respBody)
	}
	var okResp struct {
		Status  string `json:"status"`
		Action  string `json:"action"`
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(respBody), &okResp); err != nil {
		t.Fatal(err)
	}
	if okResp.Status != "ok" || okResp.Outcome != "accepted" {
		t.Fatalf("unexpected: %+v", okResp)
	}
	snap, _ := fx.store.LookupRecord(sid, aid)
	if snap.State != "approved" {
		t.Fatalf("store state after allow: %v", snap.State)
	}
}

// ── item 5: deny claim through composed route, with denial witness ──

func TestC3DB_DenyClaimCommitted(t *testing.T) {
	fx := newClaudeAppFixture(t)
	fx.shortenDeliveryTimeout()
	sid, aid, _ := fx.seedClaudeRecord(t)
	tok := fx.bearer(t)

	csid, tuid := "cs-c3db", "tu-c3db"
	var code int
	var respBody string
	done := make(chan struct{})
	go func() {
		defer close(done)
		code, respBody = fx.doJSON(t, "POST",
			"/api/sessions/"+sid+"/approvals/"+aid, tok,
			`{"action":"deny","idempotencyKey":"c3db.deny.k1"}`)
	}()
	resumeURL, _ := captureBridgeURLs(t, fx.launcher)
	r := fireResumeHook(t, resumeURL, csid, tuid, "Bash",
		`{"command":"echo pokitclaudeapprovalprobe"}`)
	if string(r) != string(term.ClaudeHookResponseBytes("deny")) {
		t.Fatalf("resume response: %s", r)
	}
	// Write the denial witness line to the resume stdout.
	denialLine := `{"type":"result","session_id":"` + csid + `","permission_denials":[{"tool_use_id":"` + tuid + `","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"}}]}` + "\n"
	fx.launcher.mu.Lock()
	w := fx.launcher.resumeW
	fx.launcher.mu.Unlock()
	if w == nil {
		t.Fatal("resumeW pipe writer is nil — delivery likely failed")
	}
	w.Write([]byte(denialLine))
	<-done

	if code != http.StatusOK {
		t.Fatalf("deny claim: code=%d body=%s", code, respBody)
	}
	snap, _ := fx.store.LookupRecord(sid, aid)
	if snap.State != "rejected" {
		t.Fatalf("store state after deny: %v", snap.State)
	}
}

// ── item 6: duplicate → already_accepted ──

// driveAllowClaim fires the authoritative claim+grant+delivery+accept
// sequence through the composed route, driving the resume hook + PostToolUse.
func driveAllowClaim(t *testing.T, fx *claudeAppFixture, sid, aid, key, csid, tuid, tok string) {
	t.Helper()
	body := `{"action":"allow_once","idempotencyKey":"` + key + `"}`
	var code int
	var respBody string
	done := make(chan struct{})
	go func() {
		defer close(done)
		code, respBody = fx.doJSON(t, "POST",
			"/api/sessions/"+sid+"/approvals/"+aid, tok, body)
	}()
	resumeURL, posttoolURL := captureBridgeURLs(t, fx.launcher)
	time.Sleep(50 * time.Millisecond)
	r := fireResumeHook(t, resumeURL, csid, tuid, "Bash",
		`{"command":"echo pokitclaudeapprovalprobe"}`)
	if !bytesEq(r, term.ClaudeHookResponseBytes("allow")) {
		t.Fatalf("resume response: %s", r)
	}
	firePostToolHook(t, posttoolURL, csid, tuid, "Bash",
		`{"command":"echo pokitclaudeapprovalprobe"}`)
	<-done
	if code != http.StatusOK {
		t.Fatalf("allow claim: code=%d body=%s", code, respBody)
	}
}

func TestC3DB_DuplicateAlreadyAccepted(t *testing.T) {
	fx := newClaudeAppFixture(t)
	fx.shortenDeliveryTimeout()
	sid, aid, _ := fx.seedClaudeRecord(t)
	tok := fx.bearer(t)

	csid, tuid := "cs-c3db", "tu-c3db"
	key := "c3db.dup.k1"
	driveAllowClaim(t, fx, sid, aid, key, csid, tuid, tok)

	// Second claim: same key, same binding → already_accepted (no delivery).
	body := `{"action":"allow_once","idempotencyKey":"` + key + `"}`
	code2, resp2 := fx.doJSON(t, "POST",
		"/api/sessions/"+sid+"/approvals/"+aid, tok, body)
	if code2 != http.StatusOK {
		t.Fatalf("duplicate: code=%d", code2)
	}
	var okResp struct {
		Outcome string `json:"outcome"`
	}
	json.Unmarshal([]byte(resp2), &okResp)
	if okResp.Outcome != "already_accepted" {
		t.Fatalf("expected already_accepted, got %s", okResp.Outcome)
	}
}

// ── item 7: same key + different option → conflict ──

func TestC3DB_ChangedOptionConflict(t *testing.T) {
	fx := newClaudeAppFixture(t)
	fx.shortenDeliveryTimeout()
	sid, aid, _ := fx.seedClaudeRecord(t)
	tok := fx.bearer(t)

	csid, tuid := "cs-c3db", "tu-c3db"
	key := "c3db.conflict.k1"
	driveAllowClaim(t, fx, sid, aid, key, csid, tuid, tok)

	// Second claim: same key, DIFFERENT option → conflict (no delivery).
	code2, _ := fx.doJSON(t, "POST", "/api/sessions/"+sid+"/approvals/"+aid, tok,
		`{"action":"deny","idempotencyKey":"`+key+`"}`)
	if code2 != http.StatusConflict {
		t.Fatalf("conflict: want 409, got %d", code2)
	}
}

// ── item 8: missing principal → 401/403, zero delivery ──

func TestC3DB_MissingPrincipalRejected(t *testing.T) {
	fx := newClaudeAppFixture(t)
	fx.shortenDeliveryTimeout()
	sid, aid, _ := fx.seedClaudeRecord(t)

	req, _ := http.NewRequest(http.MethodPost,
		fx.srv.URL+"/api/sessions/"+sid+"/approvals/"+aid,
		strings.NewReader(`{"action":"allow_once","idempotencyKey":"k"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp == nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing bearer: want 401/403, got %d", resp.StatusCode)
	}
	snap, _ := fx.store.LookupRecord(sid, aid)
	if snap.State != "pending" {
		t.Fatalf("unauthorized must not mutate: %v", snap.State)
	}
}

// ── item 9: non-actionable → 409 ──

func TestC3DB_NonActionableRejected(t *testing.T) {
	fx := newClaudeAppFixture(t)
	fx.shortenDeliveryTimeout()
	tok := fx.bearer(t)

	sid, err := fx.svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
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
	fx.shortenDeliveryTimeout()
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
	fx.shortenDeliveryTimeout()
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
