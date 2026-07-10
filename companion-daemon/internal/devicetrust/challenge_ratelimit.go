package devicetrust

import (
	"sync"
	"time"
)

type challengeRateLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*tokenBucket
	burst      int
	ratePerMin float64
	maxAge     time.Duration
}
type tokenBucket struct {
	tokens   float64
	lastFill time.Time
}
type RateLimiterConfig struct {
	Burst      int
	RatePerMin float64
	MaxAge     time.Duration
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
		buckets:    make(map[string]*tokenBucket),
		burst:      cfg.Burst,
		ratePerMin: cfg.RatePerMin,
		maxAge:     cfg.MaxAge,
	}
}

// Allow reports whether a challenge may be issued for deviceID. At most ONE
// stale entry is pruned per call so the map scan is bounded. Full purge is
// available via Purge().
func (l *challengeRateLimiter) Allow(now time.Time, deviceID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Bounded opportunistic purge: remove at most one stale entry.
	for k, b := range l.buckets {
		if now.Sub(b.lastFill) > l.maxAge {
			delete(l.buckets, k)
			break
		}
	}

	b, ok := l.buckets[deviceID]
	if !ok {
		b = &tokenBucket{tokens: float64(l.burst) - 1, lastFill: now}
		l.buckets[deviceID] = b
		return true
	}
	elapsed := now.Sub(b.lastFill).Minutes()
	b.tokens += elapsed * l.ratePerMin
	if b.tokens > float64(l.burst) {
		b.tokens = float64(l.burst)
	}
	b.lastFill = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *challengeRateLimiter) Purge(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.buckets {
		if now.Sub(b.lastFill) > l.maxAge {
			delete(l.buckets, k)
		}
	}
}
func (l *challengeRateLimiter) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
func (l *challengeRateLimiter) Burst() int { return l.burst }
