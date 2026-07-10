package devicetrust

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

type testId struct{ pub PublicHostIdentity }

func (t *testId) Public() PublicHostIdentity { return t.pub }
func (t *testId) Sign(msg []byte) ([]byte, error) {
	// test-only stub (does not produce a real signature; satisfies interface).
	return []byte("test-sig-" + t.pub.HostID), nil
}

func TestPairing_SingleDeviceSuccess(t *testing.T) {
	r, _ := newReg(t)
	pub := genPubDER(t)
	fp := Fingerprint(pub)
	id := &testId{pub: PublicHostIdentity{HostID: "h1", Fingerprint: fp}}

	ph, err := StartPairing(PairingConfig{Listen: "127.0.0.1:0", SessionLifetime: 5 * time.Second, Identity: id, Registry: r})
	if err != nil {
		t.Fatalf("StartPairing: %v", err)
	}
	defer ph.Close()

	body, _ := json.Marshal(PairingRequest{PublicKeyDER: pub, Secret: ph.Session.Secret(), DisplayName: "phone1"})
	resp, err := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("pair request: err=%v status=%d", err, resp.StatusCode)
	}

	if _, waitOK := ph.WaitForCandidate(); !waitOK {
		t.Fatalf("WaitForCandidate failed")
	}
	if err := ph.Approve(nil); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if _, active := r.GetActiveByFingerprint(fp); !active {
		t.Fatalf("device not in registry after pairing")
	}
}

func TestPairing_WrongSecretRejected(t *testing.T) {
	r, _ := newReg(t)
	id := &testId{pub: PublicHostIdentity{HostID: "h1"}}
	ph, _ := StartPairing(PairingConfig{Listen: "127.0.0.1:0", SessionLifetime: 5 * time.Second, Identity: id, Registry: r})
	defer ph.Close()

	body, _ := json.Marshal(PairingRequest{PublicKeyDER: genPubDER(t), Secret: "wrong", DisplayName: "x"})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(body))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong secret status = %d, want 400", resp.StatusCode)
	}
}

func TestPairing_ExpiredSessionRejected(t *testing.T) {
	r, _ := newReg(t)
	id := &testId{pub: PublicHostIdentity{HostID: "h1"}}
	ph, _ := StartPairing(PairingConfig{Listen: "127.0.0.1:0", SessionLifetime: 1 * time.Millisecond, Identity: id, Registry: r})
	defer ph.Close()

	time.Sleep(10 * time.Millisecond) // pass expiration
	body, _ := json.Marshal(PairingRequest{PublicKeyDER: genPubDER(t), Secret: ph.Session.Secret(), DisplayName: "x"})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(body))
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("expired status = %d, want 410", resp.StatusCode)
	}
}

func TestPairing_NonP256Rejected(t *testing.T) {
	r, _ := newReg(t)
	id := &testId{pub: PublicHostIdentity{HostID: "h1"}}
	ph, _ := StartPairing(PairingConfig{Listen: "127.0.0.1:0", SessionLifetime: 5 * time.Second, Identity: id, Registry: r})
	defer ph.Close()

	body, _ := json.Marshal(PairingRequest{PublicKeyDER: []byte("garbage"), Secret: ph.Session.Secret(), DisplayName: "x"})
	resp, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(body))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-P256 status = %d, want 400", resp.StatusCode)
	}
}

func TestPairing_SecondCandidateRejected(t *testing.T) {
	r, _ := newReg(t)
	id := &testId{pub: PublicHostIdentity{HostID: "h1"}}
	ph, _ := StartPairing(PairingConfig{Listen: "127.0.0.1:0", SessionLifetime: 5 * time.Second, Identity: id, Registry: r})
	defer ph.Close()

	pub1 := genPubDER(t)
	b1, _ := json.Marshal(PairingRequest{PublicKeyDER: pub1, Secret: ph.Session.Secret(), DisplayName: "p1"})
	r1, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(b1))
	if r1.StatusCode != http.StatusOK {
		t.Fatalf("first pair status = %d", r1.StatusCode)
	}
	// Second attempt — secret is consumed.
	pub2 := genPubDER(t)
	b2, _ := json.Marshal(PairingRequest{PublicKeyDER: pub2, Secret: ph.Session.Secret(), DisplayName: "p2"})
	r2, _ := http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(b2))
	if r2.StatusCode != http.StatusGone {
		t.Fatalf("second pair status = %d, want 410", r2.StatusCode)
	}
	if _, waitOK := ph.WaitForCandidate(); !waitOK {
		t.Fatalf("no device paired (first should have succeeded)")
	}
	if err := ph.Approve(nil); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if len(r.List()) != 1 {
		t.Fatalf("registry has %d devices, want 1", len(r.List()))
	}
}

func TestPairing_RevokedDeviceRejected(t *testing.T) {
	r, _ := newReg(t)
	pub := genPubDER(t)
	fp := Fingerprint(pub)
	// Pre-register and revoke.
	d, _ := r.Add(pub, "p")
	r.Revoke(d.DeviceID)

	id := &testId{pub: PublicHostIdentity{HostID: "h1", Fingerprint: fp}}
	ph, _ := StartPairing(PairingConfig{Listen: "127.0.0.1:0", SessionLifetime: 5 * time.Second, Identity: id, Registry: r})
	defer ph.Close()

	body, _ := json.Marshal(PairingRequest{PublicKeyDER: pub, Secret: ph.Session.Secret(), DisplayName: "again"})
	_ = body // body not needed again
	http.Post("http://"+ph.addr+"/pair", "application/json", bytes.NewReader(body))
	// Registry.Add returns ErrDeviceRevoked; the handler passes that to the
	// response as {"status":"rejected","error":"..."}. The test just checks the
	// pairing didn't succeed.
	d, ok := ph.Wait(2 * time.Second)
	if ok {
		t.Fatalf("revoked device paired: %+v", d)
	}
}

func TestPairing_SecretDestroyedAfterClose(t *testing.T) {
	r, _ := newReg(t)
	id := &testId{pub: PublicHostIdentity{HostID: "h1"}}
	ph, _ := StartPairing(PairingConfig{Listen: "127.0.0.1:0", SessionLifetime: 5 * time.Second, Identity: id, Registry: r})
	ph.Close()
	if ph.Session.Secret() != "" {
		t.Fatalf("secret not destroyed on close: %q", ph.Session.Secret())
	}
}

func TestPairing_SessionExpiresWithoutActivity(t *testing.T) {
	r, _ := newReg(t)
	id := &testId{pub: PublicHostIdentity{HostID: "h1"}}
	ph, _ := StartPairing(PairingConfig{Listen: "127.0.0.1:0", SessionLifetime: 20 * time.Millisecond, Identity: id, Registry: r})
	defer ph.Close()
	d, ok := ph.Wait(20 * time.Millisecond)
	if ok || d.DeviceID != "" {
		t.Fatalf("unattended session reported success: %+v", d)
	}
}
