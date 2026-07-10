package devicetrust

import (
	"sync"
	"time"
)

// challengeRateLimiter is a per-device token-bucket limiter for challenge
// issuance. It bounds memory by periodically purging expired entries.
type challengeRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	maxAge  time.Duration // entries idle longer than this are deleted
}

type tokenBucket struct {
	tokens   float64
	lastFill time.Time
}

// rateLimiterConfig holds injectable parameters.
type RateLimiterConfig struct {
	Burst      int           // max tokens (max consecutive challenges)
	RatePerMin float64       // sustained tokens per minute
	MaxAge     time.Duration // purge stale entries after this duration
}

func NewChallengeRateLimiter(cfg RateLimiterConfig) *challengeRateLimiter {
	if cfg.Burst <= 0 {
		cfg.Burst = 3
	}
	if cfg.RatePerMin <= 0 {
		cfg.RatePerMin = 10
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = 5 * time.Minute
	}
	return &challengeRateLimiter{
		buckets: make(map[string]*tokenBucket),
		maxAge:  cfg.MaxAge,
	}
}

// Allow reports whether a challenge may be issued for deviceID. The
// internal state is updated on success. The limiter map is periodically
// cleaned so unknown device IDs cannot create an unbounded number of
// entries.
func (l *challengeRateLimiter) Allow(now time.Time, deviceID string, burst int, ratePerMin float64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[deviceID]
	if !ok {
		b = &tokenBucket{tokens: float64(burst), lastFill: now}
		l.buckets[deviceID] = b
	}
	// Refill tokens.
	elapsed := now.Sub(b.lastFill).Minutes()
	b.tokens += elapsed * ratePerMin
	if b.tokens > float64(burst) {
		b.tokens = float64(burst)
	}
	b.lastFill = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Purge removes entries that have been idle longer than maxAge.
func (l *challengeRateLimiter) Purge(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.buckets {
		if now.Sub(b.lastFill) > l.maxAge {
			delete(l.buckets, k)
		}
	}
}

// Count returns the number of tracked buckets (for test diagnostics).
func (l *challengeRateLimiter) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
