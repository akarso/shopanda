package ratelimit

import (
	"sync"
	"time"
)

// Limiter implements a per-key token-bucket rate limiter with automatic
// cleanup of stale entries. Each key (typically a client IP) gets an
// independent bucket.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens added per second
	burst   int     // max tokens (bucket capacity)
	cleanup time.Duration
}

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// NewLimiter creates a Limiter that allows rate tokens per second with a
// maximum burst size. Stale buckets are evicted periodically.
func NewLimiter(rate float64, burst int) *Limiter {
	l := &Limiter{
		buckets: make(map[string]*bucket),
		rate:    rate,
		burst:   burst,
		cleanup: 5 * time.Minute,
	}
	go l.evictLoop()
	return l
}

// Allow reports whether a request for key should be permitted.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		// First request from this key; start with full bucket minus one token.
		l.buckets[key] = &bucket{
			tokens:   float64(l.burst) - 1,
			lastSeen: now,
		}
		return true
	}

	// Refill tokens based on elapsed time.
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > float64(l.burst) {
		b.tokens = float64(l.burst)
	}
	b.lastSeen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// evictLoop removes buckets that have not been seen recently.
func (l *Limiter) evictLoop() {
	ticker := time.NewTicker(l.cleanup)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		threshold := time.Now().Add(-l.cleanup)
		for key, b := range l.buckets {
			if b.lastSeen.Before(threshold) {
				delete(l.buckets, key)
			}
		}
		l.mu.Unlock()
	}
}
