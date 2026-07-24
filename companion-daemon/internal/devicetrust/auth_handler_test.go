package devicetrust

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func setupAuthHandler(t *testing.T) (*AuthHandler, *ecdsa.PrivateKey, string) {
	t.Helper()
	id, _ := LoadOrCreateHostIdentity(&FileKeyStore{Path: t.TempDir() + "/host.json"})
	reg, _ := newReg(t)
	devPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&devPriv.PublicKey)
	d, _ := reg.Add(pubDER, "test-phone")
	bootID, _ := NewBootID()
	return &AuthHandler{
		Identity: id, Registry: reg,
		Challenges:  NewChallengeStore(),
		Sessions:    NewPermissiveSessionManager(bootID, 20*time.Minute),
		RateLimiter: NewChallengeRateLimiter(RateLimiterConfig{Burst: 100, RatePerMin: 1000}),
	}, devPriv, d.DeviceID
}

func doVerify(t *testing.T, h *AuthHandler, deviceID string, devPriv *ecdsa.PrivateKey) string {
	t.Helper()
	clientNonce := make([]byte, 32)
	rand.Read(clientNonce)
	chalReq, _ := json.Marshal(ChallengeRequest{Version: 1, HostID: h.Identity.HostID, DeviceID: deviceID, ClientNonce: hex.EncodeToString(clientNonce)})
	rr := httptest.NewRecorder()
	h.HandleChallenge(rr, httptest.NewRequest("POST", "/challenge", bytes.NewReader(chalReq)))
	if rr.Code != 200 {
		t.Fatalf("challenge: %d", rr.Code)
	}
	var chalResp AuthChallengeResponse
	json.Unmarshal(rr.Body.Bytes(), &chalResp)

	challengeID, _ := hex.DecodeString(chalResp.ChallengeID)
	serverNonce, _ := hex.DecodeString(chalResp.ServerNonce)
	createdAtMS := chalResp.ExpiresAt.Add(-5 * time.Minute).UnixMilli()
	expiresAtMS := chalResp.ExpiresAt.UnixMilli()
	transcript := AuthTranscript{Role: "device", HostID: chalResp.HostID, DeviceID: deviceID,
		DaemonBootID: chalResp.DaemonBootID, ChallengeID: challengeID, ClientNonce: clientNonce,
		ServerNonce: serverNonce, CreatedAtMS: createdAtMS, ExpiresAtMS: expiresAtMS}
	digest := sha256.Sum256(transcript.Build())
	devSig, _ := ecdsa.SignASN1(rand.Reader, devPriv, digest[:])

	verifyReq, _ := json.Marshal(VerifyRequest{Version: 1, ChallengeID: chalResp.ChallengeID, DeviceID: deviceID, Signature: hex.EncodeToString(devSig)})
	rr2 := httptest.NewRecorder()
	h.HandleVerify(rr2, httptest.NewRequest("POST", "/verify", bytes.NewReader(verifyReq)))
	if rr2.Code != 200 {
		t.Fatalf("verify: %d %s", rr2.Code, rr2.Body.String())
	}
	var tokResp VerifyResponse
	json.Unmarshal(rr2.Body.Bytes(), &tokResp)
	return tokResp.Token
}

func TestChallengeAuth_FullFlow(t *testing.T) {
	h, devPriv, deviceID := setupAuthHandler(t)
	tok := doVerify(t, h, deviceID, devPriv)
	p := h.Sessions.AuthenticateBearer(tok)
	if p == nil || p.DeviceID != deviceID {
		t.Fatalf("principal: %+v", p)
	}
}

