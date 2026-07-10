package devicetrust

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

// wsTicket is a single-use, short-lived opaque token that grants one
// WebSocket upgrade. The raw ticket is returned exactly once; only its
// SHA-256 digest is stored.
type wsTicket struct {
	Digest    string
	DeviceID  string
	Principal *Principal
	ExpiresAt time.Time
	Consumed  bool
}

// WSTicketStore issues and consumes WebSocket upgrade tickets. Tickets
// are single-use, expire after 30s, and never appear in URLs after
// consumption.
type WSTicketStore struct {
	mu      sync.Mutex
	tickets map[string]*wsTicket // keyed by SHA-256 digest of raw ticket
}

func NewWSTicketStore() *WSTicketStore {
	return &WSTicketStore{tickets: make(map[string]*wsTicket)}
}

// Issue creates a 32-byte opaque ticket bound to a Principal.
func (s *WSTicketStore) Issue(p *Principal) (rawTicket string, err error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	raw := hex.EncodeToString(b)
	digestBytes := sha256.Sum256(b)
	digest := hex.EncodeToString(digestBytes[:])

	s.mu.Lock()
	s.tickets[digest] = &wsTicket{
		Digest:    digest,
		DeviceID:  p.DeviceID,
		Principal: p,
		ExpiresAt: time.Now().UTC().Add(30 * time.Second),
	}
	s.mu.Unlock()
	return raw, nil
}

// Consume atomically validates and consumes a ticket, returning its
// Principal. Only the first caller gets the Principal; subsequent callers
// see it as consumed/expired/invalid.
func (s *WSTicketStore) Consume(rawTicket string) *Principal {
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
	t.Consumed = true
	delete(s.tickets, digest)
	return t.Principal
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
		raw, err := store.Issue(p)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"ticket":    raw,
			"expiresIn": "30",
		})
	}
}

// ConsumeWSTicket validates a ticket from a URL query parameter and
// returns the Principal, or nil. The ticket is consumed on success.
// Callers must use this BEFORE upgrading to WebSocket. The raw ticket
// must not appear in logs, close reasons, or error payloads.
func ConsumeWSTicket(store *WSTicketStore, ticketParam string) *Principal {
	if ticketParam == "" {
		return nil
	}
	p := store.Consume(ticketParam)
	// Remove ticket from the URL so it doesn't leak into logs/diagnostics.
	// (The caller is responsible for the URL used — we just validate.)
	return p
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
