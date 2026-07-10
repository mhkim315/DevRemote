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
	DeviceID        string
	Permissions     []string // defensive copy, never shared
	SessionID       string   // internal correlation id
	BearerSessionID string   // stable id of the authorizing bearer session
	BearerExpires   time.Time
	HostID          string
	DeviceBootID    string // daemon boot ID at auth time
	AuthTime        time.Time
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
	onRevoke      OnReplaceFunc // called when a device is revoked
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

// isActiveBearer reports whether a bearer session ID is still the current
// active session for the device (not replaced/revoked/expired).
func (m *DeviceSessionManager) isActiveBearer(deviceID, bearerSessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	digest, ok := m.byDevice[deviceID]
	if !ok {
		return false
	}
	sess, ok := m.sessions[digest]
	if !ok {
		return false
	}
	if time.Now().UTC().After(sess.ExpiresAt) {
		return false
	}
	return sess.SessionID == bearerSessionID
}

func notifyInvalidated(cb OnReplaceFunc, deviceIDs []string) {
	if cb == nil {
		return
	}
	seen := make(map[string]struct{}, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		if deviceID == "" {
			continue
		}
		if _, ok := seen[deviceID]; ok {
			continue
		}
		seen[deviceID] = struct{}{}
		cb(deviceID)
	}
}

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

	// Purge expired inline so capacity reflects live sessions. The invalidation
	// callback must still run: once an entry is removed here, the periodic purge
	// can no longer discover it.
	expiredDeviceIDs := m.purgeExpiredLocked(now)

	// Determine replacement state BEFORE mutation.
	_, isReplacement := m.byDevice[deviceID]

	// Global cap: a NEW device must leave room; replacement is always allowed.
	if !isReplacement && len(m.sessions) >= m.maxSessions {
		invalidateCB := m.onRevoke
		m.mu.Unlock()
		notifyInvalidated(invalidateCB, expiredDeviceIDs)
		return "", "", time.Time{}, fmt.Errorf("too many active sessions")
	}

	// Atomic install: remove the previous session (if any) only as part of
	// the successful commit.
	if prevDigest, exists := m.byDevice[deviceID]; exists {
		delete(m.sessions, prevDigest)
	}
	m.sessions[digest] = sess
	m.byDevice[deviceID] = digest
	replaceCB := m.onReplace
	invalidateCB := m.onRevoke
	m.mu.Unlock()
	notifyInvalidated(invalidateCB, expiredDeviceIDs)
	// M2.5-4: callback outside the lock — network I/O must not block the store.
	if isReplacement && replaceCB != nil {
		replaceCB(deviceID)
	}
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
	sess, ok := m.sessions[digest]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	now := time.Now().UTC()
	if now.After(sess.ExpiresAt) {
		delete(m.sessions, digest)
		delete(m.byDevice, sess.DeviceID)
		deviceID := sess.DeviceID
		cb := m.onRevoke
		m.mu.Unlock()
		notifyInvalidated(cb, []string{deviceID})
		return nil
	}
	p := &Principal{
		DeviceID:        sess.DeviceID,
		Permissions:     clonePerms(sess.Permissions),
		SessionID:       sess.SessionID,
		BearerSessionID: sess.SessionID,
		BearerExpires:   sess.ExpiresAt,
		HostID:          sess.HostID,
		DeviceBootID:    sess.BootID,
		AuthTime:        sess.IssuedAt,
	}
	m.mu.Unlock()
	return p
}

// ── Revoke / purge ──

func (m *DeviceSessionManager) RevokeDevice(deviceID string) {
	m.mu.Lock()
	m.revokeDeviceLocked(deviceID)
	cb := m.onRevoke
	m.mu.Unlock()
	if cb != nil {
		cb(deviceID)
	}
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
	expired := m.purgeExpiredLocked(now)
	cb := m.onRevoke
	m.mu.Unlock()
	notifyInvalidated(cb, expired)
}

func (m *DeviceSessionManager) purgeExpiredLocked(now time.Time) (expiredDeviceIDs []string) {
	for k, s := range m.sessions {
		if now.After(s.ExpiresAt) {
			delete(m.sessions, k)
			delete(m.byDevice, s.DeviceID)
			expiredDeviceIDs = append(expiredDeviceIDs, s.DeviceID)
		}
	}
	return
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
func (m *DeviceSessionManager) SetOnRevoke(fn OnReplaceFunc)  { m.onRevoke = fn }

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
