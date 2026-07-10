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
	return &challengeRateLimiter{buckets: make(map[string]*tokenBucket), burst: cfg.Burst, ratePerMin: cfg.RatePerMin, maxAge: cfg.MaxAge}
}
func (l *challengeRateLimiter) Allow(now time.Time, deviceID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Opportunistic bounded purge of stale entries.
	if len(l.buckets) > 0 {
		for k, b := range l.buckets {
			if now.Sub(b.lastFill) > l.maxAge {
				delete(l.buckets, k)
				if len(l.buckets) == 0 {
					break
				}
			}
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
func (l *challengeRateLimiter) Count() int { l.mu.Lock(); defer l.mu.Unlock(); return len(l.buckets) }
func (l *challengeRateLimiter) Burst() int { return l.burst }
