package devicetrust

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

var (
	ErrWSTicketCapacity             = errors.New("websocket ticket capacity reached")
	ErrWSTicketAuthorizationExpired = errors.New("websocket ticket authorization expired")
	ErrWSTicketInvalidGrant         = errors.New("invalid websocket ticket grant")
)

type WSTicketStoreConfig struct {
	TTL          time.Duration
	MaxPerDevice int
	MaxTotal     int
}

// wsTicket is a single-use, short-lived opaque token that grants one
// WebSocket upgrade. The raw ticket is returned exactly once; only its
// SHA-256 digest is stored. Bound to a specific session, device, and host.
type wsTicket struct {
	Digest    string
	DeviceID  string
	HostID    string
	SessionID string
	Principal *Principal
	ExpiresAt time.Time
	Consumed  bool
}

// WSTicketStore issues and consumes WebSocket upgrade tickets. Tickets
// are single-use, expire after 30s, and never appear in URLs after
// consumption.
type WSTicketStore struct {
	mu           sync.Mutex
	tickets      map[string]*wsTicket // keyed by SHA-256 digest of raw ticket
	ttl          time.Duration
	maxPerDevice int
	maxTotal     int
}

func NewWSTicketStore() *WSTicketStore {
	return NewWSTicketStoreWithConfig(WSTicketStoreConfig{})

}

func NewWSTicketStoreWithConfig(cfg WSTicketStoreConfig) *WSTicketStore {
	if cfg.TTL <= 0 {
		cfg.TTL = 30 * time.Second
	}
	if cfg.MaxPerDevice <= 0 {
		cfg.MaxPerDevice = 3
	}
	if cfg.MaxTotal <= 0 {
		cfg.MaxTotal = 64
	}
	return &WSTicketStore{
		tickets:      make(map[string]*wsTicket),
		ttl:          cfg.TTL,
		maxPerDevice: cfg.MaxPerDevice,
		maxTotal:     cfg.MaxTotal,
	}
}

// Issue creates a 32-byte opaque ticket bound to a Principal, host, and
// target session. The phone must use this ticket ONLY for that session.
func (s *WSTicketStore) Issue(p *Principal, hostID, sessionID string) (rawTicket string, expiresAt time.Time, err error) {
	if p == nil || p.DeviceID == "" || p.BearerSessionID == "" || p.BearerExpires.IsZero() || hostID == "" || p.HostID != hostID || sessionID == "" {
		return "", time.Time{}, ErrWSTicketInvalidGrant
	}
	now := time.Now().UTC()
	expiresAt = effectiveTicketExpiry(now, s.ttl, p.BearerExpires)
	if !expiresAt.After(now) {
		return "", time.Time{}, ErrWSTicketAuthorizationExpired
	}
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", time.Time{}, err
	}
	raw := hex.EncodeToString(b)
	digestBytes := sha256.Sum256(b)
	digest := hex.EncodeToString(digestBytes[:])

	s.mu.Lock()
	defer s.mu.Unlock()
	// Capacity check and insertion are one atomic operation. Expired entries
	// release both global and per-device capacity before the check.
	s.purgeExpiredLocked(now)
	if len(s.tickets) >= s.maxTotal {
		return "", time.Time{}, ErrWSTicketCapacity
	}
	devCount := 0
	for _, t := range s.tickets {
		if t.DeviceID == p.DeviceID {
			devCount++
		}
	}
	if devCount >= s.maxPerDevice {
		return "", time.Time{}, ErrWSTicketCapacity
	}
	// Defensive copy: the ticket stores an immutable permission snapshot.
	pp := *p
	pp.Permissions = clonePerms(p.Permissions)
	s.tickets[digest] = &wsTicket{
		Digest:    digest,
		DeviceID:  p.DeviceID,
		HostID:    hostID,
		SessionID: sessionID,
		Principal: &pp,
		ExpiresAt: expiresAt,
	}
	return raw, expiresAt, nil
}
func (s *WSTicketStore) purgeExpiredLocked(now time.Time) {
	for k, t := range s.tickets {
		if now.After(t.ExpiresAt) {
			delete(s.tickets, k)
		}
	}
}

