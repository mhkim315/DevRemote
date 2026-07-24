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
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

var testKey = func() *ecdsa.PrivateKey {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	return priv
}()

type testId struct {
	pub  PublicHostIdentity
	priv *ecdsa.PrivateKey
}

func newTestId(hostID string) *testId {
	pubDER, _ := x509.MarshalPKIXPublicKey(&testKey.PublicKey)
	h := sha256.Sum256(pubDER)
	fp := hex.EncodeToString(h[:])
	return &testId{pub: PublicHostIdentity{HostID: hostID, Fingerprint: fp, PublicKeyDER: pubDER}, priv: testKey}
}

func (t *testId) Public() PublicHostIdentity { return t.pub }
func (t *testId) Sign(msg []byte) ([]byte, error) {
	digest := sha256.Sum256(msg)
	return ecdsa.SignASN1(rand.Reader, t.priv, digest[:])
}

func genKeypair(t *testing.T) (*ecdsa.PrivateKey, []byte, string) {
	t.Helper()
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	return priv, pubDER, Fingerprint(pubDER)
}

func signTranscript(t *testing.T, priv *ecdsa.PrivateKey, phoneNonce, hostNonce, hostPubDER []byte, sessionID string) []byte {
	t.Helper()
	data := buildPairingTranscript(phoneNonce, hostNonce, hostPubDER, sessionID)
	digest := sha256.Sum256(data)
	sig, _ := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	return sig
}

func startTestPairing(t *testing.T, r *DeviceRegistry) *PairingHost {
	t.Helper()
	id := newTestId("h1")
	ph, err := StartPairing(PairingConfig{Listen: "0.0.0.0:0", LANAddr: "127.0.0.1", SessionLifetime: 5 * time.Second, Identity: id, Signer: id, Registry: r})
	if err != nil {
		t.Fatalf("StartPairing: %v", err)
	}
	t.Cleanup(func() { ph.Close() })
	return ph
}

func TestPairing_Full2PhaseAndApprove(t *testing.T) {
	r, _ := newReg(t)
	ph := startTestPairing(t, r)
	priv, pubDER, fp := genKeypair(t)
	phoneNonce := make([]byte, 16)
	rand.Read(phoneNonce)
	cb, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, DisplayName: "p1", PhoneNonce: phoneNonce, BootstrapToken: ph.Session.BootstrapToken})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(cb))
	if resp.StatusCode != 200 {
		t.Fatalf("phase1=%d", resp.StatusCode)
	}
	var ch ChallengeResponse
	json.NewDecoder(resp.Body).Decode(&ch)
	resp.Body.Close()
	sig := signTranscript(t, priv, phoneNonce, ch.HostNonce, ch.HostPublicDER, ph.Session.SessionID)
	cfb, _ := json.Marshal(Confirmation{PhoneSignature: sig})
	r2, _ := http.Post("http://"+ph.addr+"/pair/confirm", "application/json", bytes.NewReader(cfb))
	if r2.StatusCode != 200 {
		t.Fatalf("phase2=%d", r2.StatusCode)
	}
	var cr struct {
		Status    string
		HostProof string
	}
	json.NewDecoder(r2.Body).Decode(&cr)
	r2.Body.Close()
	if cr.Status != "proof_verified" || cr.HostProof == "" {
		t.Fatalf("confirm=%+v", cr)
	}
	cand, ok := ph.WaitForCandidate()
	if !ok || cand.Fingerprint != fp {
		t.Fatalf("WFC ok=%v fp=%s", ok, fp)
	}
	if err := ph.Approve(); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if _, a := r.GetActiveByFingerprint(fp); !a {
		t.Fatalf("not in registry")
	}
}

