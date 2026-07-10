package devicetrust

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

type testId struct{ pub PublicHostIdentity }

func (t *testId) Public() PublicHostIdentity { return t.pub }

// genKeypair generates ephemeral P-256 key for the phone side of tests.
func genKeypair(t *testing.T) (*ecdsa.PrivateKey, []byte, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal pub: %v", err)
	}
	return priv, pubDER, Fingerprint(pubDER)
}

// signPairingTranscript signs the canonical pairing transcript.
func signTranscript(t *testing.T, priv *ecdsa.PrivateKey, phoneNonce, hostNonce, hostPubDER []byte, sessionID string) []byte {
	t.Helper()
	data := buildPairingTranscript(phoneNonce, hostNonce, hostPubDER, sessionID)
	digest := sha256.Sum256(data)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return sig
}

func TestPairing_Full2PhaseAndApprove(t *testing.T) {
	r, _ := newReg(t)
	priv, pubDER, fp := genKeypair(t)
	id := &testId{pub: PublicHostIdentity{HostID: "h1", Fingerprint: fp}}

	ph, err := StartPairing(PairingConfig{Listen: "0.0.0.0:0", LANAddr: "127.0.0.1", SessionLifetime: 5 * time.Second, Identity: id, Registry: r})
	if err != nil {
		t.Fatalf("StartPairing: %v", err)
	}
	defer ph.Close()

	// Phase 1: Phone submits candidate.
	phoneNonce := make([]byte, 16)
	rand.Read(phoneNonce)
	candBody, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, DisplayName: "p1", PhoneNonce: phoneNonce})
	resp, err := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(candBody))
	if err != nil {
		t.Fatalf("phase 1 POST: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("phase 1 status = %d, want 200", resp.StatusCode)
	}
	var chall ChallengeResponse
	json.NewDecoder(resp.Body).Decode(&chall)
	resp.Body.Close()
	if len(chall.HostNonce) != 32 {
		t.Fatalf("bad host nonce: len=%d", len(chall.HostNonce))
	}

	// Phase 2: Phone signs the transcript and confirms.
	sig := signTranscript(t, priv, phoneNonce, chall.HostNonce, chall.HostPublicDER, ph.Session.SessionID)
	confirmBody, _ := json.Marshal(Confirmation{PhoneSignature: sig})
	resp2, err := http.Post("http://"+ph.addr+"/pair/confirm", "application/json", bytes.NewReader(confirmBody))
	if err != nil {
		t.Fatalf("phase 2 POST: %v", err)
	}
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("phase 2 status = %d, want 200", resp2.StatusCode)
	}
	resp2.Body.Close()

	// IPC side: wait for candidate, then approve.
	cand, ok := ph.WaitForCandidate()
	if !ok || cand.Fingerprint != fp {
		t.Fatalf("WaitForCandidate ok=%v fp=%s want=%s", ok, cand.Fingerprint, fp)
	}
	if err := ph.Approve(); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	dev, state := ph.Result()
	if state != PairingStateApproved || dev.DeviceID == "" {
		t.Fatalf("Result state=%s dev=%+v, want approved", state, dev)
	}
	if _, active := r.GetActiveByFingerprint(fp); !active {
		t.Fatalf("device not in registry after approval")
	}
}

func TestPairing_BadSignatureRejected(t *testing.T) {
	r, _ := newReg(t)
	_, pubDER, _ := genKeypair(t)
	id := &testId{pub: PublicHostIdentity{HostID: "h1"}}
	ph, _ := StartPairing(PairingConfig{Listen: "0.0.0.0:0", LANAddr: "127.0.0.1", SessionLifetime: 5 * time.Second, Identity: id, Registry: r})
	defer ph.Close()

	// Phase 1 submit.
	phoneNonce := make([]byte, 16)
	rand.Read(phoneNonce)
	candBody, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, DisplayName: "p1", PhoneNonce: phoneNonce})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(candBody))
	var chall ChallengeResponse
	json.NewDecoder(resp.Body).Decode(&chall)
	resp.Body.Close()

	// Phase 2 with bad signature.
	confirmBody, _ := json.Marshal(Confirmation{PhoneSignature: []byte("bad-sig")})
	resp2, _ := http.Post("http://"+ph.addr+"/pair/confirm", "application/json", bytes.NewReader(confirmBody))
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad sig status = %d, want 401", resp2.StatusCode)
	}
}

func TestPairing_NonP256Rejected(t *testing.T) {
	r, _ := newReg(t)
	id := &testId{pub: PublicHostIdentity{HostID: "h1"}}
	ph, _ := StartPairing(PairingConfig{Listen: "0.0.0.0:0", LANAddr: "127.0.0.1", SessionLifetime: 5 * time.Second, Identity: id, Registry: r})
	defer ph.Close()

	body, _ := json.Marshal(PairingRequest{PublicKeyDER: []byte("garbage"), PhoneNonce: []byte("1234567890abcdef")})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(body))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-P256 status = %d, want 400", resp.StatusCode)
	}
}

