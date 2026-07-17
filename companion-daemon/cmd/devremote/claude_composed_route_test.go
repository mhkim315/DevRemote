// C3D-B items 4-11 — composed authenticated claim-and-commit through the
// real App-owned route, middleware, Store, resolver and dispatcher.
//
// Every allow/deny test sends a real HTTP request to the registered
// approval route, drives the resume hook + witness through the launcher's
// recorded bridge URLs, and verifies the production dispatch chain all the
// way through delivery receipt and Store commit with approved/rejected DTO.
// No direct call to HandleApprovalAction; no fake receipt; no timeout
// shortening.
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// seedNonActionableRecord ingests a second, NON-actionable observed record on
// an existing session. Used as the current-host/current-boot positive control:
// a valid principal reaches the handler and gets the deterministic 409
// (not_actionable) without entering the delivery path.
func (fx *claudeAppFixture) seedNonActionableRecord(t *testing.T, sid, aid string) {
	t.Helper()
	fx.store.IngestObserved(term.ApprovalIngest{
		SessionID: sid, LaunchGen: 1, StreamGen: 0,
		Provider: "claude_headless", Version: "2.1.209",
		Items: []term.ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: aid, SessionID: sid, AgentKind: "claude_headless",
				Kind: "approval", Status: "pending", Source: agent.SourceJSONL, Confidence: 1,
				Options: nil,
			},
			Provenance:      contract.ProvenanceProviderHook,
			Actionable:      false,
			CatalogActionID: "",
		}},
	})
}

// launchCount returns how many processes the fake launcher has started. The
// FIRST launch per fixture is CreateDetached; a claim that reaches delivery
// starts a SECOND (resume) launch, so an unchanged count is the observable
// zero-resume/zero-provider-delivery state.
func launchCount(l *compFakeLauncher) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.allArgs)
}

// newRemoteFixtureInDir mirrors newRemoteFixtureWith but pins the host
// identity/device registry directory, so two App incarnations can share ONE
// persistent host identity and paired-device set while each owns its own
// DeviceSessionManager and fresh random boot ID — the exact daemon-restart
// shape the old-boot negative requires.
func newRemoteFixtureInDir(t *testing.T, dir string, mutate func(*Config, *Dependencies)) *remoteFixture {
	t.Helper()
	id, err := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: dir + "/host.json"})
	if err != nil {
		t.Fatalf("host identity: %v", err)
	}
	reg, err := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: dir + "/devices.json"})
	if err != nil {
		t.Fatalf("device registry: %v", err)
	}
	deps := testDeps()
	cfg := Config{InsecureLocalOnly: false}
	if mutate != nil {
		mutate(&cfg, &deps)
	}
	app, err := NewAppWithDeps(cfg, deps)
	if err != nil {
		t.Fatalf("NewAppWithDeps: %v", err)
	}
	// Production late-wiring performed by App.Run(); replicate it exactly.
	app.hostIdentity = id
	app.deviceRegistry = reg
	app.handlers.HostIdentity = id
	app.authHandler.Identity = id
	app.authHandler.Registry = reg

	srv := httptest.NewServer(app.server.Handler)
	t.Cleanup(srv.Close)
	return &remoteFixture{app: app, srv: srv, id: id, reg: reg}
}

