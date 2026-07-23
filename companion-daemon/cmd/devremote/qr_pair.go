package main

import (
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/term"
)

// qrPairBridge adds QR bootstrap metadata and a one-time gate around the
// production pairing flow. It mediates between mobile (which sends QR
// metadata in request body) and PairingHost (which sees only legacy fields).
// The bridge does not register devices or issue sessions; PairingHost and
// DeviceRegistry remain those authorities.
type qrPairBridge struct {
	mu         sync.Mutex
	challenges *devicetrust.ChallengeStore
	bootID     string
	pending    map[string]qrPending
}

type qrPending struct {
	challengeID []byte
	deviceID    string
	metadata    qrMetadata // stored at Begin, validated by Verify
}

type qrMetadata struct {
	hostID       string
	daemonBootID string
	challengeID  string
	expiresAt    time.Time
}

func newQRPairBridge(challenges *devicetrust.ChallengeStore, bootID string) *qrPairBridge {
	return &qrPairBridge{challenges: challenges, bootID: bootID, pending: make(map[string]qrPending)}
}

func (b *qrPairBridge) Begin(session term.QRPairSession) (term.QRPairMetadata, error) {
	if b == nil || b.challenges == nil || b.bootID == "" || session.SessionID == "" || session.HostID == "" || session.HostPublicKey == "" || session.BootstrapValue == "" || session.ExpiresAt.IsZero() {
		return term.QRPairMetadata{}, fmt.Errorf("invalid QR pairing bridge input")
	}
	challengeID, err := devicetrust.NewChallengeID()
	if err != nil {
		return term.QRPairMetadata{}, fmt.Errorf("new QR challenge: %w", err)
	}
	// The synthetic ID is internal bridge bookkeeping only. It cannot be used
	// for device authentication; the existing PairingHost proof and explicit
	// operator approval still bind the eventual real device identity.
	pendingID := "qr-pending:" + session.SessionID
	if _, err := b.challenges.Insert(&devicetrust.PendingChallenge{
		ChallengeID:  challengeID,
		DeviceID:     pendingID,
		HostID:       session.HostID,
		DaemonBootID: b.bootID,
		IssuedAt:     time.Now().UTC(),
		ExpiresAt:    session.ExpiresAt,
	}); err != nil {
		return term.QRPairMetadata{}, fmt.Errorf("store QR challenge: %w", err)
	}
	b.mu.Lock()
	b.pending[session.SessionID] = qrPending{
		challengeID: challengeID, deviceID: pendingID,
		metadata: qrMetadata{
			hostID:       session.HostID,
			daemonBootID: b.bootID,
			challengeID:  hex.EncodeToString(challengeID),
			expiresAt:    session.ExpiresAt,
		},
	}
	b.mu.Unlock()
	return term.QRPairMetadata{
		ProtocolVersion: 1,
		Origin:          session.Endpoint,
		HostPublicKey:   session.HostPublicKey,
		DaemonBootID:    b.bootID,
		ChallengeID:     hex.EncodeToString(challengeID),
		ExpiresAt:       session.ExpiresAt,
		BootstrapValue:  session.BootstrapValue,
	}, nil
}

func (b *qrPairBridge) Consume(sessionID string) error {
	b.mu.Lock()
	pending, ok := b.pending[sessionID]
	if ok {
		delete(b.pending, sessionID)
	}
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("QR challenge not found")
	}
	if _, err := b.challenges.Consume(pending.challengeID, []byte(pending.deviceID)); err != nil {
		return fmt.Errorf("consume QR challenge: %w", err)
	}
	return nil
}

// Verify checks that the provided QR metadata matches the stored binding
// for this session. All 4 fields (hostId, daemonBootId, challengeId,
// expiresAt) must match. Mismatch on any field consumes the one-shot
// challenge (fail closed). Called by pair_ipc after a candidate arrives
// to validate the mobile echoed the correct QR data.
func (b *qrPairBridge) Verify(sessionID, hostID, daemonBootID, challengeID, expiresAt string) error {
	b.mu.Lock()
	pending, ok := b.pending[sessionID]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("QR binding: session not found")
	}
	m := pending.metadata
	if hostID != m.hostID {
		b.Cancel(sessionID)
		return fmt.Errorf("QR binding: hostId mismatch")
	}
	if daemonBootID != m.daemonBootID {
		b.Cancel(sessionID)
		return fmt.Errorf("QR binding: daemonBootId mismatch")
	}
	if challengeID != m.challengeID {
		b.Cancel(sessionID)
		return fmt.Errorf("QR binding: challengeId mismatch")
	}
	if t, err := time.Parse(time.RFC3339, expiresAt); err != nil || !t.Equal(m.expiresAt.Truncate(time.Second)) {
		b.Cancel(sessionID)
		return fmt.Errorf("QR binding: expiresAt mismatch")
	}
	if time.Now().After(m.expiresAt) {
		b.Cancel(sessionID)
		return fmt.Errorf("QR binding: expired")
	}
	return nil
}

func (b *qrPairBridge) Cancel(sessionID string) {
	// Consume invalidates a pending challenge atomically. There is no device
	// record or bearer on this path.
	_ = b.Consume(sessionID)
}
