package ratelimit

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"
)

const (
	OnErrorOpen   = "open"
	OnErrorClosed = "closed"
	OnErrorLocal  = "local"
)

// Spec is the backend-agnostic constructor input for a named limiter.
type Spec struct {
	Name    string
	Rate    float64
	Burst   int
	OnError string // open (default), closed, or local; memory ignores this
}

// Decision is the result of one Allow check.
type Decision struct {
	Allowed       bool
	RetryAfter    time.Duration
	BackendError  bool
	LocalFallback bool // BackendError and the in-process limiter produced this result
	Canceled      bool // request context already done; not a rate-limit hit
}

// Allow is the backend-agnostic admit check used by HTTP rate-limit
// middleware. *Limiter (in-process token bucket) and the Redis sliding-
// window limiter both satisfy it.
type Allow interface {
	Allow(ctx context.Context, key string) Decision
}

// Factory constructs a named Allow for one rate/burst pair. Name
// distinguishes default vs per-route counters when the backend is shared
// (Redis); the memory factory ignores it.
type Factory interface {
	New(spec Spec) Allow
}

type memoryFactory struct{}

// MemoryFactory returns the default in-process token-bucket factory.
func MemoryFactory() Factory { return memoryFactory{} }

func (memoryFactory) New(spec Spec) Allow {
	return NewLimiter(spec.Rate, spec.Burst)
}

// Window is the Redis sliding-window length that matches a token-bucket
// (rate, burst) pair: ceil(burst/rate * 1000) milliseconds. Average
// throughput is burst/window ≈ rate. Unused by the memory driver.
// Callers must reject rate > burst when using the redis driver.
func Window(rate float64, burst int) time.Duration {
	if rate <= 0 || burst <= 0 {
		return time.Second
	}
	ms := int(math.Ceil(float64(burst) / rate * 1000))
	if ms < 1 {
		ms = 1
	}
	return time.Duration(ms) * time.Millisecond
}

// MemoryRetryAfter is how long a memory-driver client should wait: ceil(1/rate) seconds.
func MemoryRetryAfter(rate float64) time.Duration {
	if rate <= 0 {
		return time.Second
	}
	secs := int(math.Ceil(1 / rate))
	if secs < 1 {
		secs = 1
	}
	return time.Duration(secs) * time.Second
}

// RetryAfterSeconds rounds a duration up to a whole number of seconds for
// the HTTP Retry-After header (minimum 1).
func RetryAfterSeconds(d time.Duration) int {
	secs := int(math.Ceil(d.Seconds()))
	if secs < 1 {
		return 1
	}
	return secs
}

// FailClosed reports whether backend errors should deny the request.
func FailClosed(onError string) bool {
	return strings.EqualFold(strings.TrimSpace(onError), OnErrorClosed)
}

// UseLocalFallback reports whether backend errors should consult an
// in-process limiter instead of failing open or closed.
func UseLocalFallback(onError string) bool {
	return strings.EqualFold(strings.TrimSpace(onError), OnErrorLocal)
}

var _ Allow = (*Limiter)(nil)
var _ Factory = memoryFactory{}

// Limiter implements a per-key token-bucket rate limiter with automatic
// cleanup of stale entries. Each key (typically a client IP) gets an
// independent bucket. The number of tracked keys is capped at maxBuckets;
// requests from new keys are rejected when the cap is reached.
type Limiter struct {
	mu         sync.Mutex
	buckets    map[string]*bucket
	rate       float64 // tokens added per second
	burst      int     // max tokens (bucket capacity)
	maxBuckets int
	cleanup    time.Duration
	retryAfter time.Duration
	now        func() time.Time
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// DefaultMaxBuckets is the default maximum number of tracked keys.
const DefaultMaxBuckets = 100_000

// NewLimiter creates a Limiter that allows rate tokens per second with a
// maximum burst size. Stale buckets are evicted periodically.
func NewLimiter(rate float64, burst int) *Limiter {
	return NewLimiterWithMax(rate, burst, DefaultMaxBuckets)
}

// NewLimiterWithMax is like NewLimiter but allows setting a custom bucket cap.
func NewLimiterWithMax(rate float64, burst, maxBuckets int) *Limiter {
	l := &Limiter{
		buckets:    make(map[string]*bucket),
		rate:       rate,
		burst:      burst,
		maxBuckets: maxBuckets,
		cleanup:    5 * time.Minute,
		retryAfter: MemoryRetryAfter(rate),
		now:        time.Now,
		stopCh:     make(chan struct{}),
	}
	l.wg.Add(1)
	go l.evictLoop()
	return l
}

// Allow reports whether a request for key should be permitted.
// The request context is ignored — unlike the Redis limiter, which
// marks already-cancelled contexts as Decision.Canceled without counting them.
func (l *Limiter) Allow(_ context.Context, key string) Decision {
	l.mu.Lock()
	defer l.mu.Unlock()

	deny := Decision{RetryAfter: l.retryAfter}
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		// Reject new keys when the bucket table is at capacity.
		if len(l.buckets) >= l.maxBuckets {
			return deny
		}
		// First request from this key; start with full bucket minus one token.
		l.buckets[key] = &bucket{
			tokens:   float64(l.burst) - 1,
			lastSeen: now,
		}
		return Decision{Allowed: true, RetryAfter: l.retryAfter}
	}

	// Refill tokens based on elapsed time.
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > float64(l.burst) {
		b.tokens = float64(l.burst)
	}
	b.lastSeen = now

	if b.tokens < 1 {
		return deny
	}
	b.tokens--
	return Decision{Allowed: true, RetryAfter: l.retryAfter}
}

// Close stops the background eviction goroutine and waits for it to exit.
func (l *Limiter) Close() {
	close(l.stopCh)
	l.wg.Wait()
}

// evictLoop removes buckets that have not been seen recently.
func (l *Limiter) evictLoop() {
	defer l.wg.Done()
	ticker := time.NewTicker(l.cleanup)
	defer ticker.Stop()
	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
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
}