// newClaudeAppFixtureInDir is newClaudeAppFixture with the identity/registry
// directory pinned (see newRemoteFixtureInDir).
func newClaudeAppFixtureInDir(t *testing.T, dir string) *claudeAppFixture {
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
	f := newRemoteFixtureInDir(t, dir, func(cfg *Config, deps *Dependencies) {
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

// captureResumeBridgeURLs polls for the second launch (resume) in the
// launcher and extracts the hook URLs from the hook script files on disk.
func captureResumeBridgeURLs(t *testing.T, launcher *compFakeLauncher) (resumeURL, posttoolURL string) {
	t.Helper()
	for i := 0; i < 500; i++ {
		time.Sleep(10 * time.Millisecond)
		launcher.mu.Lock()
		n := len(launcher.allArgs)
		launcher.mu.Unlock()
		if n >= 2 {
			launcher.mu.Lock()
			args := launcher.allArgs[1]
			launcher.mu.Unlock()
			for j, a := range args {
				if a == "--settings" && j+1 < len(args) {
					hd := strings.TrimSuffix(args[j+1], "/settings.json")
					return readHookURL(t, hd+"/hook_resume.sh"), readHookURL(t, hd+"/hook_posttool.sh")
				}
			}
			t.Fatal("cannot find --settings in resume launch args")
		}
	}
	t.Fatal("resume launch never observed")
	return
}

// ── item 4: allow claim through composed route → witness → commit ──

func TestC3DB_AllowClaimCommitted(t *testing.T) {
	fx := newClaudeAppFixture(t)
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
			`{"action":"allow_once","idempotencyKey":"c3db.allow"}`)
	}()
	resumeURL, posttoolURL := captureResumeBridgeURLs(t, fx.launcher)
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
	// DTO reflects the committed state.
	snap, _ := fx.store.LookupRecord(sid, aid)
	if snap.State != "approved" {
		t.Fatalf("store state after allow: %v", snap.State)
	}
}

// ── item 5: deny claim through composed route → denial witness → commit ──

func TestC3DB_DenyClaimCommitted(t *testing.T) {
	fx := newClaudeAppFixture(t)
	sid, aid, _ := fx.seedClaudeRecord(t)
	tok := fx.bearer(t)

	csid, tuid := "cs-c3db", "tu-c3db"
	var code int
	done := make(chan struct{})
	go func() {
		defer close(done)
		code, _ = fx.doJSON(t, "POST",
			"/api/sessions/"+sid+"/approvals/"+aid, tok,
			`{"action":"deny","idempotencyKey":"c3db.deny"}`)
	}()
	resumeURL, _ := captureResumeBridgeURLs(t, fx.launcher)
	r := fireResumeHook(t, resumeURL, csid, tuid, "Bash",
		`{"command":"echo pokitclaudeapprovalprobe"}`)
	if string(r) != string(term.ClaudeHookResponseBytes("deny")) {
		t.Fatalf("resume response: %s", r)
	}
	denialLine := `{"type":"result","session_id":"` + csid + `","permission_denials":[{"tool_use_id":"` + tuid + `","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"}}]}` + "\n"
	fx.launcher.mu.Lock()
	w := fx.launcher.resumeW
	fx.launcher.mu.Unlock()
	if w == nil {
		t.Fatal("resumeW pipe writer is nil")
	}
	w.Write([]byte(denialLine))
	<-done

	if code != http.StatusOK {
		t.Fatalf("deny claim: code=%d", code)
	}
	snap, _ := fx.store.LookupRecord(sid, aid)
	if snap.State != "rejected" {
		t.Fatalf("store state after deny: %v", snap.State)
	}
}

// ── item 6: duplicate → already_accepted (both through composed route) ──

func TestC3DB_DuplicateAlreadyAccepted(t *testing.T) {
	fx := newClaudeAppFixture(t)
	sid, aid, _ := fx.seedClaudeRecord(t)
	tok := fx.bearer(t)

	csid, tuid, key := "cs-c3db", "tu-c3db", "c3db.dup"
	// First claim: hook-driven delivery → commit approved.
	var code1 int
	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		code1, _ = fx.doJSON(t, "POST",
			"/api/sessions/"+sid+"/approvals/"+aid, tok,
			`{"action":"allow_once","idempotencyKey":"`+key+`"}`)
	}()
	resumeURL, posttoolURL := captureResumeBridgeURLs(t, fx.launcher)
	fireResumeHook(t, resumeURL, csid, tuid, "Bash",
		`{"command":"echo pokitclaudeapprovalprobe"}`)
	firePostToolHook(t, posttoolURL, csid, tuid, "Bash",
		`{"command":"echo pokitclaudeapprovalprobe"}`)
	<-done1
	if code1 != http.StatusOK {
		t.Fatalf("first claim: code=%d", code1)
	}

	// Second claim: same key, same action → already_accepted (no delivery).
	code2, resp2 := fx.doJSON(t, "POST",
		"/api/sessions/"+sid+"/approvals/"+aid, tok,
		`{"action":"allow_once","idempotencyKey":"`+key+`"}`)
	if code2 != http.StatusOK {
		t.Fatalf("duplicate: code=%d body=%s", code2, resp2)
	}
	if !strings.Contains(resp2, "already_accepted") {
		t.Fatalf("expected already_accepted: %s", resp2)
	}
}

// ── item 7: same key + different option → conflict (through route) ──

