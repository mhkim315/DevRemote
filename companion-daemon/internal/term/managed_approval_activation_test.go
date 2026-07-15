package term

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/devicetrust"
)

// ── SP1-P2B: atomic capability activation ──
//
// These tests drive the SAME InstallApprovalExecution transition, dispatcher
// and RuntimeOf resolver the production composition root wires, the REAL
// HandleApprovalAction (device principal via RequirePrincipal), the real
// store, and the production pump.

// activatedHandlers builds Handlers exactly as the composition root does on a
// successful install.
func activatedHandlers(s *deliverySession, cd *CodexManagedApprovalDelivery, runtimeOf func(string) (RuntimeRef, bool)) *Handlers {
	gate := NewGatedApprovalDelivery(NewRuntimeDeliveryGate())
	return &Handlers{
		Approvals:        s.store,
		ApprovalDelivery: NewDispatchingApprovalDelivery(cd, gate),
		RuntimeOf:        runtimeOf,
	}
}

// TestActivation_ProductionActionEndToEnd: the full production chain — wire
// observation arms the pending identity AND ingests the actionable record
// with exactly the certified options; the authenticated action POST claims,
// the dispatcher routes to the certified boundary, exactly the daemon
// response bytes are written once, the pump-observed resolved commits
// approved (allow_once) / rejected (deny).
func TestActivation_ProductionActionEndToEnd(t *testing.T) {
	s, cd, runtimeOf := newActivatedDeliverySession(t, false)
	h := activatedHandlers(s, cd, runtimeOf)
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	bearer := ownerBearer(t, m, "phone-1")

	// allow_once → accept.
	s.inject(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
	s.sync(t)
	got := waitForSafeApprovals(t, s.store, s.id, 1)
	dto := got[0]
	if !dto.Actionable || len(dto.Options) != 2 ||
		dto.Options[0].ID != "allow_once" || dto.Options[0].Kind != "approve" || dto.Options[0].Label != "Approve" ||
		dto.Options[1].ID != "deny" || dto.Options[1].Kind != "reject" || dto.Options[1].Label != "Reject" {
		t.Fatalf("actionable DTO must expose exactly the certified options: %+v", dto)
	}
	cd.timeout = 5 * time.Second
	s.resolveAtWrittenMark(t, cd, "7")
	rr := routeApproval(m, h, bearer, s.id, "codexas-1-7", `{"action":"allow_once","idempotencyKey":"k1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("allow_once action: code=%d body=%s", rr.Code, rr.Body.String())
	}
	if snap, _ := s.store.LookupRecord(s.id, "codexas-1-7"); snap.State != ApprovalApproved {
		t.Fatalf("state=%q want approved", snap.State)
	}
	s.waitWritten(t, 1)
	if lines := s.written.snapshot(); !bytes.Equal(lines[0], codexDecisionResponse("7", "accept")) {
		t.Fatalf("exact accept bytes expected: %q", lines[0])
	}

	// deny → decline on a second observed request.
	s.inject(t, certifiedApprovalRaw("8", apprTestTurn, "item-5"))
	s.sync(t)
	s.resolveAtWrittenMark(t, cd, "8")
	rr = routeApproval(m, h, bearer, s.id, "codexas-1-8", `{"action":"deny","idempotencyKey":"k2"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("deny action: code=%d body=%s", rr.Code, rr.Body.String())
	}
	if snap, _ := s.store.LookupRecord(s.id, "codexas-1-8"); snap.State != ApprovalRejected {
		t.Fatalf("state=%q want rejected", snap.State)
	}
	s.waitWritten(t, 2)
	if lines := s.written.snapshot(); !bytes.Equal(lines[1], codexDecisionResponse("8", "decline")) {
		t.Fatalf("exact decline bytes expected: %q", lines[1])
	}
}

// TestActivation_NoResolvedNeverCommits: with the production wiring, an
// action whose provider never resolves ends delivery_failed — queue-less,
// never approved (no queue-only commit exists on this path).
func TestActivation_NoResolvedNeverCommits(t *testing.T) {
	s, cd, runtimeOf := newActivatedDeliverySession(t, false)
	h := activatedHandlers(s, cd, runtimeOf)
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	s.inject(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
	s.sync(t)
	waitForSafeApprovals(t, s.store, s.id, 1)

	cd.timeout = 100 * time.Millisecond
	rr := routeApproval(m, h, ownerBearer(t, m, "phone-1"), s.id, "codexas-1-7", `{"action":"allow_once","idempotencyKey":"k1"}`)
	if rr.Code == http.StatusOK {
		t.Fatalf("no resolved must never be a success: %s", rr.Body.String())
	}
	if snap, _ := s.store.LookupRecord(s.id, "codexas-1-7"); snap.State != ApprovalDeliveryFailed {
		t.Fatalf("state=%q want delivery_failed", snap.State)
	}
	s.waitWritten(t, 1) // the single write happened; admission was never success
}

// TestActivation_PreconditionsFailClosed: every failed precondition leaves
// BOTH capabilities off with no partial state.
func TestActivation_PreconditionsFailClosed(t *testing.T) {
	// nil store.
	{
		s := buildDeliverySession(t, false)
		if _, _, err := s.managed.InstallApprovalExecution(nil); err == nil {
			t.Fatalf("nil store must fail")
		}
	}
	// wrong authority version: install fails; observation also rejects
	// (uncertified version), so zero records and RuntimeOf false.
	{
		s := buildDeliverySession(t, false)
		bad := NewManagedCodexService(CodexAppServerEntryConfig{
			Bin: "/pinned/toolchain/node_modules/.bin/codex", Version: "codex-cli 0.144.4",
			AuthorityVersion: "0.144.4",
		}, s.launcher)
		bad.verify = func() error { return nil }
		if _, _, err := bad.InstallApprovalExecution(s.store); err == nil {
			t.Fatalf("uncertified authority version must fail install")
		}
		if _, ok := bad.RuntimeOf("codex_app_server:x"); ok {
			t.Fatalf("uninstalled RuntimeOf must fail")
		}
	}
	// install after the first runtime exists.
	{
		s := newDeliverySession(t, false)
		if _, _, err := s.managed.InstallApprovalExecution(s.store); err == nil {
			t.Fatalf("install after a create must fail")
		}
		// The session stays P1 observation-only: certified request →
		// non-actionable record, no claim.
		s.inject(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
		s.sync(t)
		got := waitForSafeApprovals(t, s.store, s.id, 1)
		if got[0].Actionable || len(got[0].Options) != 0 {
			t.Fatalf("failed install must leave ingestion non-actionable: %+v", got)
		}
		if _, ok := s.managed.RuntimeOf(s.id); ok {
			t.Fatalf("failed install must leave RuntimeOf off")
		}
	}
	// double install.
	{
		s, _, _ := newActivatedDeliverySession(t, false)
		if _, _, err := s.managed.InstallApprovalExecution(s.store); err == nil {
			t.Fatalf("double install must fail")
		}
	}
	// shutdown.
	{
		s := buildDeliverySession(t, false)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.managed.Shutdown(ctx); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
		if _, _, err := s.managed.InstallApprovalExecution(s.store); err == nil {
			t.Fatalf("install during shutdown must fail")
		}
	}
}

// TestActivation_NoOldRecordUpgrade: a non-actionable record is never
// upgraded — an actionable re-offer of the same ID (different fingerprint)
// is not admitted and the stored record stays unclaimable.
func TestActivation_NoOldRecordUpgrade(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	sid := codexAppServerAdapter + ":codex-app-u"
	// Non-actionable P1-shaped record.
	if !store.IngestObserved(ApprovalIngest{
		SessionID: sid, LaunchGen: 1, StreamGen: 0,
		Provider: codexAppServerAdapter, Version: certifiedCodexAuthorityVersion,
		Items: []ApprovalIngestItem{{
			Approval:     agent.AgentApproval{ID: "codexas-1-7", SessionID: sid, AgentKind: codexAppServerAdapter, Kind: "approval", Source: agent.SourceJSONL, Confidence: 1},
			Provenance:   contract.ProvenanceProviderProtocol,
			Actionable:   false,
			RequiredPerm: devicetrust.PermTerminalInput,
		}},
	}) {
		t.Fatalf("baseline ingest refused")
	}
	// Actionable re-offer of the SAME ID with certified options + material.
	if store.IngestObserved(ApprovalIngest{
		SessionID: sid, LaunchGen: 1, StreamGen: 0,
		Provider: codexAppServerAdapter, Version: certifiedCodexAuthorityVersion,
		Items: []ApprovalIngestItem{{
			Approval:         agent.AgentApproval{ID: "codexas-1-7", SessionID: sid, AgentKind: codexAppServerAdapter, Kind: "approval", Options: codexCertifiedOptions(), Source: agent.SourceJSONL, Confidence: 1},
			Provenance:       contract.ProvenanceProviderProtocol,
			Actionable:       true,
			RequiredPerm:     devicetrust.PermTerminalInput,
			DeliveryMaterial: codexDeliveryMaterialFor("7"),
		}},
	}) {
		t.Fatalf("actionable re-offer of an existing record must not be admitted")
	}
	got := store.ListSafe(sid)
	if len(got) != 1 || got[0].Actionable || len(got[0].Options) != 0 {
		t.Fatalf("record must remain non-actionable: %+v", got)
	}
	claim := store.ClaimForExecution(ClaimRequest{
		SessionID: sid, ApprovalID: "codexas-1-7", OptionID: "allow_once",
		Runtime: boundManagedRT(), Requester: completeRequester(), IdempotencyKey: "k1",
	})
	if claim.Outcome != ClaimNotActionable {
		t.Fatalf("old record must stay unclaimable: %s", claim.Outcome)
	}
}

// TestActivation_RuntimeOfLifecycle: RuntimeOf resolves only a live,
// installed, current-epoch runtime.
func TestActivation_RuntimeOfLifecycle(t *testing.T) {
	s, _, runtimeOf := newActivatedDeliverySession(t, false)
	ref, ok := runtimeOf(s.id)
	if !ok || !ref.equal(RuntimeRef{Adapter: codexAppServerAdapter, Version: certifiedCodexAuthorityVersion, LaunchGen: 1, StreamGen: 0}) {
		t.Fatalf("live runtime must resolve exactly: %+v ok=%v", ref, ok)
	}
	if _, ok := runtimeOf("codex_app_server:unknown"); ok {
		t.Fatalf("unknown session must not resolve")
	}
	if err := s.managed.Kill(s.id, 1); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if _, ok := runtimeOf(s.id); ok {
		t.Fatalf("exited runtime must not resolve")
	}
	if err := s.managed.Delete(s.id, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := runtimeOf(s.id); ok {
		t.Fatalf("deleted session must not resolve")
	}
}

// TestActivation_KillDeactivatesActions: after kill, an actionable record is
// invalidated and the authenticated action fails closed with zero further
// provider writes; after delete the record is gone.
func TestActivation_KillDeactivatesActions(t *testing.T) {
	s, cd, runtimeOf := newActivatedDeliverySession(t, false)
	h := activatedHandlers(s, cd, runtimeOf)
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	bearer := ownerBearer(t, m, "phone-1")

	s.inject(t, certifiedApprovalRaw("7", apprTestTurn, "item-4"))
	s.sync(t)
	waitForSafeApprovals(t, s.store, s.id, 1)
	if err := s.managed.Kill(s.id, 1); err != nil {
		t.Fatalf("kill: %v", err)
	}
	got := waitForSafeApprovals(t, s.store, s.id, 1)
	if got[0].State != string(ApprovalInvalidated) {
		t.Fatalf("kill must invalidate the record: %+v", got)
	}
	rr := routeApproval(m, h, bearer, s.id, "codexas-1-7", `{"action":"allow_once","idempotencyKey":"k1"}`)
	if rr.Code == http.StatusOK {
		t.Fatalf("action after kill must fail closed: %s", rr.Body.String())
	}
	if s.written.count() != 0 {
		t.Fatalf("no provider write may happen after kill")
	}
	if err := s.managed.Delete(s.id, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	rr = routeApproval(m, h, bearer, s.id, "codexas-1-7", `{"action":"allow_once","idempotencyKey":"k1"}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("action after delete must be not found: %d %s", rr.Code, rr.Body.String())
	}
}

// TestActivation_DispatcherRoutesByAdapter: managed bindings reach the
// certified boundary; everything else falls back to the capacity-0 gate
// (unavailable, zero writes).
func TestActivation_DispatcherRoutesByAdapter(t *testing.T) {
	s, cd, _ := newActivatedDeliverySession(t, false)
	dispatcher := NewDispatchingApprovalDelivery(cd, NewGatedApprovalDelivery(NewRuntimeDeliveryGate()))

	// Non-managed adapter → gate → unavailable (capacity 0), zero writes.
	nonManaged := ApprovalDeliveryRequest{
		ClaimToken: "11111111111111111111111111111111",
		Binding: ApprovalExecutionBinding{
			ApprovalID: "a1", SessionID: "codex:s1",
			Runtime:      RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 1, StreamGen: 0},
			ActionDigest: payloadDigest(nil), PayloadDigest: payloadDigest([]byte("x")), IdempotencyKey: "k1",
			OptionID: "allow_once",
		},
		Payload: []byte("x"),
	}
	if r := dispatcher.Deliver(nonManaged); r.Outcome != DeliveryUnavailable {
		t.Fatalf("non-managed binding must hit the capacity-0 gate: %+v", r)
	}
	// Managed adapter → certified boundary (proven by its strict rejection of
	// a semantically invalid payload — the gate would answer unavailable).
	managedReq := nonManaged
	managedReq.Binding.Runtime.Adapter = codexAppServerAdapter
	if r := dispatcher.Deliver(managedReq); r.Outcome != DeliveryRejected {
		t.Fatalf("managed binding must reach the certified boundary: %+v", r)
	}
	if s.written.count() != 0 {
		t.Fatalf("no dispatcher probe may reach the wire")
	}
}