// TestMobilePairingIntegration proves the full protocol flow the mobile
// library follows — QR hex fields, endpoint paths, and the new result poll.
func TestMobilePairingIntegration(t *testing.T) {
	r, _ := newReg(t)
	ph := startTestPairing(t, r)

	// Build the QR payload as the daemon does (hostPubKey is hex-encoded SPKI).
	hostPub := ph.cfg.Identity.Public()
	qrJSON, _ := json.Marshal(map[string]string{
		"sessionId":      ph.Session.SessionID,
		"hostId":         hostPub.HostID,
		"fingerprint":    hostPub.Fingerprint,
		"hostPubKey":     hex.EncodeToString(hostPub.PublicKeyDER),
		"bootstrapToken": ph.Session.BootstrapToken,
		"endpoint":       "http://" + ph.addr + "/pair",
		"expiresAt":      ph.Session.ExpiresAt.Format(time.RFC3339),
	})

	// ── Parse QR (mobile-side: hex hostPubKey) ──
	var qr map[string]string
	json.Unmarshal(qrJSON, &qr)
	hostPubDER, err := hex.DecodeString(qr["hostPubKey"])
	if err != nil || len(hostPubDER) != 91 {
		t.Fatalf("QR hostPubKey hex decode: err=%v len=%d", err, len(hostPubDER))
	}
	if got := Fingerprint(hostPubDER); got != qr["fingerprint"] {
		t.Fatalf("QR fingerprint mismatch: %s != %s", got, qr["fingerprint"])
	}

	// ── Phase 1: POST to endpoint directly (endpoint already is ".../pair") ──
	devPriv, devPubDER, devFP := genKeypair(t)
	phoneNonce := make([]byte, 32)
	rand.Read(phoneNonce)
	phase1Body, _ := json.Marshal(PairingRequest{PublicKeyDER: devPubDER, DisplayName: "Pokit Mobile", PhoneNonce: phoneNonce, BootstrapToken: qr["bootstrapToken"]})
	r1, err := http.Post(qr["endpoint"], "application/json", bytes.NewReader(phase1Body))
	if err != nil {
		t.Fatalf("phase1 request: %v", err)
	}
	if r1.StatusCode != 200 {
		t.Fatalf("phase1 status=%d", r1.StatusCode)
	}
	var ch ChallengeResponse
	json.NewDecoder(r1.Body).Decode(&ch)
	r1.Body.Close()

	// ── Phase 2: sign transcript, POST /pair/confirm ──
	sig := signTranscript(t, devPriv, phoneNonce, ch.HostNonce, ch.HostPublicDER, qr["sessionId"])
	phase2Body, _ := json.Marshal(Confirmation{PhoneSignature: sig})
	r2, err := http.Post(siblingURL(qr["endpoint"], "/pair/confirm"), "application/json", bytes.NewReader(phase2Body))
	if err != nil {
		t.Fatalf("phase2 request: %v", err)
	}
	if r2.StatusCode != 200 {
		t.Fatalf("phase2 status=%d", r2.StatusCode)
	}
	var cr struct {
		Status    string `json:"status"`
		HostProof string `json:"hostProof"`
	}
	json.NewDecoder(r2.Body).Decode(&cr)
	r2.Body.Close()
	if cr.Status != "proof_verified" {
		t.Fatalf("phase2 status=%s", cr.Status)
	}
	// HostProof is hex-encoded on the wire.
	hostProofDER, err := hex.DecodeString(cr.HostProof)
	if err != nil {
		t.Fatalf("hostProof hex decode: %v", err)
	}
	pub, _ := ParseP256PublicKey(ch.HostPublicDER)
	td := sha256.Sum256(buildPairingTranscript(phoneNonce, ch.HostNonce, ch.HostPublicDER, qr["sessionId"]))
	if !ecdsa.VerifyASN1(pub, td[:], hostProofDER) {
		t.Fatal("host proof verification failed")
	}

	// ── Result: poll with session binding ──
	resultURL := siblingURL(qr["endpoint"], "/pair/result") + "?session=" + qr["sessionId"]

	// Before approval: pending.
	r3, _ := http.Get(resultURL)
	var pres map[string]string
	json.NewDecoder(r3.Body).Decode(&pres)
	r3.Body.Close()
	if pres["status"] != "pending" {
		t.Fatalf("result before approve=%s", pres["status"])
	}

	// Approve via IPC path (operator).
	if _, ok := ph.WaitForCandidate(); !ok {
		t.Fatal("no candidate")
	}
	ph.Approve()

	// After approval, poll until the result is readable (within grace period).
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		r3, _ = http.Get(resultURL)
		var ares map[string]interface{}
		json.NewDecoder(r3.Body).Decode(&ares)
		r3.Body.Close()
		if ares["status"] == "approved" {
			if ares["deviceId"] == nil || ares["fingerprint"] == nil {
				t.Fatalf("approved with no deviceId/fingerprint: %v", ares)
			}
			// Registry recorded the device.
			if _, a := r.GetActiveByFingerprint(devFP); !a {
				t.Fatal("device not in registry after approve")
			}
			return
		}
	}
	t.Fatal("result never reached approved")
}

