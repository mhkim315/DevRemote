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
// production pairing flow. It does not register devices or issue sessions;
// PairingHost and DeviceRegistry remain those authorities.
type qrPairBridge struct {
	mu         sync.Mutex
	challenges *devicetrust.ChallengeStore
	bootID     string
	pending    map[string]qrPending
}

type qrPending struct {
	challengeID []byte
	deviceID    string
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
	b.pending[session.SessionID] = qrPending{challengeID: challengeID, deviceID: pendingID}
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

func (b *qrPairBridge) Cancel(sessionID string) {
	// Consume invalidates a pending challenge atomically. There is no device
	// record or bearer on this path.
	_ = b.Consume(sessionID)
}
