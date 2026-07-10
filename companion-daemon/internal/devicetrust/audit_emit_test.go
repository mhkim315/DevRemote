package devicetrust

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// captureAudit is an in-memory AuditLog for asserting emit points.
type captureAudit struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (c *captureAudit) Record(ev AuditEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *captureAudit) List(limit int) []AuditEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]AuditEvent(nil), c.events...)
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func (c *captureAudit) find(action, result string) *AuditEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.events {
		if c.events[i].Action == action && c.events[i].Result == result {
			return &c.events[i]
		}
	}
	return nil
}

func TestAuditEmit_VerifyGrant(t *testing.T) {
	h, devPriv, deviceID := setupAuthHandler(t)
	cap := &captureAudit{}
	h.Audit = cap
	doVerify(t, h, deviceID, devPriv)

	ev := cap.find(ActionAuthVerify, ResultGranted)
	if ev == nil {
		t.Fatal("no auth.verify/granted event recorded")
	}
	if ev.DeviceID != deviceID {
		t.Fatalf("deviceId=%q want %q", ev.DeviceID, deviceID)
	}
	if ev.CorrelationID == "" {
		t.Fatal("granted event missing correlationId (bearer session id)")
	}
}

func TestAuditEmit_VerifyDenyBadSignature(t *testing.T) {
	h, _, deviceID := setupAuthHandler(t)
	cap := &captureAudit{}
	h.Audit = cap

	clientNonce := make([]byte, 32)
	rand.Read(clientNonce)
	chalReq, _ := json.Marshal(ChallengeRequest{Version: 1, HostID: h.Identity.HostID, DeviceID: deviceID, ClientNonce: hex.EncodeToString(clientNonce)})
	rr := httptest.NewRecorder()
	h.HandleChallenge(rr, httptest.NewRequest("POST", "/challenge", bytes.NewReader(chalReq)))
	var chalResp AuthChallengeResponse
	json.Unmarshal(rr.Body.Bytes(), &chalResp)

	badSig := make([]byte, 70)
	rand.Read(badSig)
	verifyReq, _ := json.Marshal(VerifyRequest{Version: 1, ChallengeID: chalResp.ChallengeID, DeviceID: deviceID, Signature: hex.EncodeToString(badSig)})
	rr2 := httptest.NewRecorder()
	h.HandleVerify(rr2, httptest.NewRequest("POST", "/verify", bytes.NewReader(verifyReq)))
	if rr2.Code != http.StatusUnauthorized {
		t.Fatalf("verify code=%d want 401", rr2.Code)
	}
	if ev := cap.find(ActionAuthVerify, ResultDenied); ev == nil || ev.DeviceID != deviceID {
		t.Fatalf("no auth.verify/denied event for device: %+v", ev)
	}
}

func TestAuditEmit_WSTicketDenyOnly(t *testing.T) {
	cap := &captureAudit{}
	s := NewWSTicketStoreWithConfig(WSTicketStoreConfig{TTL: time.Minute, MaxPerDevice: 1, MaxTotal: 1})
	h := HandleWSTicket(s, cap)
	p := &Principal{
		DeviceID: "device", HostID: "host", BearerSessionID: "bearer",
		BearerExpires: time.Now().Add(time.Minute), Permissions: []string{PermSessionsRead},
	}
	issue := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/device-auth/ws-ticket?session=s", nil)
		req = req.WithContext(context.WithValue(req.Context(), principalKey{}, p))
		rr := httptest.NewRecorder()
		h(rr, req)
		return rr.Code
	}
	if code := issue(); code != http.StatusOK {
		t.Fatalf("first issue: %d want 200", code)
	}
	if code := issue(); code != http.StatusTooManyRequests {
		t.Fatalf("second issue: %d want 429", code)
	}
	// Grants are not audited; only the denial is.
	ev := cap.find(ActionWSTicketDeny, ResultDenied)
	if ev == nil {
		t.Fatal("no ws.ticket.deny event recorded")
	}
	if ev.DeviceID != "device" || ev.CorrelationID != "bearer" {
		t.Fatalf("ws.ticket.deny event fields: %+v", *ev)
	}
	if ev.SessionID != "" {
		t.Fatal("ws.ticket.deny must not record the untrusted URL session query")
	}
	if len(cap.events) != 1 {
		t.Fatalf("expected exactly one audit event (deny only), got %d", len(cap.events))
	}
}