// Replay: ONE challenge → same VerifyRequest twice → first 200, second 401.
func TestChallengeAuth_ReplaySameChallenge(t *testing.T) {
	h, devPriv, deviceID := setupAuthHandler(t)
	clientNonce := make([]byte, 32)
	rand.Read(clientNonce)
	chalReq, _ := json.Marshal(ChallengeRequest{Version: 1, HostID: h.Identity.HostID, DeviceID: deviceID, ClientNonce: hex.EncodeToString(clientNonce)})
	rr := httptest.NewRecorder()
	h.HandleChallenge(rr, httptest.NewRequest("POST", "/challenge", bytes.NewReader(chalReq)))
	var chalResp AuthChallengeResponse
	json.Unmarshal(rr.Body.Bytes(), &chalResp)

	challengeID, _ := hex.DecodeString(chalResp.ChallengeID)
	serverNonce, _ := hex.DecodeString(chalResp.ServerNonce)
	createdAtMS := chalResp.ExpiresAt.Add(-5 * time.Minute).UnixMilli()
	expiresAtMS := chalResp.ExpiresAt.UnixMilli()
	transcript := AuthTranscript{Role: "device", HostID: chalResp.HostID, DeviceID: deviceID,
		DaemonBootID: chalResp.DaemonBootID, ChallengeID: challengeID, ClientNonce: clientNonce,
		ServerNonce: serverNonce, CreatedAtMS: createdAtMS, ExpiresAtMS: expiresAtMS}
	digest := sha256.Sum256(transcript.Build())
	devSig, _ := ecdsa.SignASN1(rand.Reader, devPriv, digest[:])
	sigHex := hex.EncodeToString(devSig)

	verifyReq, _ := json.Marshal(VerifyRequest{Version: 1, ChallengeID: chalResp.ChallengeID, DeviceID: deviceID, Signature: sigHex})
	// First verify.
	rr1 := httptest.NewRecorder()
	h.HandleVerify(rr1, httptest.NewRequest("POST", "/verify", bytes.NewReader(verifyReq)))
	if rr1.Code != 200 {
		t.Fatalf("first verify: %d", rr1.Code)
	}
	// Second verify with same challenge → must fail.
	rr2 := httptest.NewRecorder()
	h.HandleVerify(rr2, httptest.NewRequest("POST", "/verify", bytes.NewReader(verifyReq)))
	if rr2.Code != http.StatusUnauthorized {
		t.Fatalf("replay: %d want 401", rr2.Code)
	}
	// Only one session issued.
	if h.Sessions.Count() != 1 {
		t.Fatalf("sessions=%d want 1", h.Sessions.Count())
	}
}

// Concurrent verifiers on ONE challenge → exactly one 200, one session.
func TestChallengeAuth_ConcurrentSameChallenge(t *testing.T) {
	h, devPriv, deviceID := setupAuthHandler(t)
	clientNonce := make([]byte, 32)
	rand.Read(clientNonce)
	chalReq, _ := json.Marshal(ChallengeRequest{Version: 1, HostID: h.Identity.HostID, DeviceID: deviceID, ClientNonce: hex.EncodeToString(clientNonce)})
	rr := httptest.NewRecorder()
	h.HandleChallenge(rr, httptest.NewRequest("POST", "/challenge", bytes.NewReader(chalReq)))
	var chalResp AuthChallengeResponse
	json.Unmarshal(rr.Body.Bytes(), &chalResp)

	challengeID, _ := hex.DecodeString(chalResp.ChallengeID)
	serverNonce, _ := hex.DecodeString(chalResp.ServerNonce)
	createdAtMS := chalResp.ExpiresAt.Add(-5 * time.Minute).UnixMilli()
	expiresAtMS := chalResp.ExpiresAt.UnixMilli()
	transcript := AuthTranscript{Role: "device", HostID: chalResp.HostID, DeviceID: deviceID,
		DaemonBootID: chalResp.DaemonBootID, ChallengeID: challengeID, ClientNonce: clientNonce,
		ServerNonce: serverNonce, CreatedAtMS: createdAtMS, ExpiresAtMS: expiresAtMS}
	digest := sha256.Sum256(transcript.Build())
	devSig, _ := ecdsa.SignASN1(rand.Reader, devPriv, digest[:])
	sigHex := hex.EncodeToString(devSig)
	verifyReq, _ := json.Marshal(VerifyRequest{Version: 1, ChallengeID: chalResp.ChallengeID, DeviceID: deviceID, Signature: sigHex})

	var okCount int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rr := httptest.NewRecorder()
			h.HandleVerify(rr, httptest.NewRequest("POST", "/verify", bytes.NewReader(verifyReq)))
			if rr.Code == 200 {
				okCount++
			}
		}()
	}
	wg.Wait()
	if okCount != 1 {
		t.Fatalf("concurrent successes=%d want 1", okCount)
	}
	if h.Sessions.Count() != 1 {
		t.Fatalf("sessions=%d want 1", h.Sessions.Count())
	}
}

