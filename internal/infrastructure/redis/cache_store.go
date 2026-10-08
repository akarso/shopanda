package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/cache"
	goredis "github.com/redis/go-redis/v9"
)

var _ cache.Cache = (*CacheStore)(nil)

const (
	deleteByPrefixBatchSize = 1000
	tagScanCount            = 100
	purgeSetTTL             = time.Hour
	purgeRecoverTimeout     = 5 * time.Second
)

// delEmptySetScript deletes KEYS[1] only when it is an empty set, so a
// concurrent SADD cannot have its new member wiped by a delayed DEL.
var delEmptySetScript = goredis.NewScript(`
if redis.call('SCARD', KEYS[1]) == 0 then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

// deleteUntagScript deletes the value key and removes it from every tag set
// listed in the reverse index, then drops the reverse set.
// KEYS[1]=value key KEYS[2]=reverse set ARGV[1]=store prefix ARGV[2]=logical key
var deleteUntagScript = goredis.NewScript(`
local tags = redis.call('SMEMBERS', KEYS[2])
redis.call('DEL', KEYS[1])
for i = 1, #tags do
  redis.call('SREM', ARGV[1] .. 'tag:' .. tags[i], ARGV[2])
end
redis.call('DEL', KEYS[2])
return 1
`)

// deleteUntagBatchScript is deleteUntagScript for many keys in one round
// trip, used by DeleteByPrefix. KEYS alternates valueKey1, revKey1,
// valueKey2, revKey2, ... ; ARGV[1]=store prefix, ARGV[2..]=each key's
// logical name (same order as the KEYS pairs).
var deleteUntagBatchScript = goredis.NewScript(`
local prefix = ARGV[1]
for i = 2, #ARGV do
  local base = (i - 2) * 2
  local valueKey = KEYS[base + 1]
  local revKey = KEYS[base + 2]
  local logicalKey = ARGV[i]
  local tags = redis.call('SMEMBERS', revKey)
  redis.call('DEL', valueKey)
  for j = 1, #tags do
    redis.call('SREM', prefix .. 'tag:' .. tags[j], logicalKey)
  end
  redis.call('DEL', revKey)
end
return 1
`)

// flushAllDelScript DELs every KEY and returns how many of those keys
// were value keys that actually existed (not tag:/__keytags:/__purge:).
// ARGV[1] is the store prefix. Counting after DEL avoids counting a
// value that expired between SCAN and this call.
var flushAllDelScript = goredis.NewScript(`
local prefix = ARGV[1]
local n = 0
for i = 1, #KEYS do
  local full = KEYS[i]
  local logical = string.sub(full, #prefix + 1)
  local isIndex = string.sub(logical, 1, 4) == 'tag:'
    or string.sub(logical, 1, 10) == '__keytags:'
    or string.sub(logical, 1, 8) == '__purge:'
  if redis.call('DEL', full) == 1 and not isIndex then
    n = n + 1
  end
end
return n
`)

// Logger is the optional structured logger used for recoverable cache errors
// (per-tag prune skips, purge-key EXPIRE/restore failures). Nil is a no-op.
type Logger interface {
	Error(event string, err error, fields map[string]interface{})
}

// CacheStore implements cache.Cache using Redis.
type CacheStore struct {
	client *goredis.Client
	prefix string
	log    Logger
	// afterTagRename is a test-only hook, scoped to this instance so
	// parallel tests cannot leak into another store's DeleteByTag.
	afterTagRename func()
	// beforeFlushDel is a test-only hook, scoped to this instance, run
	// immediately before each FlushAll DEL batch.
	beforeFlushDel func()
}

// Config holds Redis cache connection settings.
type Config struct {
	URL       string
	KeyPrefix string
	Logger    Logger
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
	return &CacheStore{client: client, prefix: NormalizeKeyPrefix(cfg.KeyPrefix), log: cfg.Logger}, nil
}

// Client returns the underlying Redis client.
func (s *CacheStore) Client() *goredis.Client { return s.client }

// Prefix returns the store's normalized key prefix (empty or trailing ":").
func (s *CacheStore) Prefix() string { return s.prefix }

func (s *CacheStore) logError(evtName string, err error, fields map[string]interface{}) {
	if s.log == nil {
		return
	}
	s.log.Error(evtName, err, fields)
}

func (s *CacheStore) key(k string) string {
	return s.prefix + k
}

// escapeRedisScanGlob makes SCAN MATCH treat s as a literal prefix.
// Redis glob metacharacters are *, ?, [, and \.
func escapeRedisScanGlob(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '*', '?', '[', '\\':
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func scanMatchLiteralPrefix(literal string) string {
	return escapeRedisScanGlob(literal) + "*"
}

// tagKey is the Redis SET that holds cache keys associated with tag.
// Application cache keys should not use the "tag:" prefix — it is reserved
// for these membership sets (spec: tag:<name>).
func (s *CacheStore) tagKey(tag string) string {
	return s.key("tag:" + tag)
}

// keyTagsKey is the reverse index (tag names) for a logical cache key.
// Reserved, like tag: — application keys should not use the __keytags: prefix.
func (s *CacheStore) keyTagsKey(key string) string {
	return s.prefix + "__keytags:" + key
}

func (s *CacheStore) tagNameFromKey(tagKey string) string {
	return strings.TrimPrefix(tagKey, s.prefix+"tag:")
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
	return s.set(context.Background(), key, value, ttl)
}

func (s *CacheStore) set(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("redis cache: marshal %q: %w", key, err)
	}
	if err := s.client.Set(ctx, s.key(key), data, ttl).Err(); err != nil {
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
// Tag membership is dropped in the same step so a follow-up DeleteByTag
// does not count this key.
func (s *CacheStore) Delete(key string) error {
	if err := deleteUntagScript.Run(
		context.Background(),
		s.client,
		[]string{s.key(key), s.keyTagsKey(key)},
		s.prefix, key,
	).Err(); err != nil {
		return fmt.Errorf("redis cache: delete %q: %w", key, err)
	}
	return nil
}

// DeleteByPrefix removes all entries whose key starts with prefix, along
// with their tag memberships (forward tag: sets and the reverse
// __keytags: set) — the same cleanup Delete does for a single key, so a
// key later repopulated by a plain Set doesn't retain a stale tag
// association that a later DeleteByTag would wrongly act on.
// An empty store prefix plus an empty selector is refused (SCAN *).
func (s *CacheStore) DeleteByPrefix(ctx context.Context, prefix string) error {
	if s.prefix == "" && prefix == "" {
		return fmt.Errorf("redis cache: delete by prefix refused: empty key prefix would SCAN the whole Redis DB")
	}
	want := s.key(prefix)
	iter := s.client.Scan(ctx, 0, scanMatchLiteralPrefix(want), 100).Iterator()
	fullKeys := make([]string, 0, deleteByPrefixBatchSize)
	flush := func() error {
		if len(fullKeys) == 0 {
			return nil
		}
		if err := s.deleteUntagBatch(ctx, fullKeys); err != nil {
			return fmt.Errorf("redis cache: delete by prefix %q: %w", prefix, err)
		}
		fullKeys = fullKeys[:0]
		return nil
	}
	for iter.Next(ctx) {
		full := iter.Val()
		// MATCH glob plus a literal HasPrefix filter: miniredis (and some
		// Redis builds) do not honour backslash-escapes in SCAN MATCH, so
		// an unescaped "*" / "?" / "[" prefix must not wipe the store.
		if !strings.HasPrefix(full, want) {
			continue
		}
		fullKeys = append(fullKeys, full)
		if len(fullKeys) >= deleteByPrefixBatchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("redis cache: scan prefix %q: %w", prefix, err)
	}
	if err := flush(); err != nil {
		return err
	}
	return nil
}

// deleteUntagBatch deletes each already-prefixed key in fullKeys and
// removes its tag memberships, via deleteUntagBatchScript.
func (s *CacheStore) deleteUntagBatch(ctx context.Context, fullKeys []string) error {
	keys := make([]string, 0, len(fullKeys)*2)
	args := make([]any, 1, len(fullKeys)+1)
	args[0] = s.prefix
	for _, full := range fullKeys {
		logicalKey := strings.TrimPrefix(full, s.prefix)
		keys = append(keys, full, s.keyTagsKey(logicalKey))
		args = append(args, logicalKey)
	}
	if err := deleteUntagBatchScript.Run(ctx, s.client, keys, args...).Err(); err != nil {
		return fmt.Errorf("delete untag batch: %w", err)
	}
	return nil
}

// SetWithTags stores value under key and SADD's the key onto each tag set.
func (s *CacheStore) SetWithTags(ctx context.Context, key string, value any, ttl time.Duration, tags ...string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("redis cache: set with tags %q: %w", key, err)
	}
	tags = cache.UniqueTags(tags)
	if len(tags) == 0 {
		return s.set(ctx, key, value, ttl)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("redis cache: marshal %q: %w", key, err)
	}
	pipe := s.client.TxPipeline()
	pipe.Set(ctx, s.key(key), data, ttl)
	rev := s.keyTagsKey(key)
	for _, tag := range tags {
		pipe.SAdd(ctx, s.tagKey(tag), key)
		pipe.SAdd(ctx, rev, tag)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis cache: set with tags %q: %w", key, err)
	}
	return nil
}

// DeleteByTag snapshot-isolates the tag set with RENAME, then SSCAN+DEL
// members so a concurrent SADD creates a new set at the original key
// instead of having its membership destroyed. SSCAN avoids blocking Redis
// with a single O(N) SMEMBERS of a large tag. The returned count is how
// many value keys Del actually removed (TTL-evicted members are not
// counted). The purge key is given a TTL immediately; any failure after
// RENAME merges remaining members back onto the live tag key
// (independent of the caller's context) so a retry can still invalidate
// them.
func (s *CacheStore) DeleteByTag(ctx context.Context, tag string) (n int64, err error) {
	tag = cache.NormalizeTag(tag)
	if tag == "" {
		return 0, nil
	}
	tagKey := s.tagKey(tag)
	tmpKey, err := s.purgeKey(tag)
	if err != nil {
		return 0, fmt.Errorf("redis cache: delete by tag %q: %w", tag, err)
	}
	if err := s.client.Rename(ctx, tagKey, tmpKey).Err(); err != nil {
		if isNoSuchKey(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("redis cache: rename tag %q: %w", tag, err)
	}
	// Registered before expirePurge (not after) so a failure to set the
	// TTL is itself covered: without a TTL and without being restored, a
	// purge key that outlives this call (a later step here fails, or the
	// process dies) would sit forever with its members permanently
	// unreachable through the live tag key.
	defer func() {
		if err != nil {
			s.restorePurge(tagKey, tmpKey)
		}
	}()
	if err = s.expirePurge(tmpKey); err != nil {
		return n, fmt.Errorf("redis cache: delete by tag %q: %w", tag, err)
	}
	if s.afterTagRename != nil {
		s.afterTagRename()
	}
	if err = ctx.Err(); err != nil {
		return n, fmt.Errorf("redis cache: delete by tag %q: %w", tag, err)
	}

	// Deliberately a plain Del of the value+reverse keys, NOT
	// deleteUntagBatch: deleteUntagBatch reads each member's CURRENT
	// (live) reverse index to SREM it from every OTHER tag it's in too —
	// but by the time this loop reaches a member, a concurrent
	// SetWithTags may have already re-tagged it (additive — old tags
	// never get removed) with a tag added AFTER this call's own RENAME
	// snapshot. Reaching into that live reverse index would then SREM the
	// member from a tag set it was just, freshly, legitimately added to —
	// destroying an association the documented contract says must
	// survive (a concurrent SetWithTags commit after the snapshot). A
	// leftover forward-set entry for that other tag, pointing at a value
	// key this call is about to delete, is instead left for DeleteExpired
	// to sweep — the same accepted, already-tested eventual-consistency
	// path as any other orphan (see its own doc comment).
	valueKeys := make([]string, 0, deleteByPrefixBatchSize)
	metaKeys := make([]string, 0, deleteByPrefixBatchSize)
	flush := func() error {
		if len(valueKeys) > 0 {
			deleted, delErr := s.client.Del(ctx, valueKeys...).Result()
			if delErr != nil {
				return fmt.Errorf("redis cache: delete by tag %q: %w", tag, delErr)
			}
			n += deleted
			valueKeys = valueKeys[:0]
		}
		if len(metaKeys) > 0 {
			if err := s.client.Del(ctx, metaKeys...).Err(); err != nil {
				return fmt.Errorf("redis cache: delete by tag %q: keytags: %w", tag, err)
			}
			metaKeys = metaKeys[:0]
		}
		return nil
	}
	iter := s.client.SScan(ctx, tmpKey, 0, "", tagScanCount).Iterator()
	for iter.Next(ctx) {
		member := iter.Val()
		valueKeys = append(valueKeys, s.key(member))
		metaKeys = append(metaKeys, s.keyTagsKey(member))
		if len(valueKeys) >= deleteByPrefixBatchSize {
			if err = flush(); err != nil {
				return n, err
			}
		}
	}
	if err = iter.Err(); err != nil {
		return n, fmt.Errorf("redis cache: sscan tag %q: %w", tag, err)
	}
	if err = flush(); err != nil {
		return n, err
	}
	if err = s.client.Del(ctx, tmpKey).Err(); err != nil {
		return n, fmt.Errorf("redis cache: delete by tag %q: purge set: %w", tag, err)
	}
	return n, nil
}

// expirePurge sets tmpKey's TTL and, on failure, both logs and returns the
// error — the caller (DeleteByTag) treats this as fatal for the call and
// restores membership rather than continuing with an unbounded purge key
// (see DeleteByTag's own comment on why the restore defer is registered
// before this call, not after).
func (s *CacheStore) expirePurge(tmpKey string) error {
	ctx, cancel := context.WithTimeout(context.Background(), purgeRecoverTimeout)
	defer cancel()
	if err := s.client.Expire(ctx, tmpKey, purgeSetTTL).Err(); err != nil {
		s.logError("redis.cache.purge_expire_failed", err, map[string]interface{}{
			"key": tmpKey,
			"ttl": purgeSetTTL.String(),
		})
		return err
	}
	return nil
}

// restorePurge merges tmpKey's remaining members back onto tagKey and
// removes tmpKey. Used on any DeleteByTag failure after the rename, since
// expiry sweeps only scan the live tag: prefix (s.prefix+"tag:*") — a
// __purge: key is never picked up by DeleteExpired's own membership sweep,
// so tmpKey's TTL (set by expirePurge) is the only thing that would ever
// reclaim it if this restore itself fails.
func (s *CacheStore) restorePurge(tagKey, tmpKey string) {
	ctx, cancel := context.WithTimeout(context.Background(), purgeRecoverTimeout)
	defer cancel()
	if err := s.client.SUnionStore(ctx, tagKey, tagKey, tmpKey).Err(); err != nil {
		// Leave tmpKey in place (it has a TTL) so a later retry or expiry
		// sweep can still recover membership.
		s.logError("redis.cache.purge_restore_failed", err, map[string]interface{}{
			"tag_key":   tagKey,
			"purge_key": tmpKey,
		})
		return
	}
	if err := s.client.Del(ctx, tmpKey).Err(); err != nil {
		s.logError("redis.cache.purge_restore_cleanup_failed", err, map[string]interface{}{
			"purge_key": tmpKey,
		})
	}
}

func (s *CacheStore) purgeKey(tag string) (string, error) {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("purge nonce: %w", err)
	}
	return s.prefix + "__purge:tag:" + tag + ":" + hex.EncodeToString(nonce[:]), nil
}

func isNoSuchKey(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, goredis.Nil) {
		return true
	}
	return goredis.HasErrorPrefix(err, "no such key")
}

// DeleteExpired prunes tag-set members whose value key is already gone
// (TTL eviction or Delete). Value keys themselves are expired by Redis.
// Cost is proportional to total tag-set membership (Lua EXISTS+SREM
// batches, not a TOCTOU pair of round trips per member).
func (s *CacheStore) DeleteExpired(ctx context.Context) (int64, error) {
	tagPrefix := s.prefix + "tag:"
	iter := s.client.Scan(ctx, 0, scanMatchLiteralPrefix(tagPrefix), tagScanCount).Iterator()
	var pruned int64
	for iter.Next(ctx) {
		tagKey := iter.Val()
		if !strings.HasPrefix(tagKey, tagPrefix) {
			continue
		}
		n, err := s.pruneTagSet(ctx, tagKey)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return pruned, err
			}
			// One bad or flaky tag key must not abort the rest of this tick —
			// a reserved-prefix collision would otherwise permanently stall
			// pruning of every subsequent tag until someone deleted that key.
			s.logError("redis.cache.prune_tag_skipped", err, map[string]interface{}{
				"key": tagKey,
			})
			continue
		}
		pruned += n
	}
	if err := iter.Err(); err != nil {
		return pruned, fmt.Errorf("redis cache: scan tag sets: %w", err)
	}
	return pruned, nil
}

func (s *CacheStore) pruneTagSet(ctx context.Context, tagKey string) (int64, error) {
	typ, err := s.client.Type(ctx, tagKey).Result()
	if err != nil {
		return 0, fmt.Errorf("redis cache: type %q: %w", tagKey, err)
	}
	if typ == "none" {
		return 0, nil
	}
	if typ != "set" {
		return 0, fmt.Errorf("redis cache: prune %q: expected set, got %s (cache keys must not use the reserved tag: prefix)", tagKey, typ)
	}

	var pruned int64
	batch := make([]string, 0, tagScanCount)
	flush := func() error {
		n, err := s.pruneStaleBatch(ctx, tagKey, s.tagNameFromKey(tagKey), batch)
		batch = batch[:0]
		pruned += n
		return err
	}
	iter := s.client.SScan(ctx, tagKey, 0, "", tagScanCount).Iterator()
	for iter.Next(ctx) {
		batch = append(batch, iter.Val())
		if len(batch) >= tagScanCount {
			if err := flush(); err != nil {
				return pruned, err
			}
		}
	}
	if err := iter.Err(); err != nil {
		return pruned, fmt.Errorf("redis cache: sscan %q: %w", tagKey, err)
	}
	if err := flush(); err != nil {
		return pruned, err
	}
	if _, err := delEmptySetScript.Run(ctx, s.client, []string{tagKey}).Result(); err != nil {
		return pruned, fmt.Errorf("redis cache: del empty tag set %q: %w", tagKey, err)
	}
	return pruned, nil
}

// sremIfMissingScript SREMs members whose value key is gone, atomically
// with the EXISTS check so a concurrent SetWithTags cannot have its
// fresh membership stripped by a stale miss.
// KEYS[1]=tag set ARGV[1]=key prefix ARGV[2]=logical tag name ARGV[3..]=members
var sremIfMissingScript = goredis.NewScript(`
local tag = KEYS[1]
local prefix = ARGV[1]
local tagName = ARGV[2]
local pruned = 0
for i = 3, #ARGV do
  local member = ARGV[i]
  if redis.call('EXISTS', prefix .. member) == 0 then
    pruned = pruned + redis.call('SREM', tag, member)
    local rev = prefix .. '__keytags:' .. member
    redis.call('SREM', rev, tagName)
    if redis.call('SCARD', rev) == 0 then
      redis.call('DEL', rev)
    end
  end
end
return pruned
`)

func (s *CacheStore) pruneStaleBatch(ctx context.Context, tagKey, tagName string, members []string) (int64, error) {
	if len(members) == 0 {
		return 0, nil
	}
	args := make([]any, 2+len(members))
	args[0] = s.prefix
	args[1] = tagName
	for i, member := range members {
		args[i+2] = member
	}
	n, err := sremIfMissingScript.Run(ctx, s.client, []string{tagKey}, args...).Int64()
	if err != nil {
		return 0, fmt.Errorf("redis cache: srem if missing: %w", err)
	}
	return n, nil
}

// Stats reports Redis DBSIZE as Keys (the whole configured database, not
// prefix-scoped — that's what operators asked for as an approximate
// occupancy signal) plus used_memory from INFO memory when present.
func (s *CacheStore) Stats(ctx context.Context) (cache.Stats, error) {
	dbsize, err := s.client.DBSize(ctx).Result()
	if err != nil {
		return cache.Stats{}, fmt.Errorf("redis cache: stats dbsize: %w", err)
	}
	st := cache.Stats{Backend: "redis", Keys: dbsize}
	info, err := s.client.Info(ctx, "memory").Result()
	if err != nil {
		return st, nil
	}
	if mem, ok := parseRedisUsedMemory(info); ok {
		st.MemoryUsedBytes = &mem
	}
	return st, nil
}

func parseRedisUsedMemory(info string) (int64, bool) {
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if !strings.HasPrefix(line, "used_memory:") {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimPrefix(line, "used_memory:"), 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// FlushAll scan-and-deletes every key under this store's prefix,
// including tag:/__keytags:/__purge: indexes. It never calls FLUSHDB or
// FLUSHALL. The returned count is value keys actually removed by DEL
// (not index keys, and not keys that expired between SCAN and DEL).
// SCAN is cursor-based, not a point-in-time snapshot: a concurrent
// SetWithTags can leave tag-index orphans that DeleteExpired later
// reclaims. An empty prefix is refused — MATCH * would delete every
// key in the configured Redis DB.
func (s *CacheStore) FlushAll(ctx context.Context) (int64, error) {
	if s.prefix == "" {
		return 0, fmt.Errorf("redis cache: flush all refused: empty key prefix would SCAN the whole Redis DB")
	}
	iter := s.client.Scan(ctx, 0, scanMatchLiteralPrefix(s.prefix), tagScanCount).Iterator()
	var n int64
	batch := make([]string, 0, deleteByPrefixBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if s.beforeFlushDel != nil {
			s.beforeFlushDel()
		}
		deleted, err := flushAllDelScript.Run(ctx, s.client, batch, s.prefix).Int64()
		if err != nil {
			return fmt.Errorf("redis cache: flush all: %w", err)
		}
		n += deleted
		batch = batch[:0]
		return nil
	}
	for iter.Next(ctx) {
		full := iter.Val()
		if !strings.HasPrefix(full, s.prefix) {
			continue
		}
		batch = append(batch, full)
		if len(batch) >= deleteByPrefixBatchSize {
			if err := flush(); err != nil {
				return n, err
			}
		}
	}
	if err := iter.Err(); err != nil {
		return n, fmt.Errorf("redis cache: flush all scan: %w", err)
	}
	if err := flush(); err != nil {
		return n, err
	}
	return n, nil
}
