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
	"net"
	"net/http"
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

func TestPairing_BadSignatureRejected(t *testing.T) {
	r, _ := newReg(t)
	ph := startTestPairing(t, r)
	_, pubDER, _ := genKeypair(t)
	phoneNonce := make([]byte, 16)
	rand.Read(phoneNonce)
	cb, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, DisplayName: "p1", PhoneNonce: phoneNonce, BootstrapToken: ph.Session.BootstrapToken})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(cb))
	var ch ChallengeResponse
	json.NewDecoder(resp.Body).Decode(&ch)
	resp.Body.Close()
	cfb, _ := json.Marshal(Confirmation{PhoneSignature: []byte("bad")})
	r2, _ := http.Post("http://"+ph.addr+"/pair/confirm", "application/json", bytes.NewReader(cfb))
	if r2.StatusCode != 401 {
		t.Fatalf("bad sig=%d", r2.StatusCode)
	}
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

func writeJSON(c net.Conn, v interface{}) { b, _ := json.Marshal(v); c.Write(append(b, '\n')) }