// Phone-side reference: pin host key, verify host sig, complete verification.
func TestChallengeAuth_PhoneSideHostVerify(t *testing.T) {
	h, devPriv, deviceID := setupAuthHandler(t)
	clientNonce := make([]byte, 32)
	rand.Read(clientNonce)
	chalReq, _ := json.Marshal(ChallengeRequest{Version: 1, HostID: h.Identity.HostID, DeviceID: deviceID, ClientNonce: hex.EncodeToString(clientNonce)})
	rr := httptest.NewRecorder()
	h.HandleChallenge(rr, httptest.NewRequest("POST", "/challenge", bytes.NewReader(chalReq)))
	var chalResp AuthChallengeResponse
	json.Unmarshal(rr.Body.Bytes(), &chalResp)

	// Phone verifies host signature against the QR-pinned host key.
	hostSig, _ := hex.DecodeString(chalResp.HostSignature)
	challengeID, _ := hex.DecodeString(chalResp.ChallengeID)
	serverNonce, _ := hex.DecodeString(chalResp.ServerNonce)
	createdAtMS := chalResp.ExpiresAt.Add(-5 * time.Minute).UnixMilli()
	expiresAtMS := chalResp.ExpiresAt.UnixMilli()
	hostTranscript := AuthTranscript{Role: "host", HostID: chalResp.HostID, DeviceID: deviceID,
		DaemonBootID: chalResp.DaemonBootID, ChallengeID: challengeID, ClientNonce: clientNonce,
		ServerNonce: serverNonce, CreatedAtMS: createdAtMS, ExpiresAtMS: expiresAtMS}
	if !VerifySignature(h.Identity.PublicKeyDER(), hostTranscript.Build(), hostSig) {
		t.Fatalf("host signature verification failed")
	}
	// Wrong role → fails.
	badRoleTranscript := hostTranscript
	badRoleTranscript.Role = "device"
	if VerifySignature(h.Identity.PublicKeyDER(), badRoleTranscript.Build(), hostSig) {
		t.Fatalf("host sig verified with wrong role")
	}

	// Complete the verification.
	tok := doVerify(t, h, deviceID, devPriv)
	if h.Sessions.AuthenticateBearer(tok) == nil {
		t.Fatalf("token invalid")
	}
}

func TestChallengeAuth_SecretLeak(t *testing.T) {
	h, devPriv, deviceID := setupAuthHandler(t)
	clientNonce := make([]byte, 32)
	rand.Read(clientNonce)
	chalReq, _ := json.Marshal(ChallengeRequest{Version: 1, HostID: h.Identity.HostID, DeviceID: deviceID, ClientNonce: hex.EncodeToString(clientNonce)})
	rr := httptest.NewRecorder()
	h.HandleChallenge(rr, httptest.NewRequest("POST", "/challenge", bytes.NewReader(chalReq)))
	// Client nonce must NOT appear in the response.
	if bytes.Contains(rr.Body.Bytes(), clientNonce) {
		t.Fatalf("response leaked raw client nonce")
	}
	// A successful verify response must contain a token; the token must NOT
	// appear in the challenge response.
	tok := doVerify(t, h, deviceID, devPriv)
	if bytes.Contains(rr.Body.Bytes(), []byte(tok)) {
		t.Fatalf("challenge response leaked a session token")
	}
	// Verify that the error path does not leak the nonce or token.
	verifyReq, _ := json.Marshal(VerifyRequest{Version: 1, ChallengeID: hex.EncodeToString(make([]byte, 32)), DeviceID: deviceID, Signature: "00"})
	rr2 := httptest.NewRecorder()
	h.HandleVerify(rr2, httptest.NewRequest("POST", "/verify", bytes.NewReader(verifyReq)))
	if bytes.Contains(rr2.Body.Bytes(), clientNonce) {
		t.Fatalf("error response leaked client nonce")
	}
}

// ── Role-boundary tests (B2 proof) ──