func TestPairing_SecondCandidateRejected(t *testing.T) {
	r, _ := newReg(t)
	priv, pubDER, _ := genKeypair(t)
	_, pub2, _ := genKeypair(t)
	id := &testId{pub: PublicHostIdentity{HostID: "h1"}}
	ph, _ := StartPairing(PairingConfig{Listen: "0.0.0.0:0", LANAddr: "127.0.0.1", SessionLifetime: 5 * time.Second, Identity: id, Registry: r})
	defer ph.Close()

	// First candidate.
	phoneNonce := make([]byte, 16)
	rand.Read(phoneNonce)
	b1, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, DisplayName: "p1", PhoneNonce: phoneNonce})
	r1, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(b1))
	if r1.StatusCode != http.StatusOK {
		t.Fatalf("first candidate status = %d", r1.StatusCode)
	}
	var chall ChallengeResponse
	json.NewDecoder(r1.Body).Decode(&chall)
	r1.Body.Close()

	// Second candidate must fail.
	b2, _ := json.Marshal(PairingRequest{PublicKeyDER: pub2, DisplayName: "p2", PhoneNonce: []byte("1111111111111111")})
	r2, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(b2))
	if r2.StatusCode != http.StatusGone {
		t.Fatalf("second candidate status = %d, want 410", r2.StatusCode)
	}
	r2.Body.Close()

	// First candidate confirms and gets approved.
	sig := signTranscript(t, priv, phoneNonce, chall.HostNonce, chall.HostPublicDER, ph.Session.SessionID)
	confirmBody, _ := json.Marshal(Confirmation{PhoneSignature: sig})
	http.Post("http://"+ph.addr+"/pair/confirm", "application/json", bytes.NewReader(confirmBody))

	if _, ok := ph.WaitForCandidate(); !ok {
		t.Fatalf("first candidate not delivered")
	}
	if err := ph.Approve(); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if len(r.List()) != 1 {
		t.Fatalf("registry has %d devices, want 1", len(r.List()))
	}
}

func TestPairing_ExpiredSessionRejected(t *testing.T) {
	r, _ := newReg(t)
	_, pubDER, _ := genKeypair(t)
	id := &testId{pub: PublicHostIdentity{HostID: "h1"}}
	ph, _ := StartPairing(PairingConfig{Listen: "0.0.0.0:0", LANAddr: "127.0.0.1", SessionLifetime: 50 * time.Millisecond, Identity: id, Registry: r})
	defer ph.Close()

	time.Sleep(200 * time.Millisecond) // well past 50ms expiration
	// After expiry the listener is closed — a connection refusal is correct
	// (terminal state after cleanup). 410 would mean the listener was still
	// open and could inspect the request, which is a narrower window.
	body, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, PhoneNonce: []byte("1234567890abcdef")})
	_, postErr := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(body))
	if postErr == nil {
		t.Fatalf("expected connection refused after expiry, but POST succeeded")
	}
	// Registry must be unchanged (no device registered).
	if len(r.List()) != 0 {
		t.Fatalf("expired session registered a device: %d", len(r.List()))
	}
}

func TestPairing_ApproveWithoutProofFails(t *testing.T) {
	// Approve() before the phone has confirmed (proof_verified) must fail.
	r, _ := newReg(t)
	_, pubDER, _ := genKeypair(t)
	id := &testId{pub: PublicHostIdentity{HostID: "h1"}}
	ph, _ := StartPairing(PairingConfig{Listen: "0.0.0.0:0", LANAddr: "127.0.0.1", SessionLifetime: 5 * time.Second, Identity: id, Registry: r})
	defer ph.Close()

	phoneNonce := make([]byte, 16)
	rand.Read(phoneNonce)
	b1, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, DisplayName: "p1", PhoneNonce: phoneNonce})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(b1))
	resp.Body.Close()

	if _, ok := ph.WaitForCandidate(); !ok {
		t.Fatalf("no candidate")
	}
	// Candidate is in "challenged" state, NOT "proof_verified" — Approve must fail.
	if err := ph.Approve(); err == nil {
		t.Fatalf("Approve without proof succeeded (should require phone confirmation)")
	}
}

func TestPairing_SessionExpiresWithoutActivity(t *testing.T) {
	r, _ := newReg(t)
	id := &testId{pub: PublicHostIdentity{HostID: "h1"}}
	ph, _ := StartPairing(PairingConfig{Listen: "0.0.0.0:0", LANAddr: "127.0.0.1", SessionLifetime: 50 * time.Millisecond, Identity: id, Registry: r})
	defer ph.Close()

	_, ok := ph.WaitForCandidate()
	if ok {
		t.Fatalf("expired session returned a candidate")
	}
	_, state := ph.Result()
	if state == PairingStateApproved {
		t.Fatalf("expired session state = approved")
	}
	// Registry must be unchanged.
	if len(r.List()) != 0 {
		t.Fatalf("expired session registered a device")
	}
}

func TestPairing_RejectAfterProof(t *testing.T) {
	r, _ := newReg(t)
	priv, pubDER, _ := genKeypair(t)
	id := &testId{pub: PublicHostIdentity{HostID: "h1"}}
	ph, _ := StartPairing(PairingConfig{Listen: "0.0.0.0:0", LANAddr: "127.0.0.1", SessionLifetime: 5 * time.Second, Identity: id, Registry: r})
	defer ph.Close()

	phoneNonce := make([]byte, 16)
	rand.Read(phoneNonce)
	candBody, _ := json.Marshal(PairingRequest{PublicKeyDER: pubDER, PhoneNonce: phoneNonce})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(candBody))
	var chall ChallengeResponse
	json.NewDecoder(resp.Body).Decode(&chall)
	resp.Body.Close()

	sig := signTranscript(t, priv, phoneNonce, chall.HostNonce, chall.HostPublicDER, ph.Session.SessionID)
	confirmBody, _ := json.Marshal(Confirmation{PhoneSignature: sig})
	http.Post("http://"+ph.addr+"/pair/confirm", "application/json", bytes.NewReader(confirmBody))

	if _, ok := ph.WaitForCandidate(); !ok {
		t.Fatalf("no candidate")
	}
	ph.Reject()
	_, state := ph.Result()
	if state == PairingStateApproved {
		t.Fatalf("rejected session reports approved")
	}
}
