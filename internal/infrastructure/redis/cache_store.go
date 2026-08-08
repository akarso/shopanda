package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/cache"
	goredis "github.com/redis/go-redis/v9"
)

var _ cache.Cache = (*CacheStore)(nil)

const deleteByPrefixBatchSize = 1000

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
	client, err := ConnectURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("redis cache: init client: %w", err)
	}
	return &CacheStore{client: client, prefix: NormalizeKeyPrefix(cfg.KeyPrefix)}, nil
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

// incrScript atomically increments a JSON-number counter and refreshes TTL (PX ms).
// KEYS[1]=key ARGV[1]=delta ARGV[2]=ttl_ms (0 = no expiry).
var incrScript = goredis.NewScript(`
local raw = redis.call('GET', KEYS[1])
local n = 0
if raw then
  local ok, decoded = pcall(cjson.decode, raw)
  if ok and type(decoded) == 'number' then
    n = decoded
  elseif ok and type(decoded) == 'table' and decoded.count ~= nil then
    n = tonumber(decoded.count) or 0
  end
end
n = n + tonumber(ARGV[1])
local ttl = tonumber(ARGV[2])
if ttl > 0 then
  redis.call('SET', KEYS[1], cjson.encode(n), 'PX', ttl)
else
  redis.call('SET', KEYS[1], cjson.encode(n))
end
return n
`)

// Incr atomically increments a JSON-number counter and refreshes TTL.
func (s *CacheStore) Incr(key string, delta int64, ttl time.Duration) (int64, error) {
	ttlMs := int64(0)
	if ttl > 0 {
		ttlMs = ttl.Milliseconds()
		if ttlMs < 1 {
			ttlMs = 1
		}
	}
	res, err := incrScript.Run(context.Background(), s.client, []string{s.key(key)}, delta, ttlMs).Int64()
	if err != nil {
		return 0, fmt.Errorf("redis cache: incr %q: %w", key, err)
	}
	return res, nil
}

// compareAndSubtractScript subtracts ARGV[1] when current >= ARGV[1].
// Deletes the key when the result is 0. Returns the new count on success,
// 0 when absent/unparseable, or the unchanged current when current < expected.
var compareAndSubtractScript = goredis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then
  return 0
end
local ok, decoded = pcall(cjson.decode, raw)
local n = nil
if ok and type(decoded) == 'number' then
  if decoded == math.floor(decoded) then
    n = decoded
  end
elseif ok and type(decoded) == 'table' and decoded.count ~= nil then
  local c = tonumber(decoded.count)
  if c ~= nil and c == math.floor(c) then
    n = c
  end
end
if n == nil then
  return 0
end
local expected = tonumber(ARGV[1])
if n < expected then
  return n
end
n = n - expected
if n == 0 then
  redis.call('DEL', KEYS[1])
  return 0
end
local ttl = redis.call('PTTL', KEYS[1])
redis.call('SET', KEYS[1], cjson.encode(n))
if ttl > 0 then
  redis.call('PEXPIRE', KEYS[1], ttl)
end
return n
`)

// CompareAndSubtract subtracts expected from the counter when current >= expected.
// When current < expected, leaves the value unchanged and returns current.
func (s *CacheStore) CompareAndSubtract(key string, expected int64) (int64, error) {
	if expected <= 0 {
		return 0, nil
	}
	n, err := compareAndSubtractScript.Run(context.Background(), s.client, []string{s.key(key)}, expected).Int64()
	if err != nil {
		return 0, fmt.Errorf("redis cache: compare-and-subtract %q: %w", key, err)
	}
	return n, nil
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
	keys := make([]string, 0, deleteByPrefixBatchSize)
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
		if len(keys) >= deleteByPrefixBatchSize {
			if err := s.client.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("redis cache: delete by prefix %q: %w", prefix, err)
			}
			keys = keys[:0]
		}
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("redis cache: scan prefix %q: %w", prefix, err)
	}
	if len(keys) > 0 {
		if err := s.client.Del(ctx, keys...).Err(); err != nil {
			return fmt.Errorf("redis cache: delete by prefix %q: %w", prefix, err)
		}
	}
	return nil
}

// DeleteExpired is a no-op for Redis; TTL keys are evicted by the server.
func (s *CacheStore) DeleteExpired(ctx context.Context) (int64, error) {
	_ = ctx
	return 0, nil
}
