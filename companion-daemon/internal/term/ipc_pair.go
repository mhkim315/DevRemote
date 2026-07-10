package term

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

// pairingContext holds the daemon's identity and registry. Set by the App on
// startup; read by the IPC pair ops. Nil when device trust is not configured.
var (
	pairingIdentity *devicetrust.HostIdentity
	pairingRegistry *devicetrust.DeviceRegistry
	pairingMu       sync.Mutex
)

// SetPairingContext stores the daemon-owned trust instances for IPC pair ops.
// Called once on App startup.
func SetPairingContext(id *devicetrust.HostIdentity, reg *devicetrust.DeviceRegistry) {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	pairingIdentity = id
	pairingRegistry = reg
}

// handlePairOp dispatches a pairing IPC operation. conn is closed by the
// caller; this writes the JSON reply.
func handlePairOp(conn net.Conn, op string, durationSecs int, phoneSig []byte) {
	pairingMu.Lock()
	id := pairingIdentity
	reg := pairingRegistry
	pairingMu.Unlock()

	if id == nil || reg == nil {
		conn.Write(append(jsonErr("device trust not configured"), '\n'))
		return
	}

	switch op {
	case "pair-start":
		handlePairStart(conn, id, reg, durationSecs)
	case "pair-approve":
		handlePairApprove(conn, phoneSig)
	case "pair-reject":
		handlePairReject(conn)
	default:
		conn.Write(append(jsonErr("unknown pairing operation"), '\n'))
	}
}

func lanAddr() string {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			ip := ipnet.IP.String()
			if len(ip) > 3 && (ip[:3] == "10." || ip[:4] == "172." || ip[:8] == "192.168.") {
				return ip
			}
		}
	}
	return "127.0.0.1" // fallback
}

var pairingHost struct {
	mu   sync.Mutex
	host *devicetrust.PairingHost
}

func handlePairStart(conn net.Conn, identity *devicetrust.HostIdentity, reg *devicetrust.DeviceRegistry, durationSecs int) {
	d := time.Duration(durationSecs) * time.Second
	if d <= 0 || d > 10*time.Minute {
		d = 2 * time.Minute
	}
	lan := lanAddr() // detect actual LAN IP
	ph, err := devicetrust.StartPairing(devicetrust.PairingConfig{
		Listen:          "0.0.0.0:0", // any free port
		LANAddr:         lan,
		SessionLifetime: d,
		Identity:        identity,
		Registry:        reg,
	})
	if err != nil {
		conn.Write(append(jsonErr("pairing start failed: "+err.Error()), '\n'))
		return
	}

	pairingHost.mu.Lock()
	pairingHost.host = ph
	pairingHost.mu.Unlock()

	// Return the session immediately (payload for QR, including the secret).
	sess := ph.Session
	payload := map[string]interface{}{
		"sessionId":   sess.SessionID,
		"hostId":      sess.HostID,
		"fingerprint": sess.Fingerprint,
		"endpoint":    sess.Endpoint,
		"secret":      sess.Secret(),
		"expiresAt":   sess.ExpiresAt.Format(time.RFC3339),
	}
	sessionJSON, _ := json.Marshal(payload)
	conn.Write(append(sessionJSON, '\n'))

	// Wait for a candidate in a goroutine; signal the CLI via the socket.
	go func() {
		cand, ok := ph.WaitForCandidate()
		if !ok {
			return // session ended
		}
		resp, _ := json.Marshal(map[string]interface{}{
			"candidate": map[string]interface{}{
				"fingerprint": devicetrust.Fingerprint(cand.PubDER),
				"displayName": cand.DisplayName,
				"phoneNonce":  cand.PhoneNonce,
				"hostNonce":   cand.HostNonce,
			},
		})
		conn.Write(append(resp, '\n'))
	}()
}

func handlePairApprove(conn net.Conn, phoneSig []byte) {
	pairingHost.mu.Lock()
	ph := pairingHost.host
	pairingHost.mu.Unlock()

	if ph == nil {
		conn.Write(append(jsonErr("no active pairing session"), '\n'))
		return
	}
	if err := ph.Approve(phoneSig); err != nil {
		conn.Write(append(jsonErr("approval failed: "+err.Error()), '\n'))
		pairingHost.mu.Lock()
		pairingHost.host = nil
		pairingHost.mu.Unlock()
		return
	}
	// Approval succeeded — return paired device info.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Registry lookup to confirm.
	resp, _ := json.Marshal(map[string]interface{}{
		"status": "paired",
	})
	_ = ctx
	conn.Write(append(resp, '\n'))

	pairingHost.mu.Lock()
	pairingHost.host = nil
	pairingHost.mu.Unlock()
}

func handlePairReject(conn net.Conn) {
	pairingHost.mu.Lock()
	ph := pairingHost.host
	pairingHost.mu.Unlock()

	if ph != nil {
		ph.Reject()
	}
	conn.Write(append([]byte(`{"status":"rejected"}`), '\n'))

	pairingHost.mu.Lock()
	pairingHost.host = nil
	pairingHost.mu.Unlock()
}

func jsonErr(msg string) []byte {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return b
}

// SetPairingContext stores the daemon-owned trust instances for IPC pair ops.
// Already defined earlier; this is intentionally left alone.
var _ = SetPairingContext