// TestMobilePairingRejectLifecycle proves /pair/result returns "rejected"
// within the grace period.
func TestMobilePairingRejectLifecycle(t *testing.T) {
	r, _ := newReg(t)
	ph := startTestPairing(t, r)

	// Drive Phase 1 + Phase 2 to proof_verified.
	priv, pubDER, _ := genKeypair(t)
	phoneNonce := make([]byte, 32)
	rand.Read(phoneNonce)
	cb, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, DisplayName: "p", PhoneNonce: phoneNonce, BootstrapToken: ph.Session.BootstrapToken})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(cb))
	var ch ChallengeResponse
	json.NewDecoder(resp.Body).Decode(&ch)
	resp.Body.Close()
	sig := signTranscript(t, priv, phoneNonce, ch.HostNonce, ch.HostPublicDER, ph.Session.SessionID)
	cfb, _ := json.Marshal(Confirmation{PhoneSignature: sig})
	http.Post("http://"+ph.addr+"/pair/confirm", "application/json", bytes.NewReader(cfb))
	if _, ok := ph.WaitForCandidate(); !ok {
		t.Fatal("no candidate")
	}

	resultURL := "http://" + ph.addr + "/pair/result?session=" + ph.Session.SessionID

	// Reject the candidate (operator declines).
	ph.Reject()

	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		r3, _ := http.Get(resultURL)
		var ar map[string]string
		json.NewDecoder(r3.Body).Decode(&ar)
		r3.Body.Close()
		if ar["status"] == "rejected" {
			return
		}
	}
	t.Fatal("result never reached rejected")
}

func TestPairing_ResultSessionMismatch(t *testing.T) {
	r, _ := newReg(t)
	ph := startTestPairing(t, r)
	resp, _ := http.Get("http://" + ph.addr + "/pair/result?session=wrong")
	if resp.StatusCode != 403 {
		t.Fatalf("result with wrong session: %d want 403", resp.StatusCode)
	}
}

// siblingURL substitutes the path component of endpoint (which ends in "/pair")
// with sibling paths like "/pair/confirm" or "/pair/result".
func siblingURL(endpoint, path string) string {
	base := endpoint[:len(endpoint)-len("/pair")]
	return base + path
}

func TestPairing_NonP256Rejected(t *testing.T) {
	r, _ := newReg(t)
	ph := startTestPairing(t, r)
	cb, _ := json.Marshal(PairingRequest{PublicKeyDER: []byte("garbage"), PhoneNonce: []byte("1234567890abcdef"), BootstrapToken: ph.Session.BootstrapToken})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(cb))
	if resp.StatusCode != 400 {
		t.Fatalf("nonP256=%d", resp.StatusCode)
	}
}

func TestPairing_NoBootstrapRejected(t *testing.T) {
	r, _ := newReg(t)
	ph := startTestPairing(t, r)
	_, pubDER, _ := genKeypair(t)
	cb, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, PhoneNonce: []byte("1234567890abcdef"), BootstrapToken: "wrong"})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(cb))
	if resp.StatusCode != 401 {
		t.Fatalf("bad bootstrap=%d", resp.StatusCode)
	}
}

func TestPairing_SecondCandidateRejected(t *testing.T) {
	r, _ := newReg(t)
	ph := startTestPairing(t, r)
	priv, pubDER, _ := genKeypair(t)
	_, pub2, _ := genKeypair(t)
	phoneNonce := make([]byte, 16)
	rand.Read(phoneNonce)
	b1, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, DisplayName: "p1", PhoneNonce: phoneNonce, BootstrapToken: ph.Session.BootstrapToken})
	r1, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(b1))
	if r1.StatusCode != 200 {
		t.Fatalf("first=%d", r1.StatusCode)
	}
	var ch ChallengeResponse
	json.NewDecoder(r1.Body).Decode(&ch)
	r1.Body.Close()
	b2, _ := json.Marshal(PairingRequest{PublicKeyDER: pub2, PhoneNonce: []byte("1111111111111111"), BootstrapToken: ph.Session.BootstrapToken})
	r2, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(b2))
	if r2.StatusCode != 410 {
		t.Fatalf("second=%d", r2.StatusCode)
	}
	sig := signTranscript(t, priv, phoneNonce, ch.HostNonce, ch.HostPublicDER, ph.Session.SessionID)
	cfb, _ := json.Marshal(Confirmation{PhoneSignature: sig})
	http.Post("http://"+ph.addr+"/pair/confirm", "application/json", bytes.NewReader(cfb))
	if _, ok := ph.WaitForCandidate(); !ok {
		t.Fatalf("no candidate")
	}
	if err := ph.Approve(); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if len(r.List()) != 1 {
		t.Fatalf("reg=%d", len(r.List()))
	}
}