func TestC3DB_ChangedOptionConflict(t *testing.T) {
	fx := newClaudeAppFixture(t)
	sid, aid, _ := fx.seedClaudeRecord(t)
	tok := fx.bearer(t)

	csid, tuid, key := "cs-c3db", "tu-c3db", "c3db.conflict"
	// First claim → granted + delivered.
	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		fx.doJSON(t, "POST",
			"/api/sessions/"+sid+"/approvals/"+aid, tok,
			`{"action":"allow_once","idempotencyKey":"`+key+`"}`)
	}()
	resumeURL, posttoolURL := captureResumeBridgeURLs(t, fx.launcher)
	fireResumeHook(t, resumeURL, csid, tuid, "Bash",
		`{"command":"echo pokitclaudeapprovalprobe"}`)
	firePostToolHook(t, posttoolURL, csid, tuid, "Bash",
		`{"command":"echo pokitclaudeapprovalprobe"}`)
	<-done1

	// Second claim: same key, DIFFERENT option → conflict (no delivery).
	code2, _ := fx.doJSON(t, "POST",
		"/api/sessions/"+sid+"/approvals/"+aid, tok,
		`{"action":"deny","idempotencyKey":"`+key+`"}`)
	if code2 != http.StatusConflict {
		t.Fatalf("conflict: want 409, got %d", code2)
	}
}

// ── item 8: server-derived requester full negative matrix ──

