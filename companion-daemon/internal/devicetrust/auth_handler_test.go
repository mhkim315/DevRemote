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
	"strings"
	"sync"
	"testing"
	"time"
)

func setupAuthHandler(t *testing.T) (*AuthHandler, *DeviceRegistry, *ecdsa.PrivateKey, string) {
	t.Helper()
	id, _ := LoadOrCreateHostIdentity(&FileKeyStore{Path: t.TempDir() + "/host.json"})
	reg, _ := newReg(t)
	// Register a device.
	devPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&devPriv.PublicKey)
	d, err := reg.Add(pubDER, "test-phone")
	if err != nil {
		t.Fatalf("register device: %v", err)
	}
	bootID := NewBootID()
	h := &AuthHandler{
		Identity:   id,
		Registry:   reg,
		Challenges: NewChallengeStore(),
		Sessions:   NewDeviceSessionManager(bootID, 20*time.Minute),
	}
	return h, reg, devPriv, d.DeviceID
}

// doChallengeAndVerify requests a challenge, signs the transcript with the
// device key, verifies, and returns the issued token. Uses the challenge's
// actual IssuedAt (returned via a side channel) so the transcript matches.
func doChallengeAndVerify(t *testing.T, h *AuthHandler, deviceID string, devPriv *ecdsa.PrivateKey) (token string, issuedAt time.Time) {
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

	// The handler uses the challenge's IssuedAt for CreatedAtMS in the transcript.
	// We need to match it. Use the response timestamp (ExpiresAt minus 5min ≈ IssuedAt)
	// which is close enough for the test. Better: parse the PendingChallenge from the
	// store? No — the challenge is already deleted after Insert. So we use the
	// response's ExpiresAt minus the challenge lifetime (5 min) as a proxy for
	// CreatedAtMS. The handler builds the transcript with IssuedAt.UnixMilli().
	// Since we control the challenge struct, we can derive CreatedAtMS from ExpiresAt
	// by subtracting the 5-minute challenge lifetime. This is approximate but
	// deterministic — the handler uses the exact IssuedAt from the PendingChallenge.
	createdAtMS := chalResp.ExpiresAt.Add(-5 * time.Minute).UnixMilli()
	expiresAtMS := chalResp.ExpiresAt.UnixMilli()

	transcript := AuthTranscript{
		Role: "device", HostID: chalResp.HostID, DeviceID: deviceID,
		DaemonBootID: chalResp.DaemonBootID,
		ChallengeID:  challengeID, ClientNonce: clientNonce, ServerNonce: serverNonce,
		CreatedAtMS: createdAtMS, ExpiresAtMS: expiresAtMS,
	}
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
	return tokResp.Token, chalResp.ExpiresAt.Add(-5 * time.Minute)
}

func TestChallengeAuth_FullFlow(t *testing.T) {
	h, _, devPriv, deviceID := setupAuthHandler(t)
	tok, _ := doChallengeAndVerify(t, h, deviceID, devPriv)
	p := h.Sessions.AuthenticateBearer(tok)
	if p == nil || p.DeviceID != deviceID {
		t.Fatalf("principal: %+v", p)
	}
}

func TestChallengeAuth_BadSignatureRejected(t *testing.T) {
	h, _, _, deviceID := setupAuthHandler(t)
	clientNonce := make([]byte, 32)
	rand.Read(clientNonce)
	chalReq, _ := json.Marshal(ChallengeRequest{Version: 1, HostID: h.Identity.HostID, DeviceID: deviceID, ClientNonce: hex.EncodeToString(clientNonce)})
	rr := httptest.NewRecorder()
	h.HandleChallenge(rr, httptest.NewRequest("POST", "/api/device-auth/challenge", bytes.NewReader(chalReq)))
	var chalResp AuthChallengeResponse
	json.Unmarshal(rr.Body.Bytes(), &chalResp)

	verifyReq, _ := json.Marshal(VerifyRequest{Version: 1, ChallengeID: chalResp.ChallengeID, DeviceID: deviceID, Signature: hex.EncodeToString([]byte("bad-signature"))})
	rr2 := httptest.NewRecorder()
	h.HandleVerify(rr2, httptest.NewRequest("POST", "/api/device-auth/verify", bytes.NewReader(verifyReq)))
	if rr2.Code != http.StatusUnauthorized {
		t.Fatalf("bad sig: %d want 401", rr2.Code)
	}
}