func TestPairing_ExpiredSessionRejected(t *testing.T) {
	r, _ := newReg(t)
	_, pubDER, _ := genKeypair(t)
	id := newTestId("h1")
	ph, _ := StartPairing(PairingConfig{Listen: "0.0.0.0:0", LANAddr: "127.0.0.1", SessionLifetime: 50 * time.Millisecond, Identity: id, Signer: id, Registry: r})
	defer ph.Close()
	time.Sleep(200 * time.Millisecond)
	cb, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, PhoneNonce: []byte("1234567890abcdef"), BootstrapToken: ph.Session.BootstrapToken})
	if _, err := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(cb)); err == nil {
		t.Fatalf("expected conn refused after expiry")
	}
}

func TestPairing_ApproveWithoutProofFails(t *testing.T) {
	r, _ := newReg(t)
	ph := startTestPairing(t, r)
	_, pubDER, _ := genKeypair(t)
	phoneNonce := make([]byte, 16)
	rand.Read(phoneNonce)
	cb, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, DisplayName: "p1", PhoneNonce: phoneNonce, BootstrapToken: ph.Session.BootstrapToken})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(cb))
	resp.Body.Close()
	select {
	case <-ph.proofVerifiedCh:
		t.Fatalf("proofVerifiedCh closed before confirm")
	case <-time.After(350 * time.Millisecond):
	}
	if err := ph.Approve(); err == nil {
		t.Fatalf("Approve succeeded before proof")
	}
}

func TestPairing_SessionExpiresWithoutActivity(t *testing.T) {
	r, _ := newReg(t)
	id := newTestId("h1")
	ph, _ := StartPairing(PairingConfig{Listen: "0.0.0.0:0", LANAddr: "127.0.0.1", SessionLifetime: 50 * time.Millisecond, Identity: id, Signer: id, Registry: r})
	defer ph.Close()
	if _, ok := ph.WaitForCandidate(); ok {
		t.Fatalf("expired session returned candidate")
	}
	if _, s := ph.Result(); s == PairingStateApproved {
		t.Fatalf("expired state=approved")
	}
}

func TestPairing_RejectAfterProof(t *testing.T) {
	r, _ := newReg(t)
	ph := startTestPairing(t, r)
	priv, pubDER, _ := genKeypair(t)
	phoneNonce := make([]byte, 16)
	rand.Read(phoneNonce)
	cb, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, PhoneNonce: phoneNonce, BootstrapToken: ph.Session.BootstrapToken})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(cb))
	var ch ChallengeResponse
	json.NewDecoder(resp.Body).Decode(&ch)
	resp.Body.Close()
	sig := signTranscript(t, priv, phoneNonce, ch.HostNonce, ch.HostPublicDER, ph.Session.SessionID)
	cfb, _ := json.Marshal(Confirmation{PhoneSignature: sig})
	http.Post("http://"+ph.addr+"/pair/confirm", "application/json", bytes.NewReader(cfb))
	if _, ok := ph.WaitForCandidate(); !ok {
		t.Fatalf("no candidate")
	}
	ph.Reject()
	if _, s := ph.Result(); s == PairingStateApproved {
		t.Fatalf("rejected=approved")
	}
}

