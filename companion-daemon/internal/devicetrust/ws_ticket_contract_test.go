package devicetrust

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTicketPrincipal(t *testing.T, m *DeviceSessionManager, deviceID, hostID string, perms []string) (*Principal, string) {
	t.Helper()
	raw, _, _, err := m.CreateAfterVerifiedChallenge(deviceID, hostID, m.BootID(), perms, 0)
	if err != nil {
		t.Fatalf("create bearer: %v", err)
	}
	p := m.AuthenticateBearer(raw)
	if p == nil {
		t.Fatal("authenticate bearer returned nil")
	}
	return p, raw
}

func TestWSTicket_ExactExpiryAndDefensivePermissions(t *testing.T) {
	m := NewPermissiveSessionManager("boot", time.Minute)
	p, _ := newTicketPrincipal(t, m, "device", "host", []string{PermSessionsRead})
	p.BearerExpires = time.Now().UTC().Add(20 * time.Millisecond)
	s := NewWSTicketStoreWithConfig(testMutationAuthorizer{}, WSTicketStoreConfig{TTL: time.Minute, MaxPerDevice: 3, MaxTotal: 3})
	raw, expiresAt, err := s.Issue(p, "host", "session")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !expiresAt.Equal(p.BearerExpires) {
		t.Fatalf("expiry=%s want bearer expiry=%s", expiresAt, p.BearerExpires)
	}
	p.Permissions[0] = "mutated"
	got := s.ConsumeBound(raw, "host", "session", m)
	if got == nil || len(got.Permissions) != 1 || got.Permissions[0] != PermSessionsRead {
		t.Fatalf("permission snapshot mutated: %+v", got)
	}
}

func TestWSTicket_ExpiredAuthorizationIsNotIssued(t *testing.T) {
	s := NewWSTicketStore(testMutationAuthorizer{})
	p := &Principal{
		DeviceID: "device", HostID: "host", BearerSessionID: "bearer",
		BearerExpires: time.Now().UTC().Add(-time.Second), Permissions: []string{PermSessionsRead},
	}
	if _, _, err := s.Issue(p, "host", "session"); !errors.Is(err, ErrWSTicketAuthorizationExpired) {
		t.Fatalf("issue error=%v want ErrWSTicketAuthorizationExpired", err)
	}
	if s.Count() != 0 {
		t.Fatalf("expired grant consumed capacity: %d", s.Count())
	}
}

