package main

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
	"path/filepath"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

func TestAuthProductionPath_UnavailableBeforePairing(t *testing.T) {
	dir := t.TempDir()
	// Host identity + empty device registry.
	id, _ := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: filepath.Join(dir, "host.json")})
	reg, _ := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: filepath.Join(dir, "devices.json")})
	// Init device trust so the auth handler is wired.
	initAuth := func() { /* set via SetPairingContext-like path */ }
	_ = id
	_ = reg
	_ = initAuth

	// Build the App through the real constructor but with no paired device.
	cfg := Config{InsecureLocalOnly: true}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	// Before any device is paired, the auth endpoints return 503.
	srv := httptest.NewServer(app.server.Handler)
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{
		"version": 1, "hostId": "x", "deviceId": "y",
		"clientNonce": hex.EncodeToString(make([]byte, 32)),
	})
	resp, _ := http.Post(srv.URL+"/api/device-auth/challenge", "application/json", bytes.NewReader(body))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unpaired: challenge status=%d want 503, wiring broken?", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAuthProductionPath_ChallengeAndVerify(t *testing.T) {
	dir := t.TempDir()
	// Pre-create a host identity and a paired device.
	id, _ := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: filepath.Join(dir, "host.json")})
	reg, _ := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: filepath.Join(dir, "devices.json")})
	devPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&devPriv.PublicKey)
	d, _ := reg.Add(pubDER, "e2e-device")

	// Build App with the device trust installed.
	cfg := Config{InsecureLocalOnly: true}
	app, err := NewApp(cfg)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	app.hostIdentity = id
	app.deviceRegistry = reg
	if app.authHandler != nil {
		app.authHandler.Identity = id
		app.authHandler.Registry = reg
	}

	srv := httptest.NewServer(app.server.Handler)
	defer srv.Close()

	// 1) Challenge.
	clientNonce := make([]byte, 32)
	rand.Read(clientNonce)
	chalBody, _ := json.Marshal(map[string]interface{}{
		"version": 1, "hostId": id.HostID, "deviceId": d.DeviceID,
		"clientNonce": hex.EncodeToString(clientNonce),
	})
	resp, err := http.Post(srv.URL+"/api/device-auth/challenge", "application/json", bytes.NewReader(chalBody))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("challenge: err=%v status=%d", err, resp.StatusCode)
	}
	var chalResp devicetrust.AuthChallengeResponse
	json.NewDecoder(resp.Body).Decode(&chalResp)
	resp.Body.Close()

	// 2) Verify.
	challengeID, _ := hex.DecodeString(chalResp.ChallengeID)
	serverNonce, _ := hex.DecodeString(chalResp.ServerNonce)
	createdAtMS := chalResp.ExpiresAt.Add(-5 * time.Minute).UnixMilli()
	expiresAtMS := chalResp.ExpiresAt.UnixMilli()
	transcript := devicetrust.AuthTranscript{Role: "device", HostID: chalResp.HostID,
		DeviceID: d.DeviceID, DaemonBootID: chalResp.DaemonBootID,
		ChallengeID: challengeID, ClientNonce: clientNonce, ServerNonce: serverNonce,
		CreatedAtMS: createdAtMS, ExpiresAtMS: expiresAtMS}
	digest := sha256.Sum256(transcript.Build())
	devSig, _ := ecdsa.SignASN1(rand.Reader, devPriv, digest[:])
	verifyBody, _ := json.Marshal(map[string]interface{}{
		"version": 1, "challengeId": chalResp.ChallengeID,
		"deviceId": d.DeviceID, "signature": hex.EncodeToString(devSig),
	})
	resp2, err := http.Post(srv.URL+"/api/device-auth/verify", "application/json", bytes.NewReader(verifyBody))
	if err != nil || resp2.StatusCode != 200 {
		t.Fatalf("verify: err=%v status=%d", err, resp2.StatusCode)
	}
	var tokResp devicetrust.VerifyResponse
	json.NewDecoder(resp2.Body).Decode(&tokResp)
	resp2.Body.Close()
	if tokResp.Token == "" || tokResp.TokenType != "Bearer" {
		t.Fatalf("token: %+v", tokResp)
	}
}
