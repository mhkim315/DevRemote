package devicetrust

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"time"
)

// GenKeypair generates an ephemeral P-256 keypair for tests.
func GenKeypair(t interface{ Fatal(...interface{}) }) (*ecdsa.PrivateKey, []byte, string) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	return priv, pubDER, Fingerprint(pubDER)
}

// SignTranscript signs the canonical pairing transcript for tests.
func SignTranscript(t interface{ Fatal(...interface{}) }, priv *ecdsa.PrivateKey,
	phoneNonce, hostNonce, hostPubDER []byte, sessionID string) []byte {
	data := buildPairingTranscript(phoneNonce, hostNonce, hostPubDER, sessionID)
	digest := sha256.Sum256(data)
	sig, _ := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	return sig
}

// NewPermissiveSessionManager creates a DeviceSessionManager with a GetAuth that
// accepts any device at epoch 0. For tests that need session CRUD without real
// device authorization.
func NewPermissiveSessionManager(bootID string, lifetime time.Duration) *DeviceSessionManager {
	m := NewDeviceSessionManager(bootID, lifetime)
	m.GetAuth = func(deviceID string) AuthorizationState {
		return AuthorizationState{Epoch: 0, Active: true}
	}
	return m
}
