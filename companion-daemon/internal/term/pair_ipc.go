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

// pairingContext holds the daemon-owned trust instances for IPC pair ops and
// the local device-admin ops (list/revoke/audit).
var (
	pairingIdentity *devicetrust.HostIdentity
	pairingRegistry *devicetrust.DeviceRegistry
	pairingSessions *devicetrust.DeviceSessionManager // M2.5-5: revoke → session invalidation
	pairingAudit    devicetrust.AuditLog              // M2.5-5: local audit (nil ⇒ none)
	qrPairingBridge QRPairBridge
	pairingMu       sync.Mutex
)

// QRPairSession is the non-authoritative metadata supplied to the cmd-layer
// QR bridge. Device registration and proof verification remain PairingHost's
// responsibility.
type QRPairSession struct {
	SessionID      string
	HostID         string
	HostPublicKey  string
	Endpoint       string
	ExpiresAt      time.Time
	BootstrapValue string
}

// PairingRequest is the mobile's Phase 1 candidate body. Legacy fields are
// forwarded to PairingHost; QR metadata fields are verified by the bridge
// BEFORE the candidate reaches PairingHost.
type PairingRequest struct {
	PublicKeyDER   []byte `json:"publicKey"`
	DisplayName    string `json:"displayName"`
	PhoneNonce     []byte `json:"phoneNonce"`
	BootstrapToken string `json:"bootstrapToken"`
	// QR metadata — echoed by mobile from the QR code. Verified by bridge
	// before PairingHost sees the candidate.
	QRHostID       string `json:"qrHostId,omitempty"`
	QRDaemonBootID string `json:"qrDaemonBootId,omitempty"`
	QRChallengeID  string `json:"qrChallengeId,omitempty"`
	QRExpiresAt    string `json:"qrExpiresAt,omitempty"`
}

type QRPairMetadata struct {
	ProtocolVersion int
	Origin          string
	HostPublicKey   string
	DaemonBootID    string
	ChallengeID     string
	ExpiresAt       time.Time
	BootstrapValue  string
}

type QRPairBridge interface {
	Begin(QRPairSession) (QRPairMetadata, error)
	// Verify checks that the provided QR metadata matches the stored binding
	// for this session. hostId/daemonBootId/challengeId/expiresAt are the
	// values the mobile echoed back (from the QR code). Mismatch consumes
	// the one-shot challenge.
	Verify(sessionID, hostID, daemonBootID, challengeID, expiresAt string) error
	Consume(sessionID string) error
	Cancel(sessionID string)
}

func SetQRPairBridge(bridge QRPairBridge) {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	qrPairingBridge = bridge
}

// SetPairingContext stores the daemon-owned trust instances for IPC pair ops.
func SetPairingContext(id *devicetrust.HostIdentity, reg *devicetrust.DeviceRegistry) {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	pairingIdentity = id
	pairingRegistry = reg
}

// SetDeviceAdminContext stores the session manager and audit log used by the
// local device-admin IPC operations. Separate from SetPairingContext so the
// pairing flow and its tests need no session manager.
func SetDeviceAdminContext(sessions *devicetrust.DeviceSessionManager, audit devicetrust.AuditLog) {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	pairingSessions = sessions
	pairingAudit = audit
}

// handlePairSessionStart owns the IPC connection for the full pairing
// lifecycle. durationSecs comes from the already-decoded IPC JSON (the parent
// decoder consumed the JSON line — we must NOT decode again).
func handlePairSessionStart(conn net.Conn, durationSecs int) {
	defer conn.Close()

	pairingMu.Lock()
	id := pairingIdentity
	reg := pairingRegistry
	bridge := qrPairingBridge
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
		Signer:          id, // HostIdentity implements HostSigner (Sign method)
		Registry:        reg,
	})
	if err != nil {
		writeIPC(conn, map[string]string{"error": "pairing start failed: " + err.Error()})
		return
	}
	if bridge == nil {
		ph.Close()
		writeIPC(conn, map[string]string{"error": "QR pairing bridge not configured"})
		return
	}
	metadata, err := bridge.Begin(QRPairSession{
		SessionID:      ph.Session.SessionID,
		HostID:         ph.Session.HostID,
		HostPublicKey:  ph.Session.HostPubKeyB64,
		Endpoint:       ph.Session.Endpoint,
		ExpiresAt:      ph.Session.ExpiresAt,
		BootstrapValue: ph.Session.BootstrapToken,
	})
	if err != nil {
		ph.Close()
		writeIPC(conn, map[string]string{"error": "QR pairing bridge start failed: " + err.Error()})
		return
	}
	// NO deferred Close() — the grace period in Approve()/Reject() owns the
	// close after a terminal result. Early exits below close immediately.

	// 1) Send session payload immediately (QR data).
	sess := ph.Session
	writeIPC(conn, map[string]interface{}{
		"ok":              true,
		"sessionId":       sess.SessionID,
		"hostId":          sess.HostID,
		"fingerprint":     sess.Fingerprint,
		"hostPubKey":      sess.HostPubKeyB64,
		"bootstrapToken":  sess.BootstrapToken,
		"endpoint":        sess.Endpoint,
		"expiresAt":       sess.ExpiresAt.Format(time.RFC3339),
		"protocolVersion": metadata.ProtocolVersion,
		"origin":          metadata.Origin,
		"daemonBootId":    metadata.DaemonBootID,
		"challengeId":     metadata.ChallengeID,
	})

	// 2) Wait for candidate, then verify QR binding through the bridge.
	cand, gotCand := ph.WaitForCandidate()
	if !gotCand {
		bridge.Cancel(sess.SessionID)
		writeIPC(conn, map[string]string{"error": "session ended without a candidate"})
		ph.Close()
		return
	}
	if err := bridge.Verify(sess.SessionID, sess.HostID, metadata.DaemonBootID, metadata.ChallengeID, metadata.ExpiresAt.Format(time.RFC3339)); err != nil {
		writeIPC(conn, map[string]string{"error": "QR binding verification failed: " + err.Error()})
		ph.Reject()
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
		bridge.Cancel(sess.SessionID)
		writeIPC(conn, map[string]string{"error": "invalid decision: " + err.Error()})
		return // Reject() schedules a 10s grace period; the timer will close
	}

	if decision.Action != "approve" {
		ph.Reject()
		bridge.Cancel(sess.SessionID)
		writeIPC(conn, map[string]interface{}{"status": "rejected", "state": string(devicetrust.PairingStateRejected)})
		return
	}

	// 5) Approve → register device.
	if err := bridge.Consume(sess.SessionID); err != nil {
		ph.Reject()
		writeIPC(conn, map[string]string{"error": "QR challenge rejected: " + err.Error()})
		return
	}
	if err := ph.Approve(); err != nil {
		bridge.Cancel(sess.SessionID)
		writeIPC(conn, map[string]string{"error": "approval failed: " + err.Error()})
		ph.Close() // Approve failed — no grace period; close immediately
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
