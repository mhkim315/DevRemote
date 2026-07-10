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

// PendingChallenge is the daemon-side metadata for a single-use challenge.
type PendingChallenge struct {
	ChallengeID    []byte    `json:"-"` // 32 bytes
	DeviceID       string    `json:"-"`
	ClientNonce    []byte    `json:"-"` // 32 bytes
	ServerNonce    []byte    `json:"-"` // 32 bytes
	HostID         string    `json:"-"`
	DaemonBootID   string    `json:"-"`
	IssuedAt       time.Time `json:"-"`
	ExpiresAt      time.Time `json:"-"`
	FailedAttempts int       `json:"-"`
	Consumed       bool      `json:"-"`
}

// ChallengeStore is an in-memory, concurrency-safe store of single-use
// authentication challenges. Challenges are consumed atomically; a consumed
// or expired challenge can never be used to mint a session.
type ChallengeStore struct {
	mu         sync.Mutex
	challenges map[string]*PendingChallenge // keyed by hex(challengeId digest)
}

func NewChallengeStore() *ChallengeStore {
	return &ChallengeStore{challenges: make(map[string]*PendingChallenge)}
}

// Insert stores a new challenge. Returns the 32-byte challenge ID.
func (cs *ChallengeStore) Insert(ch *PendingChallenge) ([]byte, error) {
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	ch.ChallengeID = id
	key := hex.EncodeToString(id)
	cs.mu.Lock()
	cs.challenges[key] = ch
	cs.mu.Unlock()
	return id, nil
}

// Consume atomically verifies, marks consumed, and returns a challenge. On
// success it returns the challenge and the clientNonce (for signature
// verification).  Only the first successful caller gets the challenge;
// subsequent callers see it as consumed/expired/invalid.
func (cs *ChallengeStore) Consume(challengeID, deviceID []byte) (*PendingChallenge, error) {
	if len(challengeID) != 32 || len(deviceID) == 0 {
		return nil, fmt.Errorf("invalid challenge or device id")
	}
	key := hex.EncodeToString(challengeID)
	cs.mu.Lock()
	defer cs.mu.Unlock()

	ch, ok := cs.challenges[key]
	if !ok {
		return nil, fmt.Errorf("challenge not found")
	}
	if ch.Consumed {
		return nil, fmt.Errorf("challenge already consumed")
	}
	if time.Now().UTC().After(ch.ExpiresAt) {
		delete(cs.challenges, key)
		return nil, fmt.Errorf("challenge expired")
	}
	// Constant-time deviceId comparison to avoid enumeration.
	if subtle.ConstantTimeCompare([]byte(ch.DeviceID), deviceID) != 1 {
		ch.FailedAttempts++
		if ch.FailedAttempts >= 5 {
			delete(cs.challenges, key)
		}
		return nil, fmt.Errorf("device id mismatch")
	}
	ch.Consumed = true
	delete(cs.challenges, key)
	return ch, nil
}

// RecordFailure increments the attempt counter for a challenge and deletes
// it if the threshold is exceeded.
func (cs *ChallengeStore) RecordFailure(challengeID []byte) {
	if len(challengeID) != 32 {
		return
	}
	key := hex.EncodeToString(challengeID)
	cs.mu.Lock()
	defer cs.mu.Unlock()
	ch, ok := cs.challenges[key]
	if !ok {
		return
	}
	ch.FailedAttempts++
	if ch.FailedAttempts >= 5 {
		delete(cs.challenges, key)
	}
}

// PurgeExpired removes expired challenges.
func (cs *ChallengeStore) PurgeExpired(now time.Time) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for k, ch := range cs.challenges {
		if now.After(ch.ExpiresAt) {
			delete(cs.challenges, k)
		}
	}
}

// NewChallengeID generates a fresh 32-byte random challenge ID.
func NewChallengeID() ([]byte, error) {
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	return id, nil
}

// ChallengeDigest returns the SHA-256 hex digest of a challenge ID, used
// as the in-memory map key.
func ChallengeDigest(id []byte) string {
	s := sha256.Sum256(id)
	return hex.EncodeToString(s[:])
}

// NewBootID generates a random daemon boot identifier (survives only in
// memory; a restart creates a new one, invalidating all prior challenges
// and sessions).
func NewBootID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
