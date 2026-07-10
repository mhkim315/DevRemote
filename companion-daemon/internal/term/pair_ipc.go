package term

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

// pairingContext holds the daemon-owned trust instances for IPC pair ops.
var (
	pairingIdentity *devicetrust.HostIdentity
	pairingRegistry *devicetrust.DeviceRegistry
	pairingMu       sync.Mutex
)

// SetPairingContext stores the daemon-owned trust instances for IPC pair ops.
func SetPairingContext(id *devicetrust.HostIdentity, reg *devicetrust.DeviceRegistry) {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	pairingIdentity = id
	pairingRegistry = reg
}

// handlePairSessionStart owns the IPC connection for the full pairing
// lifecycle. durationSecs comes from the already-decoded IPC JSON (the parent
// decoder consumed the JSON line — we must NOT decode again).
func handlePairSessionStart(conn net.Conn, durationSecs int) {
	defer conn.Close()

	pairingMu.Lock()
	id := pairingIdentity
	reg := pairingRegistry
	pairingMu.Unlock()

	if id == nil || reg == nil {
		writeIPC(conn, map[string]string{"error": "device trust not configured"})
		return
	}

	d := time.Duration(durationSecs) * time.Second
	if d <= 0 || d > 10*time.Minute {
		d = 2 * time.Minute
	}

	ph, err := devicetrust.StartPairing(devicetrust.PairingConfig{
		Listen:          "0.0.0.0:0",
		LANAddr:         "", // auto-detect
		SessionLifetime: d,
		Identity:        id,
		Registry:        reg,
	})
	if err != nil {
		writeIPC(conn, map[string]string{"error": "pairing start failed: " + err.Error()})
		return
	}
	defer ph.Close()

	// 1) Send session payload immediately (QR data).
	sess := ph.Session
	writeIPC(conn, map[string]interface{}{
		"ok":          true,
		"sessionId":   sess.SessionID,
		"hostId":      sess.HostID,
		"fingerprint": sess.Fingerprint,
		"hostPubKey":  sess.HostPubKeyB64,
		"endpoint":    sess.Endpoint,
		"expiresAt":   sess.ExpiresAt.Format(time.RFC3339),
	})

	// 2) Wait for candidate.
	cand, gotCand := ph.WaitForCandidate()
	if !gotCand {
		writeIPC(conn, map[string]string{"error": "session ended without a candidate"})
		return
	}

	// 3) Notify CLI of candidate (fingerprint for display + hostNonce).
	writeIPC(conn, map[string]interface{}{
		"candidate": map[string]interface{}{
			"fingerprint": cand.Fingerprint,
			"displayName": cand.DisplayName,
			"phoneNonce":  hex.EncodeToString(cand.PhoneNonce),
			"hostNonce":   hex.EncodeToString(cand.HostNonce),
			"hostPubKey":  sess.HostPubKeyB64,
		},
	})

	// 4) Read CLI's approve/reject decision.
	var decision struct {
		Action  string `json:"action"` // "approve" or "reject"
		Version int    `json:"version"`
	}
	dec2 := json.NewDecoder(conn)
	if err := dec2.Decode(&decision); err != nil {
		ph.Reject()
		writeIPC(conn, map[string]string{"error": "invalid decision: " + err.Error()})
		return
	}

	if decision.Action != "approve" {
		ph.Reject()
		writeIPC(conn, map[string]interface{}{"status": "rejected", "state": string(devicetrust.PairingStateRejected)})
		return
	}

	// 5) Approve → register device.
	if err := ph.Approve(); err != nil {
		writeIPC(conn, map[string]string{"error": "approval failed: " + err.Error()})
		return
	}

	dev, state := ph.Result()
	writeIPC(conn, map[string]interface{}{
		"status": "paired",
		"state":  string(state),
		"device": map[string]interface{}{
			"deviceId":    dev.DeviceID,
			"fingerprint": dev.Fingerprint,
			"role":        dev.Role,
		},
	})
}

func writeIPC(conn net.Conn, v interface{}) {
	b, _ := json.Marshal(v)
	conn.Write(append(b, '\n'))
}

// handlePairOp dispatches a pairing IPC operation. For pair-start it calls
// handlePairSession which owns the conn for the full lifecycle. The duration
// has already been decoded from the IPC JSON so handlePairSession does NOT
// re-decode it.
func handlePairOp(conn net.Conn, op string, durationSecs int, phoneSig []byte) {
	if op == "pair-start" {
		handlePairSessionStart(conn, durationSecs)
		return
	}
	writeIPC(conn, map[string]string{"error": fmt.Sprintf("unknown pairing operation: %s", op)})
}
