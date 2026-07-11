package term

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

func TestPairingIPC_FullSessionFlow(t *testing.T) {
	// Create a real host identity + registry.
	reg := deviceTrustReg(t)

	id, err := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: t.TempDir() + "/id.json"})
	if err != nil {
		t.Fatalf("host identity: %v", err)
	}
	SetPairingContext(id, reg)

	// Simulate the CLI side with a socket pair.
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Run the IPC handler in a goroutine.
	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		handlePairSessionStart(serverConn, 5) // 5-second duration
	}()

	// CLI: decode the session payload.
	dec := json.NewDecoder(clientConn)
	var sess struct {
		OK             bool   `json:"ok"`
		SessionID      string `json:"sessionId"`
		BootstrapToken string `json:"bootstrapToken"`
		Endpoint       string `json:"endpoint"`
		Error          string `json:"error"`
	}
	if err := dec.Decode(&sess); err != nil || !sess.OK {
		t.Fatalf("session decode: err=%v OK=%v error=%s", err, sess.OK, sess.Error)
	}
	if sess.BootstrapToken == "" || sess.Endpoint == "" {
		t.Fatalf("incomplete session payload")
	}

	// Drive the LAN flow to deliver a candidate.
	priv, pubDER, _ := devicetrust.GenKeypair(t)
	phoneNonce := make([]byte, 16)
	phoneNonce[0] = 1
	candBody, _ := json.Marshal(devicetrust.PairingRequest{
		PublicKeyDER:   pubDER,
		DisplayName:    "ipc-test",
		PhoneNonce:     phoneNonce,
		BootstrapToken: sess.BootstrapToken,
	})
	resp, _ := http.Post(sess.Endpoint, "application/json", bytes.NewReader(candBody))
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("phase1 status = %d", resp.StatusCode)
	}
	var chall devicetrust.ChallengeResponse
	json.NewDecoder(resp.Body).Decode(&chall)
	resp.Body.Close()

	// Sign and confirm.
	sig := devicetrust.SignTranscript(t, priv, phoneNonce, chall.HostNonce, chall.HostPublicDER, sess.SessionID)
	confirmBody, _ := json.Marshal(devicetrust.Confirmation{PhoneSignature: sig})
	http.Post(sess.Endpoint+"/confirm", "application/json", bytes.NewReader(confirmBody))

	// Decode the candidate notification.
	var candMsg struct {
		Candidate struct {
			Fingerprint string `json:"fingerprint"`
		} `json:"candidate"`
	}
	if err := dec.Decode(&candMsg); err != nil || candMsg.Candidate.Fingerprint == "" {
		t.Fatalf("candidate decode: %v", err)
	}

	// Send approve.
	writeClientJSON(clientConn, map[string]interface{}{"action": "approve", "version": 1})
	var done struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := dec.Decode(&done); err != nil || done.Error != "" || done.Status != "paired" {
		t.Fatalf("final result: err=%v status=%s error=%s", err, done.Status, done.Error)
	}
	<-handlerDone

	// Phone polling after IPC delivery: /pair/result must still be reachable
	// (grace period is active; no immediate Close from defer ph.Close()).
	resp3, err := http.Get(sess.Endpoint + "/result?session=" + sess.SessionID)
	if err != nil {
		t.Errorf("pair/result after CLI approve: request err=%v", err)
	} else {
		defer resp3.Body.Close()
		var ar map[string]string
		json.NewDecoder(resp3.Body).Decode(&ar)
		if ar["status"] != "approved" {
			t.Errorf("result after CLI approve: %s", ar["status"])
		}
	}

	// Registry must have the device.
	if devs := reg.List(); len(devs) == 0 {
		t.Fatalf("registry empty after pairing")
	}
}

func TestPairingIPC_FragmentedJSON(t *testing.T) {
	// Write session JSON in two chunks; the decoder must work across net.Pipe.
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		payload := []byte(`{"ok":true,"sessionId":"frag","frag":1}` + "\n")
		serverConn.Write(payload[:10]) // fragment 1
		time.Sleep(10 * time.Millisecond)
		serverConn.Write(payload[10:]) // fragment 2
	}()

	dec := json.NewDecoder(clientConn)
	var v struct{ OK bool }
	if err := dec.Decode(&v); err != nil || !v.OK {
		t.Fatalf("fragmented decode: %v", err)
	}
}

func writeClientJSON(c net.Conn, v interface{}) {
	b, _ := json.Marshal(v)
	c.Write(append(b, '\n'))
}

func TestPairing_HostSignFail(t *testing.T) {
	// Test that host sign failure in handleConfirm does not transition state.
	// This exercises the pairing.go side.
	r := deviceTrustReg(t)
	// Create a host identity and a WRONG signer (different key).
	id, _ := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: t.TempDir() + "/host.json"})
	wrongID, _ := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: t.TempDir() + "/wrong.json"})

	ph, err := devicetrust.StartPairing(devicetrust.PairingConfig{
		Listen:          "0.0.0.0:0",
		LANAddr:         "127.0.0.1",
		SessionLifetime: 5 * time.Second,
		Identity:        id,
		Signer:          wrongID, // signer key != identity key → host proof self-verify fails
		Registry:        r,
	})
	if err != nil {
		t.Fatalf("StartPairing: %v", err)
	}
	defer ph.Close()

	priv, pubDER, _ := devicetrust.GenKeypair(t)
	phoneNonce := make([]byte, 16)
	phoneNonce[0] = 2
	candBody, _ := json.Marshal(devicetrust.PairingRequest{
		PublicKeyDER:   pubDER,
		PhoneNonce:     phoneNonce,
		BootstrapToken: ph.Session.BootstrapToken,
	})
	resp, _ := http.Post("http://"+ph.Addr()+"/pair", "application/json", bytes.NewReader(candBody))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("phase1: %d", resp.StatusCode)
	}
	var chall devicetrust.ChallengeResponse
	json.NewDecoder(resp.Body).Decode(&chall)
	resp.Body.Close()

	sig := devicetrust.SignTranscript(t, priv, phoneNonce, chall.HostNonce, chall.HostPublicDER, ph.Session.SessionID)
	confirmBody, _ := json.Marshal(devicetrust.Confirmation{PhoneSignature: sig})
	resp2, _ := http.Post("http://"+ph.Addr()+"/pair/confirm", "application/json", bytes.NewReader(confirmBody))
	if resp2.StatusCode != http.StatusInternalServerError {
		t.Fatalf("host sign mismatch: status=%d, want 500", resp2.StatusCode)
	}

	// Registry must NOT have a device (never approved).
	if len(r.List()) != 0 {
		t.Fatalf("registry was mutated on host-sign failure: %d devices", len(r.List()))
	}
	// Path cleanup.

}

func deviceTrustReg(t *testing.T) *devicetrust.DeviceRegistry {
	t.Helper()
	path := t.TempDir() + "/devices.json"
	r, err := devicetrust.NewDeviceRegistry(&devicetrust.FileDeviceStore{Path: path})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return r
}
