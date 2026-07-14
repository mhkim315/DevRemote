package term

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/devicetrust"
)

// A1 remediation handler tests (B3-B5, B7 server side): the action route drives
// strict decode → actionability gate → server-derived requester → atomic claim →
// runtime revalidation → dedicated delivery boundary → receipt-gated commit, all
// behind the paired-device PermTerminalInput route. There is no insecure-local
// bypass and no client-supplied identity authority.

// fixtureDelivery is a controlled delivery boundary that returns a configured
// receipt outcome. It proves the receipt/commit contract; it is NEVER used to claim
// a production provider path.
type fixtureDelivery struct {
	outcome DeliveryOutcome
	calls   int32
}

func (f *fixtureDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	atomic.AddInt32(&f.calls, 1)
	return DeliveryReceipt{
		Outcome: f.outcome, ApprovalID: req.ApprovalID, SessionID: req.SessionID,
		ActionDigest: req.ActionDigest, IdempotencyKey: req.IdempotencyKey,
	}
}

func ownerBearer(t *testing.T, m *devicetrust.DeviceSessionManager, device string) string {
	t.Helper()
	raw, _, _, err := m.CreateAfterVerifiedChallenge(device, "host", "boot", devicetrust.PermissionsForRole(devicetrust.RoleOwner))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func routeApproval(m *devicetrust.DeviceSessionManager, h *Handlers, bearer, sessionID, approvalID, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/sessions/{id}/approvals/{approvalId}",
		devicetrust.RequirePrincipal(m, h.HandleApprovalAction, devicetrust.PermTerminalInput))
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/approvals/"+approvalID, strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func actionableHandler(store *AuthoritativeApprovalStore, delivery ApprovalDelivery, runtime RuntimeRef) *Handlers {
	return &Handlers{
		Approvals:        store,
		ApprovalDelivery: delivery,
		RuntimeOf:        func(string) (RuntimeRef, bool) { return runtime, true },
	}
}

const codexRT = "codex"

func seedActionableForHandler(s *AuthoritativeApprovalStore) RuntimeRef {
	rt := RuntimeRef{Adapter: codexRT, Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
	seedActionable(s, "codex:s1", "a1", rt.LaunchGen, rt.StreamGen, rt.Adapter, rt.Version,
		[]agent.InteractionOption{actOpt("approve", "approve", nil), actOpt("reject", "reject", nil)})
	return rt
}

func TestHandler_NonActionableRejectedNoDelivery(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	// non-actionable ingest (production default)
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 5, StreamGen: 2, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{Approval: agent.AgentApproval{ID: "a1", SessionID: "codex:s1", Kind: "approval"}, Provenance: contract.ProvenanceNativeLog, Actionable: false, RequiredPerm: "terminal:input"}}})
	fd := &fixtureDelivery{outcome: DeliveryAccepted}
	h := actionableHandler(s, fd, RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2})
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	rr := routeApproval(m, h, ownerBearer(t, m, "d1"), "codex:s1", "a1", `{"action":"approve"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("non-actionable code=%d want 409 (%s)", rr.Code, rr.Body.String())
	}
	if atomic.LoadInt32(&fd.calls) != 0 {
		t.Error("non-actionable approval must not reach the delivery boundary")
	}
}

func TestHandler_ActionableAcceptedCommits(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionableForHandler(s)
	fd := &fixtureDelivery{outcome: DeliveryAccepted}
	h := actionableHandler(s, fd, RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2})
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	rr := routeApproval(m, h, ownerBearer(t, m, "d1"), "codex:s1", "a1", `{"action":"reject","idempotencyKey":"k1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("accepted code=%d want 200 (%s)", rr.Code, rr.Body.String())
	}
	snap, _ := s.LookupRecord("codex:s1", "a1")
	if snap.State != ApprovalRejected {
		t.Errorf("state=%q want rejected", snap.State)
	}
}

func TestHandler_UnavailableDeliveryFailsClosed(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionableForHandler(s)
	fd := &fixtureDelivery{outcome: DeliveryUnavailable}
	h := actionableHandler(s, fd, RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2})
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	rr := routeApproval(m, h, ownerBearer(t, m, "d1"), "codex:s1", "a1", `{"action":"approve"}`)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("unavailable code=%d want 502 (%s)", rr.Code, rr.Body.String())
	}
	if snap, _ := s.LookupRecord("codex:s1", "a1"); snap.State != ApprovalDeliveryFailed {
		t.Errorf("state=%q want delivery_failed", snap.State)
	}
}