func TestC3DB_PrincipalNegativeMatrix(t *testing.T) {
	fx := newClaudeAppFixture(t)
	sid, aid, _ := fx.seedClaudeRecord(t)
	nonActID := "claude-c3db-nonact-ctl"
	fx.seedNonActionableRecord(t, sid, nonActID)
	body := `{"action":"allow_once","idempotencyKey":"pri.k"}`

	// Zero-delivery baseline: exactly the CreateDetached launch, the one
	// reserved coordinator identity, and no resume entry.
	launchesBefore := launchCount(fx.launcher)
	identsBefore := fx.svc.Coordinator().IdentityCount()
	entriesBefore := fx.svc.Coordinator().EntryCount()

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

	// Claim owner role first so subsequent devices are members. The owner
	// bearer is the current-host/current-boot control principal below.
	ownerTok := fx.bearer(t)

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
	fx.reg.Revoke(revokeID)
	fx.app.sessionMgr.RevokeDevice(revokeID)
	code, _ = fx.doJSON(t, "POST",
		"/api/sessions/"+sid+"/approvals/"+aid, revokeTok, body)
	if code != http.StatusForbidden && code != http.StatusUnauthorized {
		t.Fatalf("revoked: want 401/403, got %d", code)
	}

	// (d) Foreign-host bearer: minted by a GENUINELY different App/host
	// fixture (its own host identity, device registry, session manager and
	// boot), POSTed to the TARGET App's registered approval route.
	t.Run("foreign_host", func(t *testing.T) {
		foreign := newRemoteFixture(t, nil, nil)
		if foreign.id.HostID == fx.id.HostID {
			t.Fatal("foreign fixture must have a different host identity")
		}
		foreignPriv, foreignID := foreign.pairDevice(t, "foreign-owner")
		foreignTok := foreign.token(t, foreignID, foreignPriv)
		if foreignTok == "" {
			t.Fatal("foreign fixture must mint a real bearer")
		}
		// The foreign OWNER bearer is fully privileged ON ITS OWN HOST —
		// the target must still reject it before any claim.
		code, respBody := fx.doJSON(t, "POST",
			"/api/sessions/"+sid+"/approvals/"+aid, foreignTok, body)
		if code != http.StatusUnauthorized && code != http.StatusForbidden {
			t.Fatalf("foreign-host bearer: want 401/403, got %d body=%s", code, respBody)
		}

		// Positive control: the current-host/current-boot owner bearer on the
		// SAME route shape reaches the handler. The non-actionable record
		// returns the deterministic 409 (not_actionable) without entering the
		// 120-second delivery path.
		ctlCode, ctlBody := fx.doJSON(t, "POST",
			"/api/sessions/"+sid+"/approvals/"+nonActID, ownerTok,
			`{"action":"allow_once","idempotencyKey":"pri.fh.ctl"}`)
		if ctlCode != http.StatusConflict {
			t.Fatalf("current-host control: want 409, got %d body=%s", ctlCode, ctlBody)
		}
		if !strings.Contains(ctlBody, "not_actionable") {
			t.Fatalf("current-host control must reach the claim handler: %s", ctlBody)
		}
	})

	// (e) Old-boot bearer: SAME persistent host identity and device
	// registry, but the bearer was minted by a PRIOR App/session-manager
	// incarnation (old boot ID). The current incarnation must reject it.
	t.Run("old_boot", func(t *testing.T) {
		dir := t.TempDir()

		// Incarnation 1 (old boot): pair the owner device and mint a bearer.
		oldApp := newRemoteFixtureInDir(t, dir, nil)
		ownerPriv, ownerID := oldApp.pairDevice(t, "owner")
		oldBootTok := oldApp.token(t, ownerID, ownerPriv)
		if oldBootTok == "" {
			t.Fatal("old incarnation must mint a real bearer")
		}

		// Incarnation 2 (current boot): same host.json/devices.json, fresh
		// App, fresh DeviceSessionManager, fresh random boot ID.
		cur := newClaudeAppFixtureInDir(t, dir)
		if cur.id.HostID != oldApp.id.HostID {
			t.Fatal("restart simulation must preserve the host identity")
		}
		if cur.app.sessionMgr.BootID() == oldApp.app.sessionMgr.BootID() {
			t.Fatal("restart simulation must change the boot ID")
		}
		sid2, aid2, _ := cur.seedClaudeRecord(t)
		nonActID2 := "claude-c3db-nonact-boot-ctl"
		cur.seedNonActionableRecord(t, sid2, nonActID2)
		launches2 := launchCount(cur.launcher)
		idents2 := cur.svc.Coordinator().IdentityCount()

		// The old-boot bearer against the CURRENT App's real approval route.
		code, respBody := cur.doJSON(t, "POST",
			"/api/sessions/"+sid2+"/approvals/"+aid2, oldBootTok, body)
		if code != http.StatusUnauthorized && code != http.StatusForbidden {
			t.Fatalf("old-boot bearer: want 401/403, got %d body=%s", code, respBody)
		}

		// Positive control: the SAME device re-authenticates against the
		// CURRENT incarnation (current boot) and reaches the handler → 409
		// on the non-actionable record, no delivery.
		curTok := cur.token(t, ownerID, ownerPriv)
		ctlCode, ctlBody := cur.doJSON(t, "POST",
			"/api/sessions/"+sid2+"/approvals/"+nonActID2, curTok,
			`{"action":"allow_once","idempotencyKey":"pri.boot.ctl"}`)
		if ctlCode != http.StatusConflict {
			t.Fatalf("current-boot control: want 409, got %d body=%s", ctlCode, ctlBody)
		}
		if !strings.Contains(ctlBody, "not_actionable") {
			t.Fatalf("current-boot control must reach the claim handler: %s", ctlBody)
		}

		// Zero delivery on the current incarnation: record still pending,
		// no resume launch, no new coordinator identity, no resume entry.
		snap, ok := cur.store.LookupRecord(sid2, aid2)
		if !ok || snap.State != "pending" {
			t.Fatalf("old-boot negatives must leave record pending: ok=%v state=%v", ok, snap.State)
		}
		if got := launchCount(cur.launcher); got != launches2 {
			t.Fatalf("old-boot negatives caused a launch: %d→%d (resume delivery)", launches2, got)
		}
		if got := cur.svc.Coordinator().IdentityCount(); got != idents2 {
			t.Fatalf("coordinator identity count changed: %d→%d", idents2, got)
		}
		if got := cur.svc.Coordinator().EntryCount(); got != 0 {
			t.Fatalf("coordinator gained a resume entry: %d", got)
		}
	})

	// Zero delivery on the primary fixture across ALL negatives and the
	// foreign-host control: the actionable record is untouched pending, the
	// launcher never started a resume process, and the coordinator holds
	// exactly its baseline identity set and zero resume entries.
	snap, ok := fx.store.LookupRecord(sid, aid)
	if !ok || snap.State != "pending" {
		t.Fatalf("all principal negatives must leave record pending: ok=%v state=%v", ok, snap.State)
	}
	if got := launchCount(fx.launcher); got != launchesBefore {
		t.Fatalf("principal negatives caused a launch: %d→%d (resume delivery)", launchesBefore, got)
	}
	if got := fx.svc.Coordinator().IdentityCount(); got != identsBefore {
		t.Fatalf("coordinator identity count changed: %d→%d", identsBefore, got)
	}
	if got := fx.svc.Coordinator().EntryCount(); got != entriesBefore {
		t.Fatalf("coordinator entry count changed: %d→%d", entriesBefore, got)
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
		t.Fatalf("client-authority attempts must leave record pending: %v", snap.State)
	}
}
