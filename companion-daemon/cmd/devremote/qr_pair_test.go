package main

import (
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/term"
)

func TestQRPairBridgeSingleUseChallenge(t *testing.T) {
	bridge := newQRPairBridge(devicetrust.NewChallengeStore(), "boot-1")
	session := term.QRPairSession{
		SessionID: "session-1", HostID: "host-1", HostPublicKey: "host-key",
		Endpoint: "http://192.168.1.2:9171/pair", ExpiresAt: time.Now().Add(time.Minute), BootstrapValue: "one-time-value",
	}
	metadata, err := bridge.Begin(session)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if metadata.ProtocolVersion != 1 || metadata.DaemonBootID != "boot-1" || metadata.ChallengeID == "" || metadata.BootstrapValue != session.BootstrapValue {
		t.Fatalf("unexpected QR metadata: %+v", metadata)
	}
	if err := bridge.Consume(session.SessionID); err != nil {
		t.Fatalf("first Consume: %v", err)
	}
	if err := bridge.Consume(session.SessionID); err == nil {
		t.Fatal("replayed QR challenge was accepted")
	}
}