func TestRolePermissions_OwnerGetsFullSet(t *testing.T) {
	h, devPriv, deviceID := setupAuthHandler(t)
	// The device was registered as the first device → owner.
	tok := doVerify(t, h, deviceID, devPriv)
	p := h.Sessions.AuthenticateBearer(tok)
	if p == nil {
		t.Fatalf("principal nil")
	}
	want := []string{PermSessionsRead, PermSessionsCreate, PermSessionsStop, PermSessionsKill, PermHistoryDelete, PermTerminalInput}
	if !setEq(p.Permissions, want) {
		t.Fatalf("owner perms=%v want=%v", p.Permissions, want)
	}
	// Mutate returned perms — must not change a second auth.
	p.Permissions[0] = "evil"
	p2 := h.Sessions.AuthenticateBearer(tok)
	if p2 == nil || !setEq(p2.Permissions, want) {
		t.Fatalf("perms mutated after first read: %v", p2.Permissions)
	}
}

func TestRolePermissions_MemberGetsReadOnly(t *testing.T) {
	h, devPriv, deviceID := setupAuthHandlerForMember(t)
	tok := doVerify(t, h, deviceID, devPriv)
	p := h.Sessions.AuthenticateBearer(tok)
	if p == nil {
		t.Fatalf("principal nil")
	}
	// Member must only have sessions:read.
	if !setEq(p.Permissions, []string{PermSessionsRead}) {
		t.Fatalf("member perms=%v want [sessions:read]", p.Permissions)
	}
	for _, forbidden := range []string{PermSessionsCreate, PermSessionsStop, PermSessionsKill, PermHistoryDelete, PermTerminalInput} {
		if contains(p.Permissions, forbidden) {
			t.Fatalf("member has forbidden perm %s", forbidden)
		}
	}
	// Verify response also carries the correct perms.
	clientNonce := make([]byte, 32)
	rand.Read(clientNonce)
	chalReq, _ := json.Marshal(ChallengeRequest{Version: 1, HostID: h.Identity.HostID, DeviceID: deviceID, ClientNonce: hex.EncodeToString(clientNonce)})
	rr := httptest.NewRecorder()
	h.HandleChallenge(rr, httptest.NewRequest("POST", "/c", bytes.NewReader(chalReq)))
	if rr.Code != 200 {
		t.Fatalf("challenge: %d", rr.Code)
	}
	var cr AuthChallengeResponse
	json.Unmarshal(rr.Body.Bytes(), &cr)
	cid, _ := hex.DecodeString(cr.ChallengeID)
	sn, _ := hex.DecodeString(cr.ServerNonce)
	cms := cr.ExpiresAt.Add(-5 * time.Minute).UnixMilli()
	ems := cr.ExpiresAt.UnixMilli()
	tr := AuthTranscript{Role: "device", HostID: cr.HostID, DeviceID: deviceID, DaemonBootID: cr.DaemonBootID, ChallengeID: cid, ClientNonce: clientNonce, ServerNonce: sn, CreatedAtMS: cms, ExpiresAtMS: ems}
	dig := sha256.Sum256(tr.Build())
	dsig, _ := ecdsa.SignASN1(rand.Reader, devPriv, dig[:])
	vr, _ := json.Marshal(VerifyRequest{Version: 1, ChallengeID: cr.ChallengeID, DeviceID: deviceID, Signature: hex.EncodeToString(dsig)})
	rr2 := httptest.NewRecorder()
	h.HandleVerify(rr2, httptest.NewRequest("POST", "/v", bytes.NewReader(vr)))
	var tokResp VerifyResponse
	json.Unmarshal(rr2.Body.Bytes(), &tokResp)
	if !setEq(tokResp.Permissions, []string{PermSessionsRead}) {
		t.Fatalf("verify response perms=%v", tokResp.Permissions)
	}
	// Mutate the response perms, re-auth — stored perms unchanged.
	tokResp.Permissions[0] = "evil"
	p2 := h.Sessions.AuthenticateBearer(tokResp.Token)
	if p2 == nil || !setEq(p2.Permissions, []string{PermSessionsRead}) {
		t.Fatalf("stored perms mutated via response")
	}
}

func TestRolePermissions_UnknownRoleFails(t *testing.T) {
	if got := PermissionsForRole("superuser"); got != nil {
		t.Fatalf("PermissionsForRole(superuser) = %v, want nil", got)
	}
	if got := PermissionsForRole(""); got != nil {
		t.Fatalf("PermissionsForRole(empty) = %v, want nil", got)
	}
}

// ── Session cap + replacement tests (B3 proof) ──