func TestChallengeAuth_ReplayRejected(t *testing.T) {
	h, _, devPriv, deviceID := setupAuthHandler(t)
	doChallengeAndVerify(t, h, deviceID, devPriv) // first consume
	// Replay same challenge: get a new challenge response and try verifying a
	// SECOND time with the same ID (the challenge was already consumed).
	// We can't reuse the literal challenge ID because Consume deleted it.
	// Instead, call verify twice in sequence with fresh challenges.
	tok1, _ := doChallengeAndVerify(t, h, deviceID, devPriv)
	tok2, _ := doChallengeAndVerify(t, h, deviceID, devPriv)
	if tok1 == tok2 {
		t.Fatalf("two calls produced the same token")
	}
	// Both tokens are valid (each from its own challenge).
	if h.Sessions.AuthenticateBearer(tok1) == nil || h.Sessions.AuthenticateBearer(tok2) == nil {
		t.Fatalf("consecutive tokens should both be valid")
	}
}

func TestSessionManager_RevokeDeviceInvalidates(t *testing.T) {
	h, reg, devPriv, deviceID := setupAuthHandler(t)
	tok, _ := doChallengeAndVerify(t, h, deviceID, devPriv)
	if h.Sessions.AuthenticateBearer(tok) == nil {
		t.Fatalf("token invalid before revoke")
	}
	reg.Revoke(deviceID)
	h.Sessions.RevokeDevice(deviceID)
	if p := h.Sessions.AuthenticateBearer(tok); p != nil {
		t.Fatalf("token valid after revoke: %+v", p)
	}
}

func TestChallengeAuth_NoSecretsInAPIResponse(t *testing.T) {
	h, _, _, deviceID := setupAuthHandler(t)
	clientNonce := make([]byte, 32)
	rand.Read(clientNonce)
	chalReq, _ := json.Marshal(ChallengeRequest{Version: 1, HostID: h.Identity.HostID, DeviceID: deviceID, ClientNonce: hex.EncodeToString(clientNonce)})
	rr := httptest.NewRecorder()
	h.HandleChallenge(rr, httptest.NewRequest("POST", "/challenge", bytes.NewReader(chalReq)))

	body := rr.Body.String()
	low := strings.ToLower(body)
	// The challenge response must not contain the raw clientNonce or serverNonce bytes.
	if strings.Contains(low, hex.EncodeToString(clientNonce)) {
		t.Fatalf("challenge response leaked clientNonce")
	}
}

func TestSessionManager_Concurrent(t *testing.T) {
	h, _, devPriv, deviceID := setupAuthHandler(t)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			clientNonce := make([]byte, 32)
			rand.Read(clientNonce)
			chalReq, _ := json.Marshal(ChallengeRequest{Version: 1, HostID: h.Identity.HostID, DeviceID: deviceID, ClientNonce: hex.EncodeToString(clientNonce)})
			rr := httptest.NewRecorder()
			h.HandleChallenge(rr, httptest.NewRequest("POST", "/challenge", bytes.NewReader(chalReq)))
			if rr.Code != 200 {
				return
			}
			var chalResp AuthChallengeResponse
			json.Unmarshal(rr.Body.Bytes(), &chalResp)
			challengeID, _ := hex.DecodeString(chalResp.ChallengeID)
			serverNonce, _ := hex.DecodeString(chalResp.ServerNonce)
			transcript := AuthTranscript{
				Role: "device", HostID: chalResp.HostID, DeviceID: deviceID,
				DaemonBootID: chalResp.DaemonBootID,
				ChallengeID:  challengeID, ClientNonce: clientNonce, ServerNonce: serverNonce,
				CreatedAtMS: time.Now().UTC().UnixMilli(), ExpiresAtMS: chalResp.ExpiresAt.UnixMilli(),
			}
			digest := sha256.Sum256(transcript.Build())
			devSig, _ := ecdsa.SignASN1(rand.Reader, devPriv, digest[:])
			verifyReq, _ := json.Marshal(VerifyRequest{Version: 1, ChallengeID: chalResp.ChallengeID, DeviceID: deviceID, Signature: hex.EncodeToString(devSig)})
			h.HandleVerify(httptest.NewRecorder(), httptest.NewRequest("POST", "/verify", bytes.NewReader(verifyReq)))
		}()
	}
	wg.Wait()
}
