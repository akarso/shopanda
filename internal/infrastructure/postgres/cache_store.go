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

// Incr atomically increments a JSON-number counter and refreshes TTL.
func (s *CacheStore) Incr(key string, delta int64, ttl time.Duration) (int64, error) {
	var expiresAt sql.NullTime
	if ttl > 0 {
		expiresAt = sql.NullTime{Time: time.Now().Add(ttl), Valid: true}
	}

	var newVal int64
	err := s.db.QueryRow(
		`INSERT INTO cache (key, value, expires_at)
		 VALUES ($1, to_jsonb($2::bigint), $3)
		 ON CONFLICT (key) DO UPDATE SET
		   value = to_jsonb(
		     CASE
		       WHEN cache.expires_at IS NOT NULL AND cache.expires_at < now() THEN $2::bigint
		       WHEN jsonb_typeof(cache.value) = 'number'
		            AND (cache.value #>> '{}') ~ '^-?[0-9]+$'
		         THEN (cache.value #>> '{}')::bigint + $2::bigint
		       WHEN jsonb_typeof(cache.value) = 'object'
		            AND (cache.value ? 'count')
		            AND jsonb_typeof(cache.value->'count') = 'number'
		            AND (cache.value->>'count') ~ '^-?[0-9]+$'
		         THEN (cache.value->>'count')::bigint + $2::bigint
		       ELSE $2::bigint
		     END
		   ),
		   expires_at = EXCLUDED.expires_at,
		   created_at = now()
		 RETURNING (value #>> '{}')::bigint`,
		key, delta, expiresAt,
	).Scan(&newVal)
	if err != nil {
		return 0, fmt.Errorf("cache_store: incr %q: %w", key, err)
	}
	return newVal, nil
}

// CompareAndSubtract subtracts expected from the counter when current >= expected.
// Uses a single transaction with SELECT … FOR UPDATE on key so the locked row is
// the one deleted/updated (no ctid matching). When current < expected, returns
// the unchanged current count.
func (s *CacheStore) CompareAndSubtract(key string, expected int64) (int64, error) {
	if expected <= 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("cache_store: compare-and-subtract %q: begin: %w", key, err)
	}
	defer func() { _ = tx.Rollback() }()

	var n sql.NullInt64
	err = tx.QueryRow(
		`SELECT CASE
		   WHEN expires_at IS NOT NULL AND expires_at < now() THEN NULL
		   WHEN jsonb_typeof(value) = 'number'
		        AND (value #>> '{}') ~ '^-?[0-9]+$'
		     THEN (value #>> '{}')::bigint
		   WHEN jsonb_typeof(value) = 'object'
		        AND (value ? 'count')
		        AND jsonb_typeof(value->'count') = 'number'
		        AND (value->>'count') ~ '^-?[0-9]+$'
		     THEN (value->>'count')::bigint
		   ELSE NULL
		 END
		 FROM cache
		 WHERE key = $1
		 FOR UPDATE`,
		key,
	).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("cache_store: compare-and-subtract %q: %w", key, err)
	}
	if !n.Valid {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("cache_store: compare-and-subtract %q: commit: %w", key, err)
		}
		return 0, nil
	}
	cur := n.Int64
	if cur < expected {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("cache_store: compare-and-subtract %q: commit: %w", key, err)
		}
		return cur, nil
	}
	if cur == expected {
		if _, err := tx.Exec(`DELETE FROM cache WHERE key = $1`, key); err != nil {
			return 0, fmt.Errorf("cache_store: compare-and-subtract %q: delete: %w", key, err)
		}
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("cache_store: compare-and-subtract %q: commit: %w", key, err)
		}
		return 0, nil
	}
	newVal := cur - expected
	if _, err := tx.Exec(
		`UPDATE cache SET value = to_jsonb($2::bigint), created_at = now() WHERE key = $1`,
		key, newVal,
	); err != nil {
		return 0, fmt.Errorf("cache_store: compare-and-subtract %q: update: %w", key, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("cache_store: compare-and-subtract %q: commit: %w", key, err)
	}
	return newVal, nil
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
