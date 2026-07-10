package devicetrust

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequirePrincipal_MissingToken(t *testing.T) {
	m := NewDeviceSessionManager("b", 20*time.Minute)
	h := RequirePrincipal(m, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}, PermSessionsRead)
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest("GET", "/", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: %d", rr.Code)
	}
}

func TestRequirePrincipal_InvalidToken(t *testing.T) {
	m := NewDeviceSessionManager("b", 20*time.Minute)
	h := RequirePrincipal(m, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}, PermSessionsRead)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer deadbeef")
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token: %d", rr.Code)
	}
}

func TestRequirePrincipal_ValidToken(t *testing.T) {
	m := NewDeviceSessionManager("b", 20*time.Minute)
	raw, _, _, err := m.CreateAfterVerifiedChallenge("d1", "h", "b", PermissionsForRole(RoleOwner))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	h := RequirePrincipal(m, func(w http.ResponseWriter, r *http.Request) {
		p := PrincipalFromContext(r.Context())
		if p == nil {
			t.Fatal("principal nil")
		}
		w.WriteHeader(200)
	}, PermSessionsRead)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != 200 {
		t.Fatalf("valid token: %d", rr.Code)
	}
}

func TestRequirePrincipal_MemberCannotAccessKill(t *testing.T) {
	m := NewDeviceSessionManager("b", 20*time.Minute)
	raw, _, _, _ := m.CreateAfterVerifiedChallenge("d1", "h", "b", PermissionsForRole(RoleMember))
	h := RequirePrincipal(m, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not run")
	}, PermSessionsKill)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("member got kill: %d", rr.Code)
	}
}

func TestRequirePrincipal_ReplacementInvalidatesOld(t *testing.T) {
	m := NewDeviceSessionManager("b", 20*time.Minute)
	tok1, _, _, _ := m.CreateAfterVerifiedChallenge("d1", "h", "b", PermissionsForRole(RoleOwner))
	// Replace.
	tok2, _, _, _ := m.CreateAfterVerifiedChallenge("d1", "h", "b", PermissionsForRole(RoleOwner))
	h := RequirePrincipal(m, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}, PermSessionsRead)
	// tok1 invalid.
	req1 := httptest.NewRequest("GET", "/", nil)
	req1.Header.Set("Authorization", "Bearer "+tok1)
	rr1 := httptest.NewRecorder()
	h(rr1, req1)
	if rr1.Code != http.StatusUnauthorized {
		t.Fatalf("tok1 after replace: %d", rr1.Code)
	}
	// tok2 valid.
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("Authorization", "Bearer "+tok2)
	rr2 := httptest.NewRecorder()
	h(rr2, req2)
	if rr2.Code != 200 {
		t.Fatalf("tok2: %d", rr2.Code)
	}
}

func TestRequirePrincipal_ExpiredTokenFails(t *testing.T) {
	m := NewDeviceSessionManagerWithConfig(DeviceSessionManagerConfig{BootID: "b", Lifetime: 1 * time.Millisecond, MaxSessions: 64})
	raw, _, _, _ := m.CreateAfterVerifiedChallenge("d1", "h", "b", PermissionsForRole(RoleOwner))
	time.Sleep(10 * time.Millisecond)
	h := RequirePrincipal(m, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}, PermSessionsRead)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expired: %d", rr.Code)
	}
}

func TestBearerToken_Extraction(t *testing.T) {
	if tok := bearerToken(httptest.NewRequest("GET", "/", nil)); tok != "" {
		t.Fatalf("empty header: %q", tok)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer xyz")
	if tok := bearerToken(req); tok != "xyz" {
		t.Fatalf("got %q", tok)
	}
	req.Header.Set("Authorization", "Basic xyz")
	if tok := bearerToken(req); tok != "" {
		t.Fatalf("wrong scheme: %q", tok)
	}
}

func TestWSTicketStore_Consume(t *testing.T) {
	s := NewWSTicketStore()
	p := &Principal{DeviceID: "d1", Permissions: []string{PermSessionsRead}}
	raw, _ := s.Issue(p)
	if got := s.Consume(raw); got == nil || got.DeviceID != "d1" {
		t.Fatalf("consume: %+v", got)
	}
	if s.Consume(raw) != nil {
		t.Fatal("replay succeeded")
	}
}

func TestConnRegistry_CloseDevice(t *testing.T) {
	r := NewAuthenticatedConnRegistry()
	closed := make(chan struct{}, 2)
	mk := func() *testCloser { return &testCloser{ch: closed} }
	r.Register("d1", mk())
	r.Register("d1", mk())
	r.Register("d2", mk())
	r.CloseDevice("d1")
	if len(closed) != 2 {
		t.Fatalf("closed=%d want 2", len(closed))
	}
}

type testCloser struct{ ch chan struct{} }

func (c *testCloser) Close() error { c.ch <- struct{}{}; return nil }

func TestPrincipalFromContext_Empty(t *testing.T) {
	if p := PrincipalFromContext(context.Background()); p != nil {
		t.Fatalf("expected nil: %+v", p)
	}
}