func TestSessionCap_ReplacementInvalidatesOld(t *testing.T) {
	h, devPriv, deviceID := setupAuthHandler(t)
	tok1 := doVerify(t, h, deviceID, devPriv)
	if h.Sessions.AuthenticateBearer(tok1) == nil {
		t.Fatal("tok1 invalid before replacement")
	}
	// Issue a second token for the same device.
	tok2 := doVerify(t, h, deviceID, devPriv)
	// tok1 must now be invalid.
	if h.Sessions.AuthenticateBearer(tok1) != nil {
		t.Fatal("tok1 still valid after replacement")
	}
	if h.Sessions.AuthenticateBearer(tok2) == nil {
		t.Fatal("tok2 invalid")
	}
	if h.Sessions.Count() != 1 {
		t.Fatalf("sessions=%d want 1", h.Sessions.Count())
	}
	h.Sessions.checkInvariant()
}

func TestSessionCap_GlobalCapBlocksNewDevice(t *testing.T) {
	// Create a fresh session manager with a very small cap.
	m := NewDeviceSessionManagerWithConfig(DeviceSessionManagerConfig{BootID: "b", Lifetime: 20 * time.Minute, MaxSessions: 1})
	m.GetAuth = func(deviceID string) AuthorizationState { return AuthorizationState{Epoch: 0, Active: true} }
	_, _, _, err := m.CreateAfterVerifiedChallenge("d1", "h", "b", PermissionsForRole(RoleOwner), 0)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// Second NEW device must be blocked (cap=1, and d1 is not d2).
	_, _, _, err = m.CreateAfterVerifiedChallenge("d2", "h", "b", PermissionsForRole(RoleOwner), 0)
	if err == nil {
		t.Fatal("global cap not enforced")
	}
	m.checkInvariant()
}

func TestSessionCap_ReplacementAllowedAtCap(t *testing.T) {
	m := NewDeviceSessionManagerWithConfig(DeviceSessionManagerConfig{BootID: "b", Lifetime: 20 * time.Minute, MaxSessions: 1})
	m.GetAuth = func(deviceID string) AuthorizationState { return AuthorizationState{Epoch: 0, Active: true} }
	_, _, _, err := m.CreateAfterVerifiedChallenge("d1", "h", "b", PermissionsForRole(RoleOwner), 0)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// Replacement for same device must succeed even at cap.
	_, _, _, err = m.CreateAfterVerifiedChallenge("d1", "h", "b", PermissionsForRole(RoleOwner), 0)
	if err != nil {
		t.Fatalf("replacement at cap blocked: %v", err)
	}
	if m.Count() != 1 {
		t.Fatalf("count=%d", m.Count())
	}
	m.checkInvariant()
}

func TestSessionCap_SlotFreedAfterExpiry(t *testing.T) {
	m := NewDeviceSessionManagerWithConfig(DeviceSessionManagerConfig{BootID: "b", Lifetime: 1 * time.Millisecond, MaxSessions: 1})
	m.GetAuth = func(deviceID string) AuthorizationState { return AuthorizationState{Epoch: 0, Active: true} }
	tok, _, _, _ := m.CreateAfterVerifiedChallenge("d1", "h", "b", PermissionsForRole(RoleOwner), 0)
	time.Sleep(10 * time.Millisecond)
	if m.AuthenticateBearer(tok) != nil {
		t.Fatal("expired token still valid")
	}
	// Slot freed — new device can issue.
	_, _, _, err := m.CreateAfterVerifiedChallenge("d2", "h", "b", PermissionsForRole(RoleOwner), 0)
	if err != nil {
		t.Fatalf("slot not freed after expiry: %v", err)
	}
	m.checkInvariant()
}

