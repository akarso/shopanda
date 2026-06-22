package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/cache"
	goredis "github.com/redis/go-redis/v9"
)

var _ cache.Cache = (*CacheStore)(nil)

// CacheStore implements cache.Cache using Redis.
type CacheStore struct {
	client *goredis.Client
	prefix string
}

// Config holds Redis cache connection settings.
type Config struct {
	URL       string
	KeyPrefix string
}

// New creates a CacheStore and verifies the Redis connection with PING.
func New(cfg Config) (*CacheStore, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("redis cache: empty url")
	}
	opts, err := goredis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("redis cache: parse url: %w", err)
	}
	client := goredis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis cache: ping: %w", err)
	}

	prefix := cfg.KeyPrefix
	if prefix != "" && !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}
	return &CacheStore{client: client, prefix: prefix}, nil
}

func (s *CacheStore) key(k string) string {
	return s.prefix + k
}

// Get retrieves the cached value for key and unmarshals it into dest.
func (s *CacheStore) Get(key string, dest any) (bool, error) {
	raw, err := s.client.Get(context.Background(), s.key(key)).Bytes()
	if err == goredis.Nil {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("redis cache: get %q: %w", key, err)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return false, fmt.Errorf("redis cache: unmarshal %q: %w", key, err)
	}
	return true, nil
}

// Set stores value under key with the given TTL.
func (s *CacheStore) Set(key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("redis cache: marshal %q: %w", key, err)
	}
	if err := s.client.Set(context.Background(), s.key(key), data, ttl).Err(); err != nil {
		return fmt.Errorf("redis cache: set %q: %w", key, err)
	}
	return nil
}

// Delete removes the entry for key. A missing key is not an error.
func (s *CacheStore) Delete(key string) error {
	if err := s.client.Del(context.Background(), s.key(key)).Err(); err != nil {
		return fmt.Errorf("redis cache: delete %q: %w", key, err)
	}
	return nil
}

// DeleteByPrefix removes all entries whose key starts with prefix.
func (s *CacheStore) DeleteByPrefix(ctx context.Context, prefix string) error {
	match := s.key(prefix) + "*"
	iter := s.client.Scan(ctx, 0, match, 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("redis cache: scan prefix %q: %w", prefix, err)
	}
	if len(keys) == 0 {
		return nil
	}
	if err := s.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("redis cache: delete by prefix %q: %w", prefix, err)
	}
	return nil
}

// DeleteExpired is a no-op for Redis; TTL keys are evicted by the server.
func (s *CacheStore) DeleteExpired(ctx context.Context) (int64, error) {
	_ = ctx
	return 0, nil
}
