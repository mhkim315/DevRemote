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

// A1 remediation-2 handler tests: strict decode → actionability → server-derived
// requester → atomic claim → runtime revalidation → bound delivery/receipt → commit.

// fixtureDelivery echoes the request's claim token + binding and returns a receipt
// ID for accepted outcomes. It proves the receipt/commit contract; it is never a
// production positive path.
type fixtureDelivery struct {
	outcome DeliveryOutcome
	calls   int32
}

func (f *fixtureDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	atomic.AddInt32(&f.calls, 1)
	rid := ""
	if f.outcome == DeliveryAccepted || f.outcome == DeliveryAlreadyAccepted {
		rid = newReceiptID()
	}
	return DeliveryReceipt{Outcome: f.outcome, ClaimToken: req.ClaimToken, Binding: req.Binding, ReceiptID: rid}
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

func handlerRT() RuntimeRef {
	return RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 5, StreamGen: 2}
}

func actionableHandler(store *AuthoritativeApprovalStore, delivery ApprovalDelivery) *Handlers {
	return &Handlers{Approvals: store, ApprovalDelivery: delivery, RuntimeOf: func(string) (RuntimeRef, bool) { return handlerRT(), true }}
}

func seedActionableForHandler(s *AuthoritativeApprovalStore) {
	rt := handlerRT()
	seedActionable(s, "codex:s1", "a1", rt.LaunchGen, rt.StreamGen, rt.Adapter, rt.Version,
		[]agent.InteractionOption{actOpt("approve", "approve", nil), actOpt("reject", "reject", nil)})
}

func TestHandler_NonActionableRejectedNoDelivery(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	s.Ingest(ApprovalIngest{SessionID: "codex:s1", LaunchGen: 5, StreamGen: 2, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{Approval: agent.AgentApproval{ID: "a1", SessionID: "codex:s1", Kind: "approval"}, Provenance: contract.ProvenanceNativeLog, Actionable: false, RequiredPerm: "terminal:input"}}})
	fd := &fixtureDelivery{outcome: DeliveryAccepted}
	h := actionableHandler(s, fd)
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	rr := routeApproval(m, h, ownerBearer(t, m, "d1"), "codex:s1", "a1", `{"action":"approve","idempotencyKey":"k1"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("non-actionable code=%d want 409 (%s)", rr.Code, rr.Body.String())
	}
	if atomic.LoadInt32(&fd.calls) != 0 {
		t.Error("non-actionable must not reach delivery")
	}
}

func TestHandler_ActionableAcceptedCommits(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionableForHandler(s)
	h := actionableHandler(s, &fixtureDelivery{outcome: DeliveryAccepted})
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	rr := routeApproval(m, h, ownerBearer(t, m, "d1"), "codex:s1", "a1", `{"action":"reject","idempotencyKey":"k1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("accepted code=%d want 200 (%s)", rr.Code, rr.Body.String())
	}
	if snap, _ := s.LookupRecord("codex:s1", "a1"); snap.State != ApprovalRejected {
		t.Errorf("state=%q want rejected", snap.State)
	}
}

func TestHandler_UnavailableFailsClosed(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionableForHandler(s)
	h := actionableHandler(s, &fixtureDelivery{outcome: DeliveryUnavailable})
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	rr := routeApproval(m, h, ownerBearer(t, m, "d1"), "codex:s1", "a1", `{"action":"approve","idempotencyKey":"k1"}`)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("unavailable code=%d want 502 (%s)", rr.Code, rr.Body.String())
	}
	if snap, _ := s.LookupRecord("codex:s1", "a1"); snap.State != ApprovalDeliveryFailed {
		t.Errorf("state=%q want delivery_failed", snap.State)
	}
}