func TestHandler_RuntimeReplacedBetweenClaimAndDelivery(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionableForHandler(s)
	fd := &fixtureDelivery{outcome: DeliveryAccepted}
	// RuntimeOf returns the bound runtime on the claim call, a REPLACED runtime on the
	// pre-delivery revalidation call.
	var n int32
	h := &Handlers{Approvals: s, ApprovalDelivery: fd, RuntimeOf: func(string) (RuntimeRef, bool) {
		if atomic.AddInt32(&n, 1) == 1 {
			return RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}, true
		}
		return RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 6, StreamGen: 2}, true
	}}
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	rr := routeApproval(m, h, ownerBearer(t, m, "d1"), "codex:s1", "a1", `{"action":"approve"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("replaced runtime code=%d want 409 (%s)", rr.Code, rr.Body.String())
	}
	if atomic.LoadInt32(&fd.calls) != 0 {
		t.Error("stale runtime must not reach the delivery boundary")
	}
	if snap, _ := s.LookupRecord("codex:s1", "a1"); snap.State != ApprovalDeliveryFailed {
		t.Errorf("state=%q want delivery_failed", snap.State)
	}
}

func TestHandler_IdempotentReplayNoDuplicateDelivery(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionableForHandler(s)
	fd := &fixtureDelivery{outcome: DeliveryAccepted}
	h := actionableHandler(s, fd, RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2})
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	bearer := ownerBearer(t, m, "d1")
	if rr := routeApproval(m, h, bearer, "codex:s1", "a1", `{"action":"reject","idempotencyKey":"k1"}`); rr.Code != http.StatusOK {
		t.Fatalf("first code=%d", rr.Code)
	}
	// replay same key → already_accepted, delivery NOT called a second time
	rr := routeApproval(m, h, bearer, "codex:s1", "a1", `{"action":"reject","idempotencyKey":"k1"}`)
	if rr.Code != http.StatusOK {
		t.Errorf("replay code=%d want 200", rr.Code)
	}
	if atomic.LoadInt32(&fd.calls) != 1 {
		t.Errorf("delivery called %d times; replay must not re-deliver", fd.calls)
	}
}

func TestHandler_StrictDecodeAndAuthGuards(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionableForHandler(s)
	fd := &fixtureDelivery{outcome: DeliveryAccepted}
	h := actionableHandler(s, fd, RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2})
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	owner := ownerBearer(t, m, "d1")

	// unknown / client-supplied identity field rejected
	if rr := routeApproval(m, h, owner, "codex:s1", "a1", `{"action":"approve","deviceId":"evil"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("client identity field code=%d want 400", rr.Code)
	}
	// unknown action
	if rr := routeApproval(m, h, owner, "codex:s1", "a1", `{"action":"ghost"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("unknown action code=%d want 400", rr.Code)
	}
	// not found
	if rr := routeApproval(m, h, owner, "codex:s1", "missing", `{"action":"approve"}`); rr.Code != http.StatusNotFound {
		t.Errorf("missing code=%d want 404", rr.Code)
	}
	// missing bearer → 401 (route auth)
	if rr := routeApproval(m, h, "", "codex:s1", "a1", `{"action":"approve"}`); rr.Code != http.StatusUnauthorized {
		t.Errorf("no bearer code=%d want 401", rr.Code)
	}
	// member device lacks PermTerminalInput → 403
	memRaw, _, _, _ := m.CreateAfterVerifiedChallenge("d2", "host", "boot", devicetrust.PermissionsForRole(devicetrust.RoleMember))
	if rr := routeApproval(m, h, memRaw, "codex:s1", "a1", `{"action":"approve"}`); rr.Code != http.StatusForbidden {
		t.Errorf("member code=%d want 403", rr.Code)
	}
}

func TestHandler_NoPrincipalForbidden(t *testing.T) {
	// Reaching the handler without any device principal (e.g. a misconfigured local
	// route) must fail closed — approvals are always authoritative.
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionableForHandler(s)
	h := actionableHandler(s, &fixtureDelivery{outcome: DeliveryAccepted}, RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/sessions/{id}/approvals/{approvalId}", h.HandleApprovalAction) // no RequirePrincipal
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/codex:s1/approvals/a1", strings.NewReader(`{"action":"approve"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("no-principal code=%d want 403", rr.Code)
	}
}
