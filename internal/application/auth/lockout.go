package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// AttemptStore tracks failed password-login attempts for lockout.
type AttemptStore interface {
	Failures(ctx context.Context, key string) (int, error)
	// Increment records a failure and refreshes the sliding window TTL for key
	// (each call extends expires-at by a full window from now).
	Increment(ctx context.Context, key string, window time.Duration) (int, error)
	// ResetIf clears expected failures observed before a successful login by
	// subtracting them from the counter. Concurrent increments after the
	// observation are preserved (current - expected), not erased.
	ResetIf(ctx context.Context, key string, expected int) error
}

// LockoutSettings configures failed-login lockout on Service.
type LockoutSettings struct {
	Enabled     bool
	MaxFailures int
	Window      time.Duration
}

// DefaultMaxLockoutEntries caps in-memory lockout keys (single-instance store).
const DefaultMaxLockoutEntries = 100_000

// DefaultMemorySweepInterval amortizes full-map expiry sweeps.
const DefaultMemorySweepInterval = time.Minute

const lockoutCachePrefix = "auth:lockout:"

// LockoutKey builds the counter key from client IP and normalized email.
// Empty IP uses "unknown" so account-only keys are never used alone.
func LockoutKey(clientIP, normalizedEmail string) string {
	ip := strings.TrimSpace(clientIP)
	if ip == "" {
		ip = "unknown"
	}
	email := strings.TrimSpace(normalizedEmail)
	return ip + "|" + email
}

// NewAttemptStore returns a lockout store for the configured mode.
// store=cache requires a non-nil cache.Cache (shared across instances via postgres/redis).
// store=memory is single-instance only.
func NewAttemptStore(store string, c cache.Cache, log logger.Logger) (AttemptStore, error) {
	switch strings.ToLower(strings.TrimSpace(store)) {
	case "", "cache":
		if c == nil {
			return nil, fmt.Errorf("auth lockout: store=cache requires a cache backend")
		}
		return NewCacheAttemptStore(c, log), nil
	case "memory":
		return NewMemoryAttemptStore(DefaultMaxLockoutEntries), nil
	default:
		return nil, fmt.Errorf("auth lockout: unsupported store %q (allowed: cache, memory)", store)
	}
}

// CacheAttemptStore persists counters via cache.Incr with a sliding TTL equal to
// the lockout window (each Increment refreshes the full TTL).
type CacheAttemptStore struct {
	cache cache.Cache
	log   logger.Logger
}

// NewCacheAttemptStore wraps a shared cache for lockout counters.
func NewCacheAttemptStore(c cache.Cache, log logger.Logger) *CacheAttemptStore {
	return &CacheAttemptStore{cache: c, log: log}
}

func (s *CacheAttemptStore) cacheKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return lockoutCachePrefix + hex.EncodeToString(sum[:])
}

func (s *CacheAttemptStore) Failures(_ context.Context, key string) (int, error) {
	var raw json.RawMessage
	hit, err := s.cache.Get(s.cacheKey(key), &raw)
	if err != nil {
		return 0, fmt.Errorf("auth lockout cache get: %w", err)
	}
	if !hit {
		return 0, nil
	}
	n, err := parseLockoutCount(raw)
	if err != nil {
		return 0, fmt.Errorf("auth lockout cache get: %w", err)
	}
	return n, nil
}

// parseLockoutCount accepts the current JSON-number wire format and the legacy
// {"count":N} object used by the initial Get/Set lockout implementation.
func parseLockoutCount(raw json.RawMessage) (int, error) {
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return int(n), nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return 0, fmt.Errorf("unsupported lockout counter shape %s", string(raw))
	}
	countRaw, ok := obj["count"]
	if !ok {
		return 0, fmt.Errorf("unsupported lockout counter shape %s", string(raw))
	}
	if err := json.Unmarshal(countRaw, &n); err != nil {
		return 0, fmt.Errorf("unsupported lockout counter shape %s", string(raw))
	}
	return int(n), nil
}