func TestHandler_RuntimeReplacedBeforeDelivery(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionableForHandler(s)
	fd := &fixtureDelivery{outcome: DeliveryAccepted}
	var n int32
	h := &Handlers{Approvals: s, ApprovalDelivery: fd, RuntimeOf: func(string) (RuntimeRef, bool) {
		if atomic.AddInt32(&n, 1) == 1 {
			return handlerRT(), true
		}
		return RuntimeRef{Adapter: "codex", Version: "0.144.1", LaunchGen: 6, StreamGen: 2}, true
	}}
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	rr := routeApproval(m, h, ownerBearer(t, m, "d1"), "codex:s1", "a1", `{"action":"approve","idempotencyKey":"k1"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("replaced runtime code=%d want 409 (%s)", rr.Code, rr.Body.String())
	}
	if atomic.LoadInt32(&fd.calls) != 0 {
		t.Error("stale runtime must not reach delivery")
	}
	if snap, _ := s.LookupRecord("codex:s1", "a1"); snap.State != ApprovalDeliveryFailed {
		t.Errorf("state=%q want delivery_failed", snap.State)
	}
}

func TestHandler_IdempotentReplayNoDuplicateDelivery(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionableForHandler(s)
	fd := &fixtureDelivery{outcome: DeliveryAccepted}
	h := actionableHandler(s, fd)
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	bearer := ownerBearer(t, m, "d1")
	if rr := routeApproval(m, h, bearer, "codex:s1", "a1", `{"action":"reject","idempotencyKey":"k1"}`); rr.Code != http.StatusOK {
		t.Fatalf("first code=%d", rr.Code)
	}
	rr := routeApproval(m, h, bearer, "codex:s1", "a1", `{"action":"reject","idempotencyKey":"k1"}`)
	if rr.Code != http.StatusOK {
		t.Errorf("replay code=%d want 200", rr.Code)
	}
	if atomic.LoadInt32(&fd.calls) != 1 {
		t.Errorf("delivery called %d times; replay must not re-deliver", fd.calls)
	}
}

func TestHandler_StrictDecodeAndAuth(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionableForHandler(s)
	h := actionableHandler(s, &fixtureDelivery{outcome: DeliveryAccepted})
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	owner := ownerBearer(t, m, "d1")

	// client-supplied identity field rejected
	if rr := routeApproval(m, h, owner, "codex:s1", "a1", `{"action":"approve","deviceId":"evil","idempotencyKey":"k1"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("client identity field code=%d want 400", rr.Code)
	}
	// missing idempotency key rejected by the store as invalid_key → 400
	if rr := routeApproval(m, h, owner, "codex:s1", "a1", `{"action":"approve"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("missing key code=%d want 400", rr.Code)
	}
	// unknown action
	if rr := routeApproval(m, h, owner, "codex:s1", "a1", `{"action":"ghost","idempotencyKey":"k1"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("unknown action code=%d want 400", rr.Code)
	}
	// not found
	if rr := routeApproval(m, h, owner, "codex:s1", "missing", `{"action":"approve","idempotencyKey":"k1"}`); rr.Code != http.StatusNotFound {
		t.Errorf("missing code=%d want 404", rr.Code)
	}
	// no bearer → 401; member → 403
	if rr := routeApproval(m, h, "", "codex:s1", "a1", `{"action":"approve","idempotencyKey":"k1"}`); rr.Code != http.StatusUnauthorized {
		t.Errorf("no bearer code=%d want 401", rr.Code)
	}
	memRaw, _, _, _ := m.CreateAfterVerifiedChallenge("d2", "host", "boot", devicetrust.PermissionsForRole(devicetrust.RoleMember))
	if rr := routeApproval(m, h, memRaw, "codex:s1", "a1", `{"action":"approve","idempotencyKey":"k1"}`); rr.Code != http.StatusForbidden {
		t.Errorf("member code=%d want 403", rr.Code)
	}
}

func TestHandler_NoPrincipalForbidden(t *testing.T) {
	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionableForHandler(s)
	h := actionableHandler(s, &fixtureDelivery{outcome: DeliveryAccepted})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/sessions/{id}/approvals/{approvalId}", h.HandleApprovalAction)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/codex:s1/approvals/a1", strings.NewReader(`{"action":"approve","idempotencyKey":"k1"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("no-principal code=%d want 403", rr.Code)
	}
}

// R2-D: log-safety — control/newline/path/token-like identifiers are sanitized.
func TestSanitizeLogID(t *testing.T) {
	cases := map[string]string{
		"clean:id-1": "clean:id-1",
		"a\nb":       "a.b",
		"a\rb\tc":    "a.b.c",
		"x\x00y":     "x.y",
		"tok\x1besc": "tok.esc",
	}
	for in, want := range cases {
		if got := sanitizeLogID(in); got != want {
			t.Errorf("sanitizeLogID(%q)=%q want %q", in, got, want)
		}
	}
	// bounded
	if got := sanitizeLogID(strings.Repeat("a", 500)); len(got) != maxLogIDLen {
		t.Errorf("sanitizeLogID length=%d want %d", len(got), maxLogIDLen)
	}
}
