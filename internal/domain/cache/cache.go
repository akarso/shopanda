package cache

import "time"

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

	// Delete removes the entry for key. A missing key is not an error.
	Delete(key string) error
}
