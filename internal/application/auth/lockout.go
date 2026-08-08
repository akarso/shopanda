package auth

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/akarso/shopanda/internal/domain/cache"
)

// AttemptStore tracks failed password-login attempts for lockout.
type AttemptStore interface {
	Failures(ctx context.Context, key string) (int, error)
	Increment(ctx context.Context, key string, window time.Duration) (int, error)
	Reset(ctx context.Context, key string) error
}

// LockoutSettings configures failed-login lockout on Service.
type LockoutSettings struct {
	Enabled     bool
	MaxFailures int
	Window      time.Duration
}

// DefaultMaxLockoutEntries caps in-memory lockout keys (single-instance store).
const DefaultMaxLockoutEntries = 100_000

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
func NewAttemptStore(store string, c cache.Cache) (AttemptStore, error) {
	switch strings.ToLower(strings.TrimSpace(store)) {
	case "", "cache":
		if c == nil {
			return nil, fmt.Errorf("auth lockout: store=cache requires a cache backend")
		}
		return NewCacheAttemptStore(c), nil
	case "memory":
		return NewMemoryAttemptStore(DefaultMaxLockoutEntries), nil
	default:
		return nil, fmt.Errorf("auth lockout: unsupported store %q (allowed: cache, memory)", store)
	}
}

type lockoutEntry struct {
	Count int `json:"count"`
}

// CacheAttemptStore persists counters in cache.Cache with TTL = lockout window.
type CacheAttemptStore struct {
	cache cache.Cache
}

// NewCacheAttemptStore wraps a shared cache for lockout counters.
func NewCacheAttemptStore(c cache.Cache) *CacheAttemptStore {
	return &CacheAttemptStore{cache: c}
}

func (s *CacheAttemptStore) cacheKey(key string) string {
	return lockoutCachePrefix + key
}

func (s *CacheAttemptStore) Failures(_ context.Context, key string) (int, error) {
	var entry lockoutEntry
	hit, err := s.cache.Get(s.cacheKey(key), &entry)
	if err != nil {
		return 0, fmt.Errorf("auth lockout cache get: %w", err)
	}
	if !hit {
		return 0, nil
	}
	return entry.Count, nil
}

func (s *CacheAttemptStore) Increment(_ context.Context, key string, window time.Duration) (int, error) {
	var entry lockoutEntry
	hit, err := s.cache.Get(s.cacheKey(key), &entry)
	if err != nil {
		return 0, fmt.Errorf("auth lockout cache get: %w", err)
	}
	if !hit {
		entry.Count = 0
	}
	entry.Count++
	if err := s.cache.Set(s.cacheKey(key), entry, window); err != nil {
		return 0, fmt.Errorf("auth lockout cache set: %w", err)
	}
	return entry.Count, nil
}

func (s *CacheAttemptStore) Reset(_ context.Context, key string) error {
	if err := s.cache.Delete(s.cacheKey(key)); err != nil {
		return fmt.Errorf("auth lockout cache delete: %w", err)
	}
	return nil
}

type memoryEntry struct {
	count   int
	expires time.Time
}

// MemoryAttemptStore is a bounded in-process counter store (single-instance only).
type MemoryAttemptStore struct {
	mu         sync.Mutex
	entries    map[string]memoryEntry
	maxEntries int
}

// NewMemoryAttemptStore creates a TTL-bounded memory store with a max entry cap.
func NewMemoryAttemptStore(maxEntries int) *MemoryAttemptStore {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxLockoutEntries
	}
	return &MemoryAttemptStore{
		entries:    make(map[string]memoryEntry),
		maxEntries: maxEntries,
	}
}

func (s *MemoryAttemptStore) Failures(_ context.Context, key string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpiredLocked(time.Now())
	e, ok := s.entries[key]
	if !ok || time.Now().After(e.expires) {
		return 0, nil
	}
	return e.count, nil
}

func (s *MemoryAttemptStore) Increment(_ context.Context, key string, window time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.evictExpiredLocked(now)

	e, ok := s.entries[key]
	if !ok || now.After(e.expires) {
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
	e.expires = now.Add(window)
	s.entries[key] = e
	return e.count, nil
}

func (s *MemoryAttemptStore) Reset(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
	return nil
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
