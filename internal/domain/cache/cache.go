package cache

import (
	"context"
	"time"
)

// Cache abstracts key-value caching operations.
// The default implementation uses a PostgreSQL UNLOGGED table;
// plugins can provide Redis or other backends.
type Cache interface {
	// Get deserialises the cached value for key into dest.
	// Returns (true, nil) on hit, (false, nil) on miss.
	Get(key string, dest any) (bool, error)

	// Set stores value under key with the given TTL.
	// A zero TTL means the entry never expires automatically.
	Set(key string, value any, ttl time.Duration) error

	// Incr atomically adds delta to a JSON-number counter at key, refreshes TTL,
	// and returns the new value. Missing or expired keys start at 0 before applying delta.
	// A zero TTL means the entry never expires automatically.
	Incr(key string, delta int64, ttl time.Duration) (int64, error)

	// CompareAndSubtract subtracts expected from an integer counter when
	// current >= expected (JSON number or legacy {"count":N}). Deletes the key
	// when the result is 0. Returns the new count after a successful subtract
	// (0 if deleted). When the key is absent, expired, or unparseable, returns 0
	// and leaves storage unchanged. When current < expected, leaves the value
	// unchanged and returns the current count (no-op).
	CompareAndSubtract(key string, expected int64) (int64, error)

	// Delete removes the entry for key. A missing key is not an error.
	Delete(key string) error

	// DeleteByPrefix removes all entries whose key starts with prefix.
	// Used for cache invalidation when a product or price changes.
	DeleteByPrefix(ctx context.Context, prefix string) error
}
