package devicetrust

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"time"
)

// ── Canonical permission vocabulary (M2.5-4 enforcement boundary) ──

const (
	PermSessionsRead   = "sessions:read"
	PermSessionsCreate = "sessions:create"
	PermSessionsStop   = "sessions:stop"
	PermSessionsKill   = "sessions:kill"
	PermHistoryDelete  = "history:delete"
	PermTerminalInput  = "terminal:input"
)

// PermOwner is the full set granted to the first paired (owner) device.
// permission templates are private so no caller can mutate them.
var permOwnerTemplate = []string{
	PermSessionsRead,
	PermSessionsCreate,
	PermSessionsStop,
	PermSessionsKill,
	PermHistoryDelete,
	PermTerminalInput,
}
var permMemberTemplate = []string{
	PermSessionsRead,
}

// PermissionsForRole returns a FRESH COPY of the permission set for a device
// role. Unknown or empty roles return nil (fail closed — no token issued).
func PermissionsForRole(role string) []string {
	var src []string
	switch role {
	case RoleOwner:
		src = permOwnerTemplate
	case RoleMember:
		src = permMemberTemplate
	default:
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

// clonePerms returns a defensive copy so no caller can mutate the templates.
func clonePerms(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// ── Principal (handler-independent) ──

type Principal struct {
	DeviceID    string
	Permissions []string // defensive copy, never shared
	SessionID   string
	HostID      string
	AuthTime    time.Time
}

// ── DeviceSession ──

type DeviceSession struct {
	TokenDigest string    `json:"-"`
	SessionID   string    `json:"-"`
	DeviceID    string    `json:"-"`
	HostID      string    `json:"-"`
	BootID      string    `json:"-"`
	Permissions []string  `json:"-"`
	IssuedAt    time.Time `json:"-"`
	ExpiresAt   time.Time `json:"-"`
}

// ── DeviceSessionManager ──

type DeviceSessionManager struct {
	mu            sync.Mutex
	sessions      map[string]*DeviceSession // token digest → session
	byDevice      map[string]string         // deviceId → token digest (one active per device)
	bootID        string
	lifetime      time.Duration
	onReplace     OnReplaceFunc // called when a device session is replaced
	maxSessions   int
	purgeStop     chan struct{}
	purgeDone     chan struct{}
	purgeInterval time.Duration
}

// DeviceSessionManagerConfig holds injectable parameters.
type DeviceSessionManagerConfig struct {
	BootID        string
	Lifetime      time.Duration
	MaxSessions   int
	PurgeInterval time.Duration
}

func NewDeviceSessionManager(bootID string, lifetime time.Duration) *DeviceSessionManager {
	return NewDeviceSessionManagerWithConfig(DeviceSessionManagerConfig{
		BootID: bootID, Lifetime: lifetime,
	})
}

func NewDeviceSessionManagerWithConfig(cfg DeviceSessionManagerConfig) *DeviceSessionManager {
	if cfg.Lifetime <= 0 {
		cfg.Lifetime = 20 * time.Minute
	}
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 64
	}
	if cfg.PurgeInterval <= 0 {
		cfg.PurgeInterval = 5 * time.Minute
	}
	return &DeviceSessionManager{
		sessions:      make(map[string]*DeviceSession),
		byDevice:      make(map[string]string),
		bootID:        cfg.BootID,
		lifetime:      cfg.Lifetime,
		maxSessions:   cfg.MaxSessions,
		purgeInterval: cfg.PurgeInterval,
	}
}

// ── Lifecycle ──

func (m *DeviceSessionManager) StartPurgeLoop() {
	m.mu.Lock()
	if m.purgeStop != nil {
		m.mu.Unlock()
		return
	}
	m.purgeStop = make(chan struct{})
	m.purgeDone = make(chan struct{})
	interval := m.purgeInterval
	m.mu.Unlock()
	go func() {
		defer close(m.purgeDone)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.PurgeExpired(time.Now().UTC())
			case <-m.purgeStop:
				return
			}
		}
	}()
}

func (m *DeviceSessionManager) StopPurgeLoop() {
	m.mu.Lock()
	if m.purgeStop == nil {
		m.mu.Unlock()
		return
	}
	close(m.purgeStop)
	done := m.purgeDone
	m.mu.Unlock()
	<-done
}

func (m *DeviceSessionManager) SetPurgeInterval(d time.Duration) {
	m.mu.Lock()
	m.purgeInterval = d
	m.mu.Unlock()
}