// ConsumeBound validates the ticket AND its session/host bindings AND that
// the authorizing bearer session is still active (not replaced/revoked/expired).
// A mismatch consumes the ticket (one-shot) but returns nil — the caller
// cannot retry with a different session or after the bearer has changed.
func (s *WSTicketStore) ConsumeBound(rawTicket, expectedHostID, expectedSessionID string, mgr *DeviceSessionManager) *Principal {
	if expectedHostID == "" || expectedSessionID == "" || mgr == nil {
		return nil
	}
	b, err := hex.DecodeString(rawTicket)
	if err != nil || len(b) != 32 {
		return nil
	}
	digestBytes := sha256.Sum256(b)
	digest := hex.EncodeToString(digestBytes[:])

	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tickets[digest]
	if !ok || t.Consumed {
		return nil
	}
	if time.Now().UTC().After(t.ExpiresAt) {
		delete(s.tickets, digest)
		return nil
	}
	// Validate bindings. A mismatch consumes the ticket (one-shot) but
	// returns nil — the caller cannot retry with a different session.
	if t.HostID != expectedHostID {
		t.Consumed = true
		delete(s.tickets, digest)
		return nil
	}
	if t.SessionID != expectedSessionID {
		t.Consumed = true
		delete(s.tickets, digest)
		return nil
	}
	// Bearer-session revalidation: the authorizing bearer must still be
	// the active session for this device. If the bearer was replaced or
	// revoked after ticket issuance, the ticket is invalid.
	if !mgr.isActiveBearer(t.DeviceID, t.Principal.BearerSessionID) {
		t.Consumed = true
		delete(s.tickets, digest)
		return nil
	}

	t.Consumed = true
	delete(s.tickets, digest)
	// Return a fresh copy — no shared pointers.
	pp := *t.Principal
	pp.Permissions = clonePerms(t.Principal.Permissions)
	return &pp
}

// PurgeExpired removes expired tickets. Safe to call periodically.
func (s *WSTicketStore) PurgeExpired(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, t := range s.tickets {
		if now.After(t.ExpiresAt) {
			delete(s.tickets, k)
		}
	}
}

func (s *WSTicketStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tickets)
}

// ── POST /api/device-auth/ws-ticket handler ──

// HandleWSTicket issues a WebSocket upgrade ticket to an already-
// authenticated Principal. The caller must have PermSessionsRead.
func HandleWSTicket(store *WSTicketStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := PrincipalFromContext(r.Context())
		if p == nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		sessionID := r.URL.Query().Get("session")
		if sessionID == "" {
			sessionID = "devremote"
		}
		raw, expiresAt, err := store.Issue(p, p.HostID, sessionID)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, ErrWSTicketCapacity) {
				status = http.StatusTooManyRequests
			} else if errors.Is(err, ErrWSTicketAuthorizationExpired) {
				status = http.StatusUnauthorized
			} else if errors.Is(err, ErrWSTicketInvalidGrant) {
				status = http.StatusBadRequest
			}
			http.Error(w, "unable to issue websocket ticket", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"ticket":    raw,
			"expiresAt": expiresAt.Format(time.RFC3339Nano),
		})
	}
}

// RevokeForDevice removes all pending tickets for a device.
func (s *WSTicketStore) RevokeForDevice(deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, t := range s.tickets {
		if subtle.ConstantTimeCompare([]byte(t.DeviceID), []byte(deviceID)) == 1 {
			delete(s.tickets, k)
		}
	}
}
func effectiveTicketExpiry(now time.Time, ttl time.Duration, bearerExpires time.Time) time.Time {
	candidate := now.Add(ttl)
	if bearerExpires.IsZero() {
		return candidate
	}
	if bearerExpires.Before(candidate) {
		return bearerExpires
	}
	return candidate
}
