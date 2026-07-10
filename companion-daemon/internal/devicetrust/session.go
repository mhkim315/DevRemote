package devicetrust

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Principal is the device identity extracted from a valid session token.
// It is handler-independent — M2.5-4 middleware reads this to authorize
// REST and WebSocket requests. No raw token is exposed here.
type Principal struct {
	DeviceID    string
	Permissions []string
	SessionID   string
	HostID      string
	AuthTime    time.Time
}

// DeviceSession is the daemon-side record for an issued bearer token.
// The raw token is returned exactly once; only its SHA-256 digest is stored.
type DeviceSession struct {
	TokenDigest string    `json:"-"` // SHA-256 hex of the raw 32-byte token
	SessionID   string    `json:"-"` // internal correlation id
	DeviceID    string    `json:"-"`
	HostID      string    `json:"-"`
	BootID      string    `json:"-"`
	Permissions []string  `json:"-"`
	IssuedAt    time.Time `json:"-"`
	ExpiresAt   time.Time `json:"-"`
}

// DeviceSessionManager owns all active device sessions. It is in-memory
// only — daemon restart invalidates every session (new boot ID).
type DeviceSessionManager struct {
	mu       sync.Mutex
	sessions map[string]*DeviceSession // keyed by token digest
	bootID   string
	lifetime time.Duration
}

// NewDeviceSessionManager creates an empty session store. bootID ensures
// a restart invalidates all prior sessions. lifetime is the token TTL.
func NewDeviceSessionManager(bootID string, lifetime time.Duration) *DeviceSessionManager {
	if lifetime <= 0 {
		lifetime = 20 * time.Minute
	}
	return &DeviceSessionManager{
		sessions: make(map[string]*DeviceSession),
		bootID:   bootID,
		lifetime: lifetime,
	}
}

// BootID returns the daemon boot ID (sessions are bound to it).
func (m *DeviceSessionManager) BootID() string { return m.bootID }

// CreateAfterVerifiedChallenge issues a new opaque 32-byte bearer token
// after a challenge has been consumed and the device signature verified.
// The raw token is returned exactly once; only its SHA-256 digest is stored.
// Callers must never log the raw token.
func (m *DeviceSessionManager) CreateAfterVerifiedChallenge(
	deviceID, hostID, bootID string, permissions []string,
) (rawToken string, sessionID string, expiresAt time.Time, err error) {
	if bootID != m.bootID {
		return "", "", time.Time{}, fmt.Errorf("boot ID mismatch")
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", "", time.Time{}, err
	}
	raw := hex.EncodeToString(tokenBytes)
	digestBytes := sha256.Sum256(tokenBytes)
	digest := hex.EncodeToString(digestBytes[:])

	now := time.Now().UTC()
	sessID := hex.EncodeToString(tokenBytes[:12])
	exp := now.Add(m.lifetime)
	sess := &DeviceSession{
		TokenDigest: digest,
		SessionID:   sessID,
		DeviceID:    deviceID,
		HostID:      hostID,
		BootID:      bootID,
		Permissions: append([]string{}, permissions...),
		IssuedAt:    now,
		ExpiresAt:   exp,
	}
	m.mu.Lock()
	m.sessions[digest] = sess
	m.mu.Unlock()
	return raw, sessID, exp, nil
}

// AuthenticateBearer validates a raw bearer token and returns a Principal.
// The raw token is compared by SHA-256 digest — no raw token is stored or
// exposed. Returns nil if the token is invalid, expired or the device was
// revoked.
func (m *DeviceSessionManager) AuthenticateBearer(rawToken string) *Principal {
	if rawToken == "" {
		return nil
	}
	tokenBytes, err := hex.DecodeString(rawToken)
	if err != nil || len(tokenBytes) != 32 {
		return nil
	}
	digestBytes := sha256.Sum256(tokenBytes)
	digest := hex.EncodeToString(digestBytes[:])

	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[digest]
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	if now.After(sess.ExpiresAt) {
		delete(m.sessions, digest)
		return nil
	}
	return &Principal{
		DeviceID:    sess.DeviceID,
		Permissions: append([]string{}, sess.Permissions...),
		SessionID:   sess.SessionID,
		HostID:      sess.HostID,
		AuthTime:    sess.IssuedAt,
	}
}

// RevokeDevice immediately invalidates every session for a device.
func (m *DeviceSessionManager) RevokeDevice(deviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, s := range m.sessions {
		if subtle.ConstantTimeCompare([]byte(s.DeviceID), []byte(deviceID)) == 1 {
			delete(m.sessions, k)
		}
	}
}

// PurgeExpired removes expired sessions.
func (m *DeviceSessionManager) PurgeExpired(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, s := range m.sessions {
		if now.After(s.ExpiresAt) {
			delete(m.sessions, k)
		}
	}
}

// Count returns the number of active sessions (for diagnostics).
func (m *DeviceSessionManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}