func (s *CacheAttemptStore) Increment(_ context.Context, key string, window time.Duration) (int, error) {
	// Sliding window: atomic Incr refreshes the full TTL on every failure.
	n, err := s.cache.Incr(s.cacheKey(key), 1, window)
	if err != nil {
		return 0, fmt.Errorf("auth lockout cache incr: %w", err)
	}
	return int(n), nil
}

func (s *CacheAttemptStore) ResetIf(_ context.Context, key string, expected int) error {
	if expected <= 0 {
		return nil
	}
	if _, err := s.cache.CompareAndSubtract(s.cacheKey(key), int64(expected)); err != nil {
		return fmt.Errorf("auth lockout cache compare-and-subtract: %w", err)
	}
	return nil
}

type memoryEntry struct {
	count   int
	expires time.Time
}

// MemoryAttemptStore is a bounded in-process counter store (single-instance only).
// Increment uses a sliding window: each failure sets expires = now + window.
type MemoryAttemptStore struct {
	mu         sync.Mutex
	entries    map[string]memoryEntry
	maxEntries int
	now        func() time.Time
	lastSweep  time.Time
	sweepEvery time.Duration
}

// NewMemoryAttemptStore creates a TTL-bounded memory store with a max entry cap.
func NewMemoryAttemptStore(maxEntries int) *MemoryAttemptStore {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxLockoutEntries
	}
	return &MemoryAttemptStore{
		entries:    make(map[string]memoryEntry),
		maxEntries: maxEntries,
		now:        time.Now,
		sweepEvery: DefaultMemorySweepInterval,
	}
}

// SetNowFunc overrides the clock (tests). Nil restores time.Now.
func (s *MemoryAttemptStore) SetNowFunc(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now == nil {
		s.now = time.Now
		return
	}
	s.now = now
}

func (s *MemoryAttemptStore) Failures(_ context.Context, key string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.maybeSweepLocked(now)
	e, ok := s.entries[key]
	if !ok || now.After(e.expires) {
		if ok {
			delete(s.entries, key)
		}
		return 0, nil
	}
	return e.count, nil
}

func (s *MemoryAttemptStore) Increment(_ context.Context, key string, window time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.maybeSweepLocked(now)

	e, ok := s.entries[key]
	if !ok || now.After(e.expires) {
		if ok {
			delete(s.entries, key)
		}
		if !ok && len(s.entries) >= s.maxEntries {
			// Evict one arbitrary expired-or-oldest entry to stay bounded.
			s.evictOneLocked()
			if len(s.entries) >= s.maxEntries {
				return 0, fmt.Errorf("auth lockout memory store at capacity")
			}
		}
		e = memoryEntry{}
	}
	e.count++
	e.expires = now.Add(window) // sliding window
	s.entries[key] = e
	return e.count, nil
}

func (s *MemoryAttemptStore) ResetIf(_ context.Context, key string, expected int) error {
	if expected <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	e, ok := s.entries[key]
	if !ok || now.After(e.expires) {
		delete(s.entries, key)
		return nil
	}
	if e.count < expected {
		return nil
	}
	e.count -= expected
	if e.count == 0 {
		delete(s.entries, key)
		return nil
	}
	s.entries[key] = e
	return nil
}

func (s *MemoryAttemptStore) maybeSweepLocked(now time.Time) {
	if s.sweepEvery > 0 && !s.lastSweep.IsZero() && now.Sub(s.lastSweep) < s.sweepEvery {
		return
	}
	s.evictExpiredLocked(now)
	s.lastSweep = now
}

func (s *MemoryAttemptStore) evictExpiredLocked(now time.Time) {
	for k, e := range s.entries {
		if now.After(e.expires) {
			delete(s.entries, k)
		}
	}
}

func (s *MemoryAttemptStore) evictOneLocked() {
	var oldestKey string
	var oldestExp time.Time
	first := true
	for k, e := range s.entries {
		if first || e.expires.Before(oldestExp) {
			oldestKey = k
			oldestExp = e.expires
			first = false
		}
	}
	if oldestKey != "" {
		delete(s.entries, oldestKey)
	}
}
