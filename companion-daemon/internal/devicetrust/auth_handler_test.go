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
		Challenges: NewChallengeStore(),
		Sessions:   NewDeviceSessionManager(bootID, 20*time.Minute),
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
