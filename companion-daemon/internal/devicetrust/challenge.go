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

// PendingChallenge is the daemon-side metadata for a single-use challenge.
// All fields are json:"-" so serialisation never leaks internal state.
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

// challengeDigest returns the SHA-256 hex digest of a challenge ID. This is
// the internal map key — the raw ID is never persisted.
func challengeDigest(id []byte) string {
	s := sha256.Sum256(id)
	return hex.EncodeToString(s[:])
}

// ChallengeStore is an in-memory, concurrency-safe store of single-use
// authentication challenges.
//
// State machine (per challenge, serialized by the store mutex):
//
//	inserted ──Find──▶ find returns a copy ──Verify (caller does crypto)──▶
//	  │                    │                              │
//	  │                    │ signature ok                 │ signature fail
//	  │                    ▼                              ▼
//	  │               Consume()                      RecordFailure()
//	  │               atomic: marks consumed         increments attempts;
//	  │               + deletes, returns challenge    deletes if ≥ maxAttempts
//	  │
//	  └── expired / capped / duplicate → insert rejected
//
// Concurrent verifiers: Find returns the same challenge to multiple callers.
// Only the FIRST successful Consume wins; others see "already consumed".
// RecordFailure is safe to call concurrently with Find/Consume.
//
// Per-device and total caps bound memory; Insert auto-purges expired
// challenges.
type ChallengeStore struct {
	mu           sync.Mutex
	challenges   map[string]*PendingChallenge // keyed by SHA-256 hex digest
	maxPerDevice int
	maxTotal     int
	maxAttempts  int
	randReader   io.Reader // injectable for tests
}

// ChallengeStoreConfig allows injectable parameters for tests.
type ChallengeStoreConfig struct {
	MaxPerDevice, MaxTotal, MaxAttempts int
	RandReader                          io.Reader
}

func NewChallengeStore() *ChallengeStore {
	return NewChallengeStoreWithConfig(ChallengeStoreConfig{})
}

func NewChallengeStoreWithConfig(cfg ChallengeStoreConfig) *ChallengeStore {
	if cfg.MaxPerDevice <= 0 {
		cfg.MaxPerDevice = 3
	}
	if cfg.MaxTotal <= 0 {
		cfg.MaxTotal = 30
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	r := cfg.RandReader
	if r == nil {
		r = rand.Reader
	}
	return &ChallengeStore{
		challenges:   make(map[string]*PendingChallenge),
		maxPerDevice: cfg.MaxPerDevice,
		maxTotal:     cfg.MaxTotal,
		maxAttempts:  cfg.MaxAttempts,
		randReader:   r,
	}
}

// Insert stores a new challenge. Returns the 32-byte challenge ID (sent to
// the client). Auto-purges expired challenges. Rejects if the device has
// too many pending challenges or the total limit is hit.
func (cs *ChallengeStore) Insert(ch *PendingChallenge) ([]byte, error) {
	id := make([]byte, 32)
	if _, err := io.ReadFull(cs.randReader, id); err != nil {
		return nil, fmt.Errorf("challenge insert: %w", err)
	}
	ch.ChallengeID = id
	key := challengeDigest(id)

	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.purgeExpiredLocked(time.Now().UTC())

	devCount := 0
	for _, e := range cs.challenges {
		if e.DeviceID == ch.DeviceID {
			devCount++
		}
	}
	if devCount >= cs.maxPerDevice {
		return nil, fmt.Errorf("too many pending challenges for this device")
	}
	if len(cs.challenges) >= cs.maxTotal {
		return nil, fmt.Errorf("too many pending challenges")
	}
	cs.challenges[key] = ch
	return id, nil
}

// Find returns a copy of a challenge without consuming it. Used by the
// verify handler so signature verification can happen before the atomic
// consume step.
func (cs *ChallengeStore) Find(challengeID []byte) (*PendingChallenge, error) {
	if len(challengeID) != 32 {
		return nil, fmt.Errorf("invalid challenge id")
	}
	key := challengeDigest(challengeID)
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
	copy := *ch
	return &copy, nil
}

// Consume atomically verifies and consumes a challenge. Only the first
// caller succeeds; subsequent callers see "already consumed" or "not found".
func (cs *ChallengeStore) Consume(challengeID, deviceID []byte) (*PendingChallenge, error) {
	if len(challengeID) != 32 || len(deviceID) == 0 {
		return nil, fmt.Errorf("invalid challenge or device id")
	}
	key := challengeDigest(challengeID)
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
	if subtle.ConstantTimeCompare([]byte(ch.DeviceID), deviceID) != 1 {
		return nil, fmt.Errorf("device id mismatch")
	}
	ch.Consumed = true
	delete(cs.challenges, key)
	return ch, nil
}

// RecordFailure increments the attempt counter. If the threshold is reached
// the challenge is deleted (terminal). Safe to call concurrently with Find.
func (cs *ChallengeStore) RecordFailure(challengeID []byte) {
	if len(challengeID) != 32 {
		return
	}
	key := challengeDigest(challengeID)
	cs.mu.Lock()
	defer cs.mu.Unlock()
	ch, ok := cs.challenges[key]
	if !ok {
		return
	}
	ch.FailedAttempts++
	if ch.FailedAttempts >= cs.maxAttempts {
		delete(cs.challenges, key)
	}
}

// PurgeExpired removes all expired challenges. Called by the periodic
// cleanup goroutine owned by the app lifecycle.
func (cs *ChallengeStore) PurgeExpired(now time.Time) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.purgeExpiredLocked(now)
}

func (cs *ChallengeStore) purgeExpiredLocked(now time.Time) {
	for k, ch := range cs.challenges {
		if now.After(ch.ExpiresAt) {
			delete(cs.challenges, k)
		}
	}
}

// Count returns the number of pending challenges (package-private, for tests).
func (cs *ChallengeStore) Count() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return len(cs.challenges)
}

// NewChallengeID generates a fresh 32-byte challenge ID.
func NewChallengeID() ([]byte, error) {
	id := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, id); err != nil {
		return nil, err
	}
	return id, nil
}

// NewBootID generates a random daemon boot identifier.
func NewBootID() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