func TestHandleWSTicket_ReturnsStoredEffectiveExpiry(t *testing.T) {
	s := NewWSTicketStoreWithConfig(testMutationAuthorizer{}, WSTicketStoreConfig{TTL: time.Minute, MaxPerDevice: 2, MaxTotal: 2})
	expiresAt := time.Now().UTC().Add(10 * time.Second).Truncate(time.Microsecond)
	p := &Principal{
		DeviceID: "device", HostID: "host", BearerSessionID: "bearer",
		BearerExpires: expiresAt, Permissions: []string{PermSessionsRead},
	}
	h := HandleWSTicket(s, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/device-auth/ws-ticket?session=session", nil)
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, p))
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var response struct {
		Ticket    string    `json:"ticket"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.Ticket == "" || !response.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("response=%+v want expiry=%s", response, expiresAt)
	}
}

func TestWSTicket_WrongBindingConsumesTicket(t *testing.T) {
	m := NewPermissiveSessionManager("boot", time.Minute)
	p, _ := newTicketPrincipal(t, m, "device", "host", []string{PermSessionsRead})
	s := NewWSTicketStore(testMutationAuthorizer{})
	raw, _, err := s.Issue(p, "host", "session")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if got := s.ConsumeBound(raw, "wrong-host", "session", m); got != nil {
		t.Fatal("wrong-host ticket accepted")
	}
	if got := s.ConsumeBound(raw, "host", "session", m); got != nil {
		t.Fatal("wrong-binding ticket was reusable")
	}
}

func TestWSTicket_BearerReplacementAndRevokeInvalidate(t *testing.T) {
	m := NewPermissiveSessionManager("boot", time.Minute)
	s := NewWSTicketStore(testMutationAuthorizer{})
	p1, _ := newTicketPrincipal(t, m, "device", "host", []string{PermSessionsRead})
	raw1, _, _ := s.Issue(p1, "host", "session")
	_, _, _, err := m.CreateAfterVerifiedChallenge("device", "host", "boot", []string{PermSessionsRead}, 0)
	if err != nil {
		t.Fatalf("replace bearer: %v", err)
	}
	if got := s.ConsumeBound(raw1, "host", "session", m); got != nil {
		t.Fatal("ticket authorized by replaced bearer was accepted")
	}

	p2 := m.AuthenticateBearer(func() string {
		raw, _, _, _ := m.CreateAfterVerifiedChallenge("other", "host", "boot", []string{PermSessionsRead}, 0)
		return raw
	}())
	raw2, _, _ := s.Issue(p2, "host", "session")
	m.RevokeDevice("other")
	if got := s.ConsumeBound(raw2, "host", "session", m); got != nil {
		t.Fatal("ticket authorized by revoked bearer was accepted")
	}
}

func TestWSTicket_CapacityIsAtomicAndExpiredTicketsReleaseIt(t *testing.T) {
	s := NewWSTicketStoreWithConfig(testMutationAuthorizer{}, WSTicketStoreConfig{
		TTL: time.Minute, MaxPerDevice: 3, MaxTotal: 3,
	})
	p := &Principal{
		DeviceID: "device", HostID: "host", BearerSessionID: "bearer",
		BearerExpires: time.Now().UTC().Add(time.Minute), Permissions: []string{PermSessionsRead},
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := s.Issue(p, "host", "session"); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrWSTicketCapacity) {
				t.Errorf("unexpected issue error: %v", err)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 3 || s.Count() != 3 {
		t.Fatalf("successes=%d count=%d want 3/3", successes.Load(), s.Count())
	}

	expiring := NewWSTicketStoreWithConfig(testMutationAuthorizer{}, WSTicketStoreConfig{
		TTL: time.Millisecond, MaxPerDevice: 1, MaxTotal: 1,
	})
	if _, _, err := expiring.Issue(p, "host", "session"); err != nil {
		t.Fatalf("first issue: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, _, err := expiring.Issue(p, "host", "session"); err != nil {
		t.Fatalf("expired ticket did not release capacity: %v", err)
	}
	if expiring.Count() != 1 {
		t.Fatalf("expired cleanup count=%d want 1", expiring.Count())
	}
}

func TestWSTicket_GlobalCapacityAcrossDevices(t *testing.T) {
	s := NewWSTicketStoreWithConfig(testMutationAuthorizer{}, WSTicketStoreConfig{
		TTL: time.Minute, MaxPerDevice: 3, MaxTotal: 2,
	})
	principal := func(deviceID string) *Principal {
		return &Principal{
			DeviceID: deviceID, HostID: "host", BearerSessionID: "bearer-" + deviceID,
			BearerExpires: time.Now().UTC().Add(time.Minute), Permissions: []string{PermSessionsRead},
		}
	}
	if _, _, err := s.Issue(principal("d1"), "host", "session"); err != nil {
		t.Fatalf("d1: %v", err)
	}
	if _, _, err := s.Issue(principal("d2"), "host", "session"); err != nil {
		t.Fatalf("d2: %v", err)
	}
	if _, _, err := s.Issue(principal("d3"), "host", "session"); !errors.Is(err, ErrWSTicketCapacity) {
		t.Fatalf("global cap error=%v want ErrWSTicketCapacity", err)
	}
}

func TestDeviceSessionExpiry_AllRemovalPathsNotify(t *testing.T) {
	tests := []struct {
		name string
		run  func(*DeviceSessionManager, string)
	}{
		{
			name: "authenticate",
			run: func(m *DeviceSessionManager, raw string) {
				_ = m.AuthenticateBearer(raw)
			},
		},
		{
			name: "inline-create-purge",
			run: func(m *DeviceSessionManager, _ string) {
				_, _, _, _ = m.CreateAfterVerifiedChallenge("second", "host", m.BootID(), []string{PermSessionsRead}, 0)
			},
		},
		{
			name: "periodic-purge-path",
			run: func(m *DeviceSessionManager, _ string) {
				m.PurgeExpired(time.Now().UTC())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewDeviceSessionManagerWithConfig(DeviceSessionManagerConfig{
				BootID: "boot", Lifetime: time.Millisecond, MaxSessions: 2,
			})
			m.GetAuth = func(deviceID string) AuthorizationState { return AuthorizationState{Epoch: 0, Active: true} }
			var mu sync.Mutex
			var invalidated []string
			m.SetOnRevoke(func(deviceID string) {
				mu.Lock()
				invalidated = append(invalidated, deviceID)
				mu.Unlock()
			})
			raw, _, _, err := m.CreateAfterVerifiedChallenge("device", "host", "boot", []string{PermSessionsRead}, 0)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			time.Sleep(10 * time.Millisecond)
			tt.run(m, raw)
			mu.Lock()
			defer mu.Unlock()
			if len(invalidated) != 1 || invalidated[0] != "device" {
				t.Fatalf("invalidated=%v want [device]", invalidated)
			}
		})
	}
}
