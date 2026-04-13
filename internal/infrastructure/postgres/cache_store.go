package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/cache"
)

// Compile-time check.
var _ cache.Cache = (*CacheStore)(nil)

// CacheStore implements cache.Cache using a PostgreSQL UNLOGGED table.
type CacheStore struct {
	db *sql.DB
}

// NewCacheStore returns a CacheStore backed by db.
func NewCacheStore(db *sql.DB) (*CacheStore, error) {
	if db == nil {
		return nil, fmt.Errorf("NewCacheStore: nil *sql.DB")
	}
	return &CacheStore{db: db}, nil
}

// Get retrieves the cached value for key and unmarshals it into dest.
// Returns (true, nil) on hit, (false, nil) on miss.
func (s *CacheStore) Get(key string, dest any) (bool, error) {
	var raw json.RawMessage
	var expiresAt sql.NullTime

	err := s.db.QueryRow(
		`SELECT value, expires_at FROM cache WHERE key = $1`,
		key,
	).Scan(&raw, &expiresAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cache_store: get %q: %w", key, err)
	}

	// Check expiration in application code so we never return stale data
	// even if the cleanup job hasn't run yet.
	if expiresAt.Valid && expiresAt.Time.Before(time.Now()) {
		return false, nil
	}

	if err := json.Unmarshal(raw, dest); err != nil {
		return false, fmt.Errorf("cache_store: unmarshal %q: %w", key, err)
	}
	return true, nil
}

// Set stores value under key with the given TTL.
// A zero TTL means the entry never expires automatically.
func (s *CacheStore) Set(key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache_store: marshal %q: %w", key, err)
	}

	var expiresAt sql.NullTime
	if ttl > 0 {
		expiresAt = sql.NullTime{Time: time.Now().Add(ttl), Valid: true}
	}

	_, err = s.db.Exec(
		`INSERT INTO cache (key, value, expires_at)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (key)
		 DO UPDATE SET value = EXCLUDED.value,
		               expires_at = EXCLUDED.expires_at,
		               created_at = now()`,
		key, data, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("cache_store: set %q: %w", key, err)
	}
	return nil
}

// Delete removes the entry for key. A missing key is not an error.
func (s *CacheStore) Delete(key string) error {
	_, err := s.db.Exec(`DELETE FROM cache WHERE key = $1`, key)
	if err != nil {
		return fmt.Errorf("cache_store: delete %q: %w", key, err)
	}
	return nil
}

// DeleteByPrefix removes all entries whose key starts with prefix.
// LIKE metacharacters (%, _, \) in prefix are escaped so only true
// prefix matches are deleted.
func (s *CacheStore) DeleteByPrefix(ctx context.Context, prefix string) error {
	escaped := escapeLike(prefix)
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM cache WHERE key LIKE $1 ESCAPE '\'`,
		escaped+"%",
	)
	if err != nil {
		return fmt.Errorf("cache_store: delete by prefix %q: %w", prefix, err)
	}
	return nil
}

// escapeLike escapes LIKE metacharacters so they match literally.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// DeleteExpired removes all entries whose TTL has elapsed.
// Called by the cache cleanup scheduled job.
func (s *CacheStore) DeleteExpired(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM cache WHERE expires_at < now()`)
	if err != nil {
		return 0, fmt.Errorf("cache_store: delete expired: %w", err)
	}
	return res.RowsAffected()
}
