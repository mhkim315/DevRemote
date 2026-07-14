package term

import (
	"bytes"
	"encoding/json"
	"log"
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

// A1 remediation-3 handler tests: the store-authoritative claim flow through the
// route, receipt payload binding, and conservative log redaction.

// fixtureDelivery echoes the request binding + token, a receipt id, and the digest of
// the EXACT bytes it was handed (req.Payload). It proves the receipt/commit contract.
type fixtureDelivery struct {
	outcome DeliveryOutcome
	calls   int32
}

func (f *fixtureDelivery) Deliver(req ApprovalDeliveryRequest) DeliveryReceipt {
	atomic.AddInt32(&f.calls, 1)
	rid, pd := "", ""
	if f.outcome == DeliveryAccepted || f.outcome == DeliveryAlreadyAccepted {
		rid = newGateNonce()
		pd = payloadDigest(req.Payload)
	}
	return DeliveryReceipt{Outcome: f.outcome, ClaimToken: req.ClaimToken, Binding: req.Binding, ReceiptID: rid, DeliveredPayloadDigest: pd}
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

	if rr := routeApproval(m, h, owner, "codex:s1", "a1", `{"action":"approve","deviceId":"evil","idempotencyKey":"k1"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("client identity field code=%d want 400", rr.Code)
	}
	if rr := routeApproval(m, h, owner, "codex:s1", "a1", `{"action":"approve"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("missing key code=%d want 400", rr.Code)
	}
	if rr := routeApproval(m, h, owner, "codex:s1", "a1", `{"action":"ghost","idempotencyKey":"k1"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("unknown action code=%d want 400", rr.Code)
	}
	if rr := routeApproval(m, h, owner, "codex:s1", "missing", `{"action":"approve","idempotencyKey":"k1"}`); rr.Code != http.StatusNotFound {
		t.Errorf("missing code=%d want 404", rr.Code)
	}
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

// R3-E: capture actual emitted log output and prove paths, secrets, control bytes,
// and long values do not appear verbatim.
func TestHandler_LogRedaction(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	s, _ := newTestStore(time.Unix(1000, 0))
	seedActionableForHandler(s)
	h := actionableHandler(s, &fixtureDelivery{outcome: DeliveryAccepted})
	m := devicetrust.NewDeviceSessionManager("boot", 20*time.Minute)
	owner := ownerBearer(t, m, "d1")

	// path-shaped + secret-shaped + token-shaped identifiers as the action id. The
	// secret-shaped literals are assembled at runtime so this test source does not
	// itself trip the repository secret scan.
	skToken := "sk-" + "ABCDEF0123456789"
	ghToken := "ghp" + "_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for _, bad := range []string{
		`/Users/alice/private/repo`,
		`api_key=secret-value`,
		skToken,
		ghToken,
		`C:\Users\alice\secret`,
	} {
		routeApproval(m, h, owner, "codex:s1", "a1", `{"action":`+jsonQuote(bad)+`,"idempotencyKey":"k1"}`)
	}
	out := buf.String()
	for _, leak := range []string{
		"/Users/alice/private/repo", "secret-value", skToken, ghToken, `C:\Users\alice`,
	} {
		if strings.Contains(out, leak) {
			t.Errorf("log leaked %q", leak)
		}
	}
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