func TestSessionCap_ConcurrentReplacementOneWinner(t *testing.T) {
	h, devPriv, deviceID := setupAuthHandler(t)
	clientNonce := make([]byte, 32)
	rand.Read(clientNonce)
	chalReq, _ := json.Marshal(ChallengeRequest{Version: 1, HostID: h.Identity.HostID, DeviceID: deviceID, ClientNonce: hex.EncodeToString(clientNonce)})
	rr := httptest.NewRecorder()
	h.HandleChallenge(rr, httptest.NewRequest("POST", "/c", bytes.NewReader(chalReq)))
	var cr AuthChallengeResponse
	json.Unmarshal(rr.Body.Bytes(), &cr)
	cid, _ := hex.DecodeString(cr.ChallengeID)
	sn, _ := hex.DecodeString(cr.ServerNonce)
	cms := cr.ExpiresAt.Add(-5 * time.Minute).UnixMilli()
	ems := cr.ExpiresAt.UnixMilli()
	tr := AuthTranscript{Role: "device", HostID: cr.HostID, DeviceID: deviceID, DaemonBootID: cr.DaemonBootID, ChallengeID: cid, ClientNonce: clientNonce, ServerNonce: sn, CreatedAtMS: cms, ExpiresAtMS: ems}
	dig := sha256.Sum256(tr.Build())
	dsig, _ := ecdsa.SignASN1(rand.Reader, devPriv, dig[:])
	vr, _ := json.Marshal(VerifyRequest{Version: 1, ChallengeID: cr.ChallengeID, DeviceID: deviceID, Signature: hex.EncodeToString(dsig)})

	// Each goroutine gets its own challenge (unique clientNonce).
	var ready sync.WaitGroup
	ready.Add(10)
	var done sync.WaitGroup
	done.Add(10)
	var successCount int32
	for i := 0; i < 10; i++ {
		go func() {
			defer done.Done()
			ready.Done()
			ready.Wait()
			rr := httptest.NewRecorder()
			h.HandleVerify(rr, httptest.NewRequest("POST", "/v", bytes.NewReader(vr)))
			if rr.Code == 200 {
				successCount++
			}
		}()
	}
	done.Wait()
	// Because challenges are single-use, only ONE verify should succeed (not 10).
	// But each goroutine uses the SAME challenge. First one consumes; rest fail.
	if successCount != 1 {
		t.Fatalf("concurrent successes=%d want 1", successCount)
	}
	h.Sessions.checkInvariant()
}

// ── Rate limiter integration test ──

func TestRateLimiter_Integration(t *testing.T) {
	// Inject a limiter with burst=2, call 3 times → 3rd must fail.
	lim := NewChallengeRateLimiter(RateLimiterConfig{Burst: 2, RatePerMin: 0.001, MaxAge: 10 * time.Minute})
	now := time.Now()
	if !lim.Allow(now, "d1") {
		t.Fatal("1st")
	}
	if !lim.Allow(now, "d1") {
		t.Fatal("2nd")
	}
	if lim.Allow(now, "d1") {
		t.Fatal("3rd should be rate-limited")
	}
}

// ── helpers ──

func setEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	am := make(map[string]int, len(a))
	for _, v := range a {
		am[v]++
	}
	for _, v := range b {
		if am[v] == 0 {
			return false
		}
		am[v]--
	}
	return true
}
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func setupAuthHandlerForMember(t *testing.T) (*AuthHandler, *ecdsa.PrivateKey, string) {
	t.Helper()
	id, _ := LoadOrCreateHostIdentity(&FileKeyStore{Path: t.TempDir() + "/host.json"})
	reg, _ := newReg(t)
	// First device → owner.
	reg.Add(genPubDER(t), "owner-device")
	// Second device → member.
	devPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&devPriv.PublicKey)
	d, _ := reg.Add(pubDER, "member-device")
	if d.Role != RoleMember {
		t.Fatalf("expected member role, got %s", d.Role)
	}
	bootID, _ := NewBootID()
	return &AuthHandler{Identity: id, Registry: reg, Challenges: NewChallengeStore(), Sessions: NewPermissiveSessionManager(bootID, 20*time.Minute), RateLimiter: NewChallengeRateLimiter(RateLimiterConfig{Burst: 100, RatePerMin: 1000})}, devPriv, d.DeviceID
}