func (m *DeviceSessionManager) BootID() string { return m.bootID }

// ── Token issuance (atomic: device-index update + cap check + insertion) ──

// CreateAfterVerifiedChallenge issues a new 32-byte bearer token. The
// per-device index is updated atomically so the previous session for the
// same device is immediately invalidated and the new token is the only
// valid one. The raw token is returned exactly once and never stored.
//
// Global cap: replacement within the same device is allowed even at the
// cap; a NEW device is rejected if the cap is full. Expired sessions are
// purged inline so capacity is immediately reusable.
func (m *DeviceSessionManager) CreateAfterVerifiedChallenge(
	deviceID, hostID, bootID string, permissions []string,
) (rawToken string, sessionID string, expiresAt time.Time, err error) {
	if bootID != m.bootID {
		return "", "", time.Time{}, fmt.Errorf("boot ID mismatch")
	}
	tokenBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, tokenBytes); err != nil {
		return "", "", time.Time{}, fmt.Errorf("session token: %w", err)
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
		Permissions: clonePerms(permissions),
		IssuedAt:    now,
		ExpiresAt:   exp,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Purge expired inline so capacity reflects live sessions.
	m.purgeExpiredLocked(now)

	// Determine replacement state BEFORE mutation.
	_, isReplacement := m.byDevice[deviceID]

	// Global cap: a NEW device must leave room; replacement is always allowed.
	if !isReplacement && len(m.sessions) >= m.maxSessions {
		return "", "", time.Time{}, fmt.Errorf("too many active sessions")
	}

	// Atomic install: remove the previous session (if any) only as part of
	// the successful commit.
	if prevDigest, exists := m.byDevice[deviceID]; exists {
		delete(m.sessions, prevDigest)
	}
	// M2.5-4: notify conn registry that this device's old session is replaced.
	if isReplacement && m.onReplace != nil {
		m.onReplace(deviceID)
	}
	m.sessions[digest] = sess
	m.byDevice[deviceID] = digest
	return raw, sessID, exp, nil
}

// ── Authentication ──

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
		delete(m.byDevice, sess.DeviceID)
		return nil
	}
	return &Principal{
		DeviceID:    sess.DeviceID,
		Permissions: clonePerms(sess.Permissions),
		SessionID:   sess.SessionID,
		HostID:      sess.HostID,
		AuthTime:    sess.IssuedAt,
	}
}

// ── Revoke / purge ──

func (m *DeviceSessionManager) RevokeDevice(deviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revokeDeviceLocked(deviceID)
}

func (m *DeviceSessionManager) revokeDeviceLocked(deviceID string) {
	if digest, ok := m.byDevice[deviceID]; ok {
		delete(m.sessions, digest)
		delete(m.byDevice, deviceID)
	}
	// Also walk all sessions for this device (belt-and-suspenders).
	for k, s := range m.sessions {
		if subtle.ConstantTimeCompare([]byte(s.DeviceID), []byte(deviceID)) == 1 {
			delete(m.sessions, k)
		}
	}
}

func (m *DeviceSessionManager) PurgeExpired(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeExpiredLocked(now)
}

func (m *DeviceSessionManager) purgeExpiredLocked(now time.Time) {
	for k, s := range m.sessions {
		if now.After(s.ExpiresAt) {
			delete(m.sessions, k)
			delete(m.byDevice, s.DeviceID)
		}
	}
}

// Count returns active session count (package-private, tests).
func (m *DeviceSessionManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// OnReplaceFunc is called when a device session is replaced. The conn registry
// uses this to close active WebSocket connections for the old session.
type OnReplaceFunc func(deviceID string)

// SetOnReplace registers a callback invoked after a session is replaced.
func (m *DeviceSessionManager) SetOnReplace(fn OnReplaceFunc) { m.onReplace = fn }

func (m *DeviceSessionManager) checkInvariant() {
	for devID, digest := range m.byDevice {
		s, ok := m.sessions[digest]
		if !ok {
			panic("byDevice " + devID + " → missing session " + digest)
		}
		if s.DeviceID != devID {
			panic("byDevice " + devID + " → session for " + s.DeviceID)
		}
	}
	if len(m.byDevice) > len(m.sessions) {
		panic("orphan byDevice entries")
	}
	// Every session must have a byDevice entry pointing to it.
	for digest, s := range m.sessions {
		d2, ok := m.byDevice[s.DeviceID]
		if !ok || d2 != digest {
			panic("session " + digest + " missing byDevice entry")
		}
	}
}