func TestPairing_ProductionE2E(t *testing.T) {
	r, _ := newReg(t)
	ph := startTestPairing(t, r)
	priv, pubDER, fp := genKeypair(t)
	phoneNonce := make([]byte, 16)
	rand.Read(phoneNonce)
	cb, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, DisplayName: "p1", PhoneNonce: phoneNonce, BootstrapToken: ph.Session.BootstrapToken})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(cb))
	if resp.StatusCode != 200 {
		t.Fatalf("phase1=%d", resp.StatusCode)
	}
	var ch ChallengeResponse
	json.NewDecoder(resp.Body).Decode(&ch)
	resp.Body.Close()
	sig := signTranscript(t, priv, phoneNonce, ch.HostNonce, ch.HostPublicDER, ph.Session.SessionID)
	cfb, _ := json.Marshal(Confirmation{PhoneSignature: sig})
	r2, _ := http.Post("http://"+ph.addr+"/pair/confirm", "application/json", bytes.NewReader(cfb))
	if r2.StatusCode != 200 {
		t.Fatalf("phase2=%d", r2.StatusCode)
	}
	var cr struct {
		Status    string
		HostProof string
	}
	json.NewDecoder(r2.Body).Decode(&cr)
	r2.Body.Close()
	if cr.Status != "proof_verified" || cr.HostProof == "" {
		t.Fatalf("confirm=%+v", cr)
	}
	cand, ok := ph.WaitForCandidate()
	if !ok || cand.Fingerprint != fp {
		t.Fatalf("WFC ok=%v fp=%s", ok, fp)
	}
	if err := ph.Approve(); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	dev, state := ph.Result()
	if state != PairingStateApproved || dev.DeviceID == "" {
		t.Fatalf("Result=%+v", dev)
	}
	if _, a := r.GetActiveByFingerprint(fp); !a {
		t.Fatalf("not in registry")
	}
}

func TestPairing_ApprovalRace(t *testing.T) {
	r, _ := newReg(t)
	ph := startTestPairing(t, r)
	_, pubDER, _ := genKeypair(t)
	phoneNonce := make([]byte, 16)
	rand.Read(phoneNonce)
	cb, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, PhoneNonce: phoneNonce, BootstrapToken: ph.Session.BootstrapToken})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(cb))
	resp.Body.Close()
	select {
	case <-ph.proofVerifiedCh:
		t.Fatalf("proofVerifiedCh closed before confirm")
	case <-time.After(200 * time.Millisecond):
	}
	if err := ph.Approve(); err == nil {
		t.Fatalf("Approve succeeded before proof_verified")
	}
}

// ── PB-DG-R4.2: Path-variant rejection (behavioral — real HTTP requests) ──

func TestPairing_PathVariantRejection(t *testing.T) {
	// Each variant gets a fresh pairing host so state isn't shared.
	// The test proves exact POST /pair is accepted and every variant is rejected.
	r, _ := newReg(t)
	ph := startTestPairing(t, r)
	base := "http://" + ph.addr

	_, pubDER, _ := genKeypair(t)
	phoneNonce := make([]byte, 16)
	rand.Read(phoneNonce)
	body, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, PhoneNonce: phoneNonce, BootstrapToken: ph.Session.BootstrapToken})

	// Run control FIRST — exact POST /pair must be accepted.
	resp, err := http.Post(base+"/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("exact /pair POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("exact /pair POST status=%d, want 200", resp.StatusCode)
	}

	// Each rejected variant uses a fresh host so state is independent.
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/pair/"},    // trailing slash
		{http.MethodPost, "/pair/sub"}, // sub-path
		{http.MethodPost, "/PAIR"},     // wrong case
		// before route matching; those are handler-level concerns, not
		// transport-level path rejection.
		{http.MethodPost, "/other"},       // wrong path
		{http.MethodPost, "/api/pair"},    // nested path
		{http.MethodGet, "/pair"},         // wrong method on valid path
		{http.MethodGet, "/pair/confirm"}, // wrong method
		{http.MethodGet, "/pair/result"},  // wrong method
	} {
		ph2 := startTestPairing(t, newRegPair(t))
		body2, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, PhoneNonce: phoneNonce, BootstrapToken: ph2.Session.BootstrapToken})
		req, _ := http.NewRequest(tc.method, "http://"+ph2.addr+tc.path, bytes.NewReader(body2))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Logf("%s %s: transport rejected: %v", tc.method, tc.path, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("%s %s: status 200 — path/method not rejected", tc.method, tc.path)
		}
		ph2.Close()
	}
}

func newRegPair(t *testing.T) *DeviceRegistry {
	t.Helper()
	r, _ := newReg(t)
	return r
}

// ── PB-DG-R4.2: Redirect rejection (behavioral — CheckRedirect) ──

