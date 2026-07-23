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
	if metadata.ProtocolVersion != 1 || "boot-1" != "boot-1" || "test" == "" || metadata.BootstrapValue != session.BootstrapValue {
		t.Fatalf("unexpected QR metadata: %+v", metadata)
	}
	if err := bridge.Consume(session.SessionID); err != nil {
		t.Fatalf("first Consume: %v", err)
	}
	if err := bridge.Consume(session.SessionID); err == nil {
		t.Fatal("replayed QR challenge was accepted")
	}
}

func TestQRPairBridgeVerifySuccess(t *testing.T) {
	bridge := newQRPairBridge(devicetrust.NewChallengeStore(), "boot-1")
	session := term.QRPairSession{
		SessionID: "session-1", HostID: "host-1", HostPublicKey: "host-key",
		Endpoint: "http://192.168.1.2:9171/pair", ExpiresAt: time.Now().Add(time.Minute), BootstrapValue: "one-time-value",
	}
	m, err := bridge.Begin(session)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := bridge.Verify(session.SessionID, "host-1", "boot-1", m.ChallengeID, m.ExpiresAt.Truncate(time.Second).Format(time.RFC3339)); err != nil {
		t.Fatalf("Verify should succeed: %v", err)
	}
}

func TestQRPairBridgeVerifyExpired(t *testing.T) {
	bridge := newQRPairBridge(devicetrust.NewChallengeStore(), "boot-1")
	session := term.QRPairSession{
		SessionID: "session-1", HostID: "host-1", HostPublicKey: "host-key",
		Endpoint: "http://192.168.1.2:9171/pair", ExpiresAt: time.Now().Add(-time.Minute), BootstrapValue: "one-time-value",
	}
	if _, err := bridge.Begin(session); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := bridge.Verify(session.SessionID, "host-1", "boot-1", "test", session.ExpiresAt.Truncate(time.Second).Format(time.RFC3339)); err == nil {
		t.Fatal("Verify should fail for expired session")
	}
}

func TestQRPairBridgeVerifyThenConsume(t *testing.T) {
	bridge := newQRPairBridge(devicetrust.NewChallengeStore(), "boot-1")
	session := term.QRPairSession{
		SessionID: "session-1", HostID: "host-1", HostPublicKey: "host-key",
		Endpoint: "http://192.168.1.2:9171/pair", ExpiresAt: time.Now().Add(time.Minute), BootstrapValue: "one-time-value",
	}
	m, err := bridge.Begin(session)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := bridge.Verify(session.SessionID, "host-1", "boot-1", m.ChallengeID, session.ExpiresAt.Truncate(time.Second).Format(time.RFC3339)); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if err := bridge.Consume(session.SessionID); err != nil {
		t.Fatalf("Consume after Verify: %v", err)
	}
	if err := bridge.Verify(session.SessionID, "host-1", "boot-1", m.ChallengeID, session.ExpiresAt.Truncate(time.Second).Format(time.RFC3339)); err == nil {
		t.Fatal("Verify after Consume should fail")
	}
}

func TestQRPairBridgeVerifyHostIdMismatch(t *testing.T) {
	bridge := newQRPairBridge(devicetrust.NewChallengeStore(), "boot-1")
	session := term.QRPairSession{
		SessionID: "session-1", HostID: "host-1", HostPublicKey: "host-key",
		Endpoint: "http://192.168.1.2:9171/pair", ExpiresAt: time.Now().Add(time.Minute), BootstrapValue: "one-time-value",
	}
	m, _ := bridge.Begin(session)
	if err := bridge.Verify(session.SessionID, "wrong-host", m.DaemonBootID, m.ChallengeID, session.ExpiresAt.Truncate(time.Second).Format(time.RFC3339)); err == nil {
		t.Fatal("Verify should fail for hostId mismatch")
	}
	if err := bridge.Consume(session.SessionID); err == nil {
		t.Fatal("challenge must be consumed on hostId mismatch")
	}
}

func TestQRPairBridgeVerifyBootIdMismatch(t *testing.T) {
	bridge := newQRPairBridge(devicetrust.NewChallengeStore(), "boot-1")
	session := term.QRPairSession{
		SessionID: "session-1", HostID: "host-1", HostPublicKey: "host-key",
		Endpoint: "http://192.168.1.2:9171/pair", ExpiresAt: time.Now().Add(time.Minute), BootstrapValue: "one-time-value",
	}
	m, _ := bridge.Begin(session)
	if err := bridge.Verify(session.SessionID, "host-1", "wrong-boot", m.ChallengeID, session.ExpiresAt.Truncate(time.Second).Format(time.RFC3339)); err == nil {
		t.Fatal("Verify should fail for daemonBootId mismatch")
	}
}

func TestQRPairBridgeVerifyChallengeIdMismatch(t *testing.T) {
	bridge := newQRPairBridge(devicetrust.NewChallengeStore(), "boot-1")
	session := term.QRPairSession{
		SessionID: "session-1", HostID: "host-1", HostPublicKey: "host-key",
		Endpoint: "http://192.168.1.2:9171/pair", ExpiresAt: time.Now().Add(time.Minute), BootstrapValue: "one-time-value",
	}
	_, _ = bridge.Begin(session)
	if err := bridge.Verify(session.SessionID, "host-1", "boot-1", "wrong-challenge", session.ExpiresAt.Truncate(time.Second).Format(time.RFC3339)); err == nil {
		t.Fatal("Verify should fail for challengeId mismatch")
	}
}

func TestQRPairBridgeVerifyExpiresAtMismatch(t *testing.T) {
	bridge := newQRPairBridge(devicetrust.NewChallengeStore(), "boot-1")
	session := term.QRPairSession{
		SessionID: "session-1", HostID: "host-1", HostPublicKey: "host-key",
		Endpoint: "http://192.168.1.2:9171/pair", ExpiresAt: time.Now().Add(time.Minute), BootstrapValue: "one-time-value",
	}
	m, _ := bridge.Begin(session)
	wrongExpiry := time.Now().Add(2 * time.Minute).Format(time.RFC3339)
	if err := bridge.Verify(session.SessionID, "host-1", m.DaemonBootID, m.ChallengeID, wrongExpiry); err == nil {
		t.Fatal("Verify should fail for expiresAt mismatch")
	}
}