func TestRateLimiter_HandleChallengeIntegration(t *testing.T) {
	h, _, deviceID := setupAuthHandler(t)
	lim := NewChallengeRateLimiter(RateLimiterConfig{Burst: 2, RatePerMin: 0.001, MaxAge: 10 * time.Minute})
	h.RateLimiter = lim
	for i := 0; i < 2; i++ {
		cn := make([]byte, 32)
		rand.Read(cn)
		req, _ := json.Marshal(ChallengeRequest{Version: 1, HostID: h.Identity.HostID, DeviceID: deviceID, ClientNonce: hex.EncodeToString(cn)})
		rr := httptest.NewRecorder()
		h.HandleChallenge(rr, httptest.NewRequest("POST", "/c", bytes.NewReader(req)))
		if rr.Code != 200 {
			t.Fatalf("challenge %d: status=%d", i+1, rr.Code)
		}
	}
	cn := make([]byte, 32)
	rand.Read(cn)
	req, _ := json.Marshal(ChallengeRequest{Version: 1, HostID: h.Identity.HostID, DeviceID: deviceID, ClientNonce: hex.EncodeToString(cn)})
	rr := httptest.NewRecorder()
	h.HandleChallenge(rr, httptest.NewRequest("POST", "/c", bytes.NewReader(req)))
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd challenge: status=%d want 429", rr.Code)
	}
}

func TestSessionCap_ConcurrentReplacementTwoChallenges(t *testing.T) {
	h, devPriv, deviceID := setupAuthHandler(t)
	cn1 := make([]byte, 32)
	rand.Read(cn1)
	cn2 := make([]byte, 32)
	rand.Read(cn2)
	cr1 := issueChallenge(t, h, deviceID, cn1)
	cr2 := issueChallenge(t, h, deviceID, cn2)
	buildVerify := func(cr AuthChallengeResponse, cn []byte) []byte {
		cid, _ := hex.DecodeString(cr.ChallengeID)
		sn, _ := hex.DecodeString(cr.ServerNonce)
		cms := cr.ExpiresAt.Add(-5 * time.Minute).UnixMilli()
		ems := cr.ExpiresAt.UnixMilli()
		tr := AuthTranscript{Role: "device", HostID: cr.HostID, DeviceID: deviceID, DaemonBootID: cr.DaemonBootID, ChallengeID: cid, ClientNonce: cn, ServerNonce: sn, CreatedAtMS: cms, ExpiresAtMS: ems}
		dig := sha256.Sum256(tr.Build())
		dsig, _ := ecdsa.SignASN1(rand.Reader, devPriv, dig[:])
		vr, _ := json.Marshal(VerifyRequest{Version: 1, ChallengeID: cr.ChallengeID, DeviceID: deviceID, Signature: hex.EncodeToString(dsig)})
		return vr
	}
	vr1 := buildVerify(cr1, cn1)
	vr2 := buildVerify(cr2, cn2)
	var wg sync.WaitGroup
	wg.Add(2)
	var tok1, tok2 string
	go func() {
		defer wg.Done()
		rr := httptest.NewRecorder()
		h.HandleVerify(rr, httptest.NewRequest("POST", "/v", bytes.NewReader(vr1)))
		if rr.Code == 200 {
			var r VerifyResponse
			json.Unmarshal(rr.Body.Bytes(), &r)
			tok1 = r.Token
		}
	}()
	go func() {
		defer wg.Done()
		rr := httptest.NewRecorder()
		h.HandleVerify(rr, httptest.NewRequest("POST", "/v", bytes.NewReader(vr2)))
		if rr.Code == 200 {
			var r VerifyResponse
			json.Unmarshal(rr.Body.Bytes(), &r)
			tok2 = r.Token
		}
	}()
	wg.Wait()
	if h.Sessions.Count() != 1 {
		t.Fatalf("sessions=%d want 1", h.Sessions.Count())
	}
	valid := 0
	if h.Sessions.AuthenticateBearer(tok1) != nil {
		valid++
	}
	if h.Sessions.AuthenticateBearer(tok2) != nil {
		valid++
	}
	if valid != 1 {
		t.Fatalf("valid tokens=%d want 1", valid)
	}
	h.Sessions.checkInvariant()
}

func issueChallenge(t *testing.T, h *AuthHandler, deviceID string, clientNonce []byte) AuthChallengeResponse {
	t.Helper()
	req, _ := json.Marshal(ChallengeRequest{Version: 1, HostID: h.Identity.HostID, DeviceID: deviceID, ClientNonce: hex.EncodeToString(clientNonce)})
	rr := httptest.NewRecorder()
	h.HandleChallenge(rr, httptest.NewRequest("POST", "/c", bytes.NewReader(req)))
	if rr.Code != 200 {
		t.Fatalf("challenge: %d", rr.Code)
	}
	var cr AuthChallengeResponse
	json.Unmarshal(rr.Body.Bytes(), &cr)
	return cr
}