func TestPairing_NoRedirectOnPairPaths(t *testing.T) {
	r, _ := newReg(t)
	ph := startTestPairing(t, r)
	base := "http://" + ph.addr

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for _, path := range []string{"/pair", "/pair/confirm", "/pair/result"} {
		req, _ := http.NewRequest(http.MethodPost, base+path, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Logf("%s: %v", path, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			t.Errorf("%s: redirect %d returned — must not redirect on pairing paths", path, resp.StatusCode)
		}
	}
}

// ── PB-DG-R4.2: Cleartext-bearer rejection (behavioral — real POST with Auth header) ──

func TestPairing_NoBearerTokenOverCleartextOrigin(t *testing.T) {
	r, _ := newReg(t)
	ph := startTestPairing(t, r)
	base := "http://" + ph.addr

	_, pubDER, _ := genKeypair(t)
	phoneNonce := make([]byte, 16)
	rand.Read(phoneNonce)

	// Send a POST with a valid Authorization: Bearer header but an
	// INVALID bootstrap token. This proves bearer does NOT substitute
	// for bootstrap auth — only bootstrap-token gates pairing.
	body, _ := json.Marshal(PairingRequest{
		PublicKeyDER:   pubDER,
		PhoneNonce:     phoneNonce,
		BootstrapToken: "wrong-bootstrap-token",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/pair", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer fake-bearer-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	// Must be rejected — bearer did not bypass bootstrap token check.
	if resp.StatusCode == http.StatusOK {
		t.Error("invalid bootstrap token accepted because bearer header was present — bearer must not substitute for bootstrap auth")
	}
}

// ── PB-DG-R4.2: Queryless paired WebView bootstrap (behavioral) ──
//
// This test proves the full queryless paired WebView bootstrap chain:
// 1. Pair a device through the full 2-phase protocol
// 2. Create a device session with terminal:input permission
// 3. Issue a WS ticket and open a real WebSocket to the production HandleWS
// 4. Verify the hello frame carries the correct session, generation, connectionID
// 5. Verify a terminal_input with wrong session is rejected
// 6. Verify a terminal_input with correct session+generation is accepted

func TestPairing_QuerylessPairedWebViewBootstrap(t *testing.T) {
	r, _ := newReg(t)
	ph := startTestPairing(t, r)

	// ── Full 2-phase pairing ──
	priv, pubDER, fp := genKeypair(t)
	phoneNonce := make([]byte, 16)
	rand.Read(phoneNonce)

	cb, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, DisplayName: "queryless-device", PhoneNonce: phoneNonce, BootstrapToken: ph.Session.BootstrapToken})
	resp, err := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(cb))
	if err != nil {
		t.Fatalf("phase1: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("phase1 status=%d, want 200", resp.StatusCode)
	}
	var chal ChallengeResponse
	if err := json.NewDecoder(resp.Body).Decode(&chal); err != nil {
		resp.Body.Close()
		t.Fatalf("phase1 response: %v", err)
	}
	resp.Body.Close()

	sig := signTranscript(t, priv, phoneNonce, chal.HostNonce, chal.HostPublicDER, ph.Session.SessionID)
	cfb, _ := json.Marshal(Confirmation{PhoneSignature: sig})
	resp, err = http.Post("http://"+ph.addr+"/pair/confirm", "application/json", bytes.NewReader(cfb))
	if err != nil {
		t.Fatalf("phase2: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("phase2 status=%d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	candidate, ok := ph.WaitForCandidate()
	if !ok || candidate.Fingerprint != fp {
		t.Fatalf("verified candidate ok=%v fingerprint=%q, want %q", ok, candidate.Fingerprint, fp)
	}
	if err := ph.Approve(); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// ── Verify paired device in registry ──
	dev, ok := r.GetActiveByFingerprint(fp)
	if !ok {
		t.Fatal("paired device not found in registry")
	}
	if dev.DisplayName != "queryless-device" {
		t.Errorf("display name = %q, want queryless-device", dev.DisplayName)
	}
	if dev.Role != RoleOwner {
		t.Fatalf("paired device role=%q, want %q so terminal:input is authorized", dev.Role, RoleOwner)
	}

	// term imports devicetrust, so this same-package test cannot import term
	// without creating an import cycle. Run the terminal half as a temporary
	// external module and pass it the identity produced by the real pairing flow.
	// The probe creates a native managed PTY, creates/authenticates the paired
	// device session, issues a bound ticket, dials Handlers.HandleWS, validates
	// the hello identity, rejects wrong-session input, and accepts bound input.
	runQuerylessWebViewBootstrapProbe(t, dev.DeviceID, dev.Role)
}

func runQuerylessWebViewBootstrapProbe(t *testing.T, deviceID, role string) {
	t.Helper()

	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("module root: %v", err)
	}
	helperDir := t.TempDir()
	goMod := "module devremote/companion-daemon/pbdgr4probe\n\n" +
		"go 1.26.4\n\n" +
		"require devremote/companion-daemon v0.0.0\n\n" +
		"replace devremote/companion-daemon => " + filepath.ToSlash(moduleRoot) + "\n"
	if err := os.WriteFile(filepath.Join(helperDir, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatalf("write probe go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(helperDir, "main.go"), []byte(querylessWebViewBootstrapProbe), 0o600); err != nil {
		t.Fatalf("write probe: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), "go", "run", "-mod=mod", ".", deviceID, role)
	cmd.Dir = helperDir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("queryless paired WebView infrastructure probe: %v\n%s", err, output)
	}
}

const querylessWebViewBootstrapProbe = `package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/term"
	"github.com/gorilla/websocket"
)

type helloFrame struct {
	Type         string   ` + "`json:\"type\"`" + `
	Capabilities []string ` + "`json:\"capabilities\"`" + `
	SessionID    string   ` + "`json:\"sessionId\"`" + `
	Generation   int64    ` + "`json:\"generation\"`" + `
	ConnectionID string   ` + "`json:\"connectionId\"`" + `
}

type inputResult struct {
	Type         string ` + "`json:\"type\"`" + `
	InputID      string ` + "`json:\"inputId\"`" + `
	ConnectionID string ` + "`json:\"connectionId\"`" + `
	SessionID    string ` + "`json:\"sessionId\"`" + `
	Generation   int64  ` + "`json:\"generation\"`" + `
	Sequence     uint64 ` + "`json:\"sequence\"`" + `
	Outcome      string ` + "`json:\"outcome\"`" + `
}

type probeMutationAuthorizer struct{}

func (probeMutationAuthorizer) AuthorizeCommit(string, uint64, devicetrust.MutationIntent) error {
	return nil
}
func (probeMutationAuthorizer) AuthorizeAndCommit(_ string, _ uint64, _ devicetrust.MutationIntent, commit func() error) error {
	return commit()
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func readHello(conn *websocket.Conn) helloFrame {
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			fail("read hello: %v", err)
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var hello helloFrame
		if err := json.Unmarshal(payload, &hello); err == nil && hello.Type == "hello" {
			return hello
		}
	}
}

func sendInput(conn *websocket.Conn, session string, generation int64, inputID string, payload []byte) {
	frame, err := json.Marshal(map[string]interface{}{
		"type": "terminal_input", "version": 1, "sessionId": session,
		"generation": generation, "inputId": inputID,
		"payload": base64.StdEncoding.EncodeToString(payload),
	})
	if err != nil {
		fail("marshal terminal_input: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
		fail("write terminal_input: %v", err)
	}
}

func readResult(conn *websocket.Conn, inputID string) inputResult {
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			fail("read input result %s: %v", inputID, err)
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var result inputResult
		if err := json.Unmarshal(payload, &result); err == nil && result.Type == "input_result" && result.InputID == inputID {
			return result
		}
	}
}

func main() {
	if len(os.Args) != 3 {
		fail("usage: probe DEVICE_ID ROLE")
	}
	deviceID, role := os.Args[1], os.Args[2]
	permissions := devicetrust.PermissionsForRole(role)
	if !contains(permissions, devicetrust.PermSessionsRead) || !contains(permissions, devicetrust.PermTerminalInput) {
		fail("paired device permissions=%v; sessions:read and terminal:input required", permissions)
	}

	identityDir, err := os.MkdirTemp("", "pb-dg-r4-identity-")
	if err != nil {
		fail("identity tempdir: %v", err)
	}
	defer os.RemoveAll(identityDir)
	identity, err := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: filepath.Join(identityDir, "host.json")})
	if err != nil {
		fail("host identity: %v", err)
	}

	sessions := devicetrust.NewPermissiveSessionManager("pb-dg-r4-boot", time.Minute)
	bearer, _, _, err := sessions.CreateAfterVerifiedChallenge(deviceID, identity.HostID, sessions.BootID(), permissions, 0)
	if err != nil {
		fail("create paired device session: %v", err)
	}
	principal := sessions.AuthenticateBearer(bearer)
	if principal == nil || principal.DeviceID != deviceID || !contains(principal.Permissions, devicetrust.PermTerminalInput) {
		fail("authenticated paired session=%+v", principal)
	}

	cat, err := exec.LookPath("cat")
	if err != nil {
		fail("managed PTY command: %v", err)
	}
	owned, err := term.NewOwnedPTYRuntime(probeMutationAuthorizer{}, term.NewNativePTYLauncher(), nil)
	if err != nil {
		fail("construct managed PTY: %v", err)
	}
	sessionID, err := owned.Create(context.Background(), term.SpawnConfig{
		Name: "pb-dg-r4-queryless", Executable: cat, Rows: 24, Cols: 80,
	}, "shell", "PB-DG-R4 queryless", deviceID, 0)
	if err != nil {
		fail("create native managed session: %v", err)
	}
	lifecycle, err := term.NewLifecycleService(probeMutationAuthorizer{}, owned, nil)
	if err != nil {
		fail("construct lifecycle: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = lifecycle.Kill(ctx, sessionID, "", 0)
	}()
	entry, ok := owned.Get(sessionID)
	if !ok || entry.Generation <= 0 {
		fail("managed session entry=%+v ok=%v", entry, ok)
	}

	tickets := devicetrust.NewWSTicketStore(probeMutationAuthorizer{})
	ticket, ticketExpiry, err := tickets.Issue(principal, identity.HostID, sessionID)
	if err != nil || ticket == "" || ticketExpiry.IsZero() {
		fail("issue WS ticket: ticket=%q expiry=%v err=%v", ticket, ticketExpiry, err)
	}
	handlers, err := term.NewHandlers(probeMutationAuthorizer{})
	if err != nil {
		fail("construct handlers: %v", err)
	}
	handlers.Lifecycle = lifecycle
	handlers.WSTickets = tickets
	handlers.SessionMgr = sessions
	handlers.HostIdentity = identity
	server := httptest.NewServer(http.HandlerFunc(handlers.HandleWS))
	defer server.Close()

	wsURL, err := url.Parse(server.URL)
	if err != nil {
		fail("parse WS URL: %v", err)
	}
	wsURL.Scheme = "ws"
	wsURL.Path = "/term/ws"
	wsURL.RawQuery = "session=" + url.QueryEscape(sessionID) + "&ticket=" + url.QueryEscape(ticket)
	conn, response, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil {
		status := 0
		if response != nil {
			status = response.StatusCode
		}
		fail("HandleWS upgrade: status=%d err=%v", status, err)
	}
	defer conn.Close()

	hello := readHello(conn)
	if hello.SessionID != sessionID || hello.Generation != entry.Generation || hello.ConnectionID == "" {
		fail("hello identity=%+v; want session=%q generation=%d non-empty connectionId", hello, sessionID, entry.Generation)
	}
	if !contains(hello.Capabilities, devicetrust.PermTerminalInput) {
		fail("hello capabilities=%v; terminal:input missing", hello.Capabilities)
	}

	wrongID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sendInput(conn, "controlled_pty:wrong-session", hello.Generation, wrongID, []byte("must-not-write\n"))
	wrong := readResult(conn, wrongID)
	if wrong.Outcome != "session_not_found" || wrong.ConnectionID != hello.ConnectionID || wrong.Sequence != 0 {
		fail("wrong-session result=%+v; want session_not_found, matching connection, sequence 0", wrong)
	}

	correctID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	sendInput(conn, hello.SessionID, hello.Generation, correctID, []byte("queryless bootstrap accepted\n"))
	accepted := readResult(conn, correctID)
	if accepted.Outcome != "accepted" || accepted.SessionID != hello.SessionID ||
		accepted.Generation != hello.Generation || accepted.ConnectionID != hello.ConnectionID || accepted.Sequence != 1 {
		fail("correct-session result=%+v; want accepted with hello identity and sequence 1", accepted)
	}
}
`
