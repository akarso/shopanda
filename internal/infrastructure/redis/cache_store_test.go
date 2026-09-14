package redis_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/cache/tagtest"
	inredis "github.com/akarso/shopanda/internal/infrastructure/redis"
	"github.com/alicebob/miniredis/v2"
)

func setupRedisCache(t *testing.T, keyPrefix string) (*miniredis.Miniredis, *inredis.CacheStore) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	store, err := inredis.New(inredis.Config{
		URL:       "redis://" + mr.Addr(),
		KeyPrefix: keyPrefix,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mr, store
}

func TestCacheStore_SetAndGet(t *testing.T) {
	_, store := setupRedisCache(t, "shopanda")

	type payload struct {
		Name string `json:"name"`
	}
	if err := store.Set("user:1", payload{Name: "Ada"}, time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var got payload
	hit, err := store.Get("user:1", &got)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !hit {
		t.Fatal("Get hit = false, want true")
	}
	if got.Name != "Ada" {
		t.Fatalf("Name = %q, want Ada", got.Name)
	}
}

func TestCacheStore_Miss(t *testing.T) {
	_, store := setupRedisCache(t, "")

	var got string
	hit, err := store.Get("missing", &got)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hit {
		t.Fatal("Get hit = true, want false")
	}
}

func TestCacheStore_Delete(t *testing.T) {
	mr, store := setupRedisCache(t, "p")

	if err := store.Set("k", "v", time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Delete("k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if mr.Exists("p:k") {
		t.Fatal("key still exists after Delete")
	}
}

func TestCacheStore_DeleteByPrefix(t *testing.T) {
	_, store := setupRedisCache(t, "cache")

	for _, key := range []string{"product:1", "product:2", "order:1"} {
		if err := store.Set(key, key, time.Minute); err != nil {
			t.Fatalf("Set %q: %v", key, err)
		}
	}
	if err := store.DeleteByPrefix(context.Background(), "product:"); err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}

	var v string
	if hit, _ := store.Get("product:1", &v); hit {
		t.Fatal("product:1 should be deleted")
	}
	if hit, _ := store.Get("order:1", &v); !hit {
		t.Fatal("order:1 should remain")
	}
}

func TestCacheStore_Expired(t *testing.T) {
	mr, store := setupRedisCache(t, "")

	if err := store.Set("ttl-key", "v", 50*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	mr.FastForward(100 * time.Millisecond)

	var got string
	hit, err := store.Get("ttl-key", &got)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hit {
		t.Fatal("Get hit = true, want false after TTL expiry")
	}
}

func TestCacheStore_Incr(t *testing.T) {
	mr, store := setupRedisCache(t, "incr")

	n, err := store.Incr("c", 1, time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("Incr miss = (%d, %v), want (1, nil)", n, err)
	}
	n, err = store.Incr("c", 1, time.Minute)
	if err != nil || n != 2 {
		t.Fatalf("Incr accumulate = (%d, %v), want (2, nil)", n, err)
	}

	// Legacy object shape continues from .count.
	if err := store.Set("legacy", map[string]any{"count": 5}, time.Minute); err != nil {
		t.Fatalf("Set legacy: %v", err)
	}
	n, err = store.Incr("legacy", 1, time.Minute)
	if err != nil || n != 6 {
		t.Fatalf("Incr legacy = (%d, %v), want (6, nil)", n, err)
	}

	// Expired key resets to delta.
	if err := store.Set("exp", int64(9), 50*time.Millisecond); err != nil {
		t.Fatalf("Set exp: %v", err)
	}
	mr.FastForward(100 * time.Millisecond)
	n, err = store.Incr("exp", 1, time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("Incr after expiry = (%d, %v), want (1, nil)", n, err)
	}
}

func TestCacheStore_IncrConcurrent(t *testing.T) {
	_, store := setupRedisCache(t, "race")
	const workers = 40
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := store.Incr("k", 1, time.Minute); err != nil {
				t.Errorf("Incr: %v", err)
			}
		}()
	}
	wg.Wait()
	var got int64
	hit, err := store.Get("k", &got)
	if err != nil || !hit || got != workers {
		t.Fatalf("Get after concurrent = hit=%v val=%d err=%v, want %d", hit, got, err, workers)
	}
}

func TestCacheStore_CompareAndSubtract(t *testing.T) {
	_, store := setupRedisCache(t, "cas")
	if _, err := store.Incr("c", 5, time.Minute); err != nil {
		t.Fatalf("Incr: %v", err)
	}
	n, err := store.CompareAndSubtract("c", 3)
	if err != nil || n != 2 {
		t.Fatalf("CompareAndSubtract = (%d, %v), want (2, nil)", n, err)
	}
	n, err = store.CompareAndSubtract("c", 9)
	if err != nil || n != 2 {
		t.Fatalf("current < expected = (%d, %v), want (2, nil)", n, err)
	}
	var still int64
	hit, err := store.Get("c", &still)
	if err != nil || !hit || still != 2 {
		t.Fatalf("Get after no-op = hit=%v val=%d err=%v, want 2", hit, still, err)
	}
	n, err = store.CompareAndSubtract("c", 2)
	if err != nil || n != 0 {
		t.Fatalf("clear = (%d, %v), want (0, nil)", n, err)
	}
	hit, err = store.Get("c", &still)
	if err != nil || hit {
		t.Fatalf("Get after clear = hit=%v err=%v, want miss", hit, err)
	}
	n, err = store.CompareAndSubtract("missing", 1)
	if err != nil || n != 0 {
		t.Fatalf("missing = (%d, %v), want (0, nil)", n, err)
	}
}

func TestCacheStore_DeleteExpiredNoOp(t *testing.T) {
	_, store := setupRedisCache(t, "")

	deleted, err := store.DeleteExpired(context.Background())
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("DeleteExpired = %d, want 0", deleted)
	}
}

func TestNew_EmptyURL(t *testing.T) {
	if _, err := inredis.New(inredis.Config{}); err == nil {
		t.Fatal("New() expected error for empty url")
	}
}

func TestNew_UnreachableRedis(t *testing.T) {
	if _, err := inredis.New(inredis.Config{URL: "redis://127.0.0.1:1"}); err == nil {
		t.Fatal("New() expected error when Redis is unreachable")
	}
}

func TestCacheStore_TagInvalidation(t *testing.T) {
	_, store := setupRedisCache(t, "shopanda")
	tagtest.Run(t, store)
}

func TestCacheStore_TagDeleteAfterExpiry(t *testing.T) {
	mr, store := setupRedisCache(t, "p")
	ctx := context.Background()

	if err := store.SetWithTags(ctx, "ttl-tagged", "v", 50*time.Millisecond, "exp-tag"); err != nil {
		t.Fatalf("SetWithTags: %v", err)
	}
	mr.FastForward(100 * time.Millisecond)

	var got string
	hit, err := store.Get("ttl-tagged", &got)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hit {
		t.Fatal("Get hit = true, want false after TTL expiry")
	}
	if _, err := store.DeleteByTag(ctx, "exp-tag"); err != nil {
		t.Fatalf("DeleteByTag after expiry: %v", err)
	}
}

func TestCacheStore_TagDeleteExpiredPrunesStaleMembers(t *testing.T) {
	mr, store := setupRedisCache(t, "p")
	ctx := context.Background()
	if err := store.SetWithTags(ctx, "ttl-tagged", "v", 50*time.Millisecond, "exp-tag"); err != nil {
		t.Fatalf("SetWithTags: %v", err)
	}
	if err := store.SetWithTags(ctx, "keep", "v", time.Hour, "exp-tag"); err != nil {
		t.Fatalf("SetWithTags keep: %v", err)
	}
	mr.FastForward(100 * time.Millisecond)

	deleted, err := store.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("DeleteExpired pruned = %d, want 1", deleted)
	}

	if _, err := store.DeleteByTag(ctx, "exp-tag"); err != nil {
		t.Fatalf("DeleteByTag: %v", err)
	}
	var got string
	hit, err := store.Get("keep", &got)
	if err != nil || hit {
		t.Fatalf("keep should be deleted by tag, hit=%v err=%v", hit, err)
	}
}

func TestCacheStore_TagPruneDoesNotDropRecreatedMembership(t *testing.T) {
	mr, store := setupRedisCache(t, "p")
	ctx := context.Background()
	for i := 0; i < 40; i++ {
		key := fmt.Sprintf("recreate:%d", i)
		tag := fmt.Sprintf("recreate-tag:%d", i)
		if err := store.SetWithTags(ctx, key, "old", 50*time.Millisecond, tag); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
		mr.FastForward(100 * time.Millisecond)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = store.DeleteExpired(ctx)
		}()
		go func() {
			defer wg.Done()
			_ = store.SetWithTags(ctx, key, "fresh", time.Hour, tag)
		}()
		wg.Wait()
		if _, err := store.DeleteByTag(ctx, tag); err != nil {
			t.Fatalf("follow-up DeleteByTag: %v", err)
		}
		var got string
		hit, err := store.Get(key, &got)
		if err != nil {
			t.Fatalf("Get(%q): %v", key, err)
		}
		if hit {
			t.Fatalf("Get(%q): hit %q, want miss (prune dropped a recreated membership)", key, got)
		}
	}
}

type captureLog struct {
	mu     sync.Mutex
	events []string
}

func (c *captureLog) Error(event string, _ error, _ map[string]interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func TestCacheStore_TagDeleteExpiredSkipsBadKey(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	log := &captureLog{}
	store, err := inredis.New(inredis.Config{
		URL:       "redis://" + mr.Addr(),
		KeyPrefix: "p",
		Logger:    log,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := store.SetWithTags(ctx, "ttl-tagged", "v", 50*time.Millisecond, "good-tag"); err != nil {
		t.Fatalf("SetWithTags: %v", err)
	}
	if err := store.SetWithTags(ctx, "keep", "v", time.Hour, "good-tag"); err != nil {
		t.Fatalf("SetWithTags keep: %v", err)
	}
	mr.FastForward(100 * time.Millisecond)
	if err := mr.Set("p:tag:poison", "not-a-set"); err != nil {
		t.Fatalf("seed poison key: %v", err)
	}

	deleted, err := store.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("DeleteExpired pruned = %d, want 1 (poison key must not abort the scan)", deleted)
	}
	log.mu.Lock()
	events := append([]string(nil), log.events...)
	log.mu.Unlock()
	found := false
	for _, e := range events {
		if e == "redis.cache.prune_tag_skipped" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("logged events = %v, want redis.cache.prune_tag_skipped", events)
	}

	if _, err := store.DeleteByTag(ctx, "good-tag"); err != nil {
		t.Fatalf("DeleteByTag: %v", err)
	}
	var got string
	hit, err := store.Get("keep", &got)
	if err != nil || hit {
		t.Fatalf("keep should be deleted by tag, hit=%v err=%v", hit, err)
	}
}

func TestCacheStore_TagDeleteByTagCountIncludesTTLEvictedMembers(t *testing.T) {
	mr, store := setupRedisCache(t, "p")
	ctx := context.Background()
	if err := store.SetWithTags(ctx, "live", "v", time.Hour, "mix-tag"); err != nil {
		t.Fatalf("SetWithTags live: %v", err)
	}
	if err := store.SetWithTags(ctx, "gone", "v", 50*time.Millisecond, "mix-tag"); err != nil {
		t.Fatalf("SetWithTags gone: %v", err)
	}
	mr.FastForward(100 * time.Millisecond)
	n, err := store.DeleteByTag(ctx, "mix-tag")
	if err != nil {
		t.Fatalf("DeleteByTag: %v", err)
	}
	if n != 2 {
		t.Fatalf("DeleteByTag count = %d, want 2 (snapshot includes the TTL-evicted member)", n)
	}
}

func TestCacheStore_TagDeleteByTagRestoresOnFailureAfterRename(t *testing.T) {
	mr, store := setupRedisCache(t, "p")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Cleanup(func() { inredis.SetAfterTagRename(store, nil) })

	if err := store.SetWithTags(ctx, "keep-me", "v", time.Hour, "restore-tag"); err != nil {
		t.Fatalf("SetWithTags: %v", err)
	}
	inredis.SetAfterTagRename(store, func() { cancel() })

	_, err := store.DeleteByTag(ctx, "restore-tag")
	if err == nil {
		t.Fatal("DeleteByTag expected error after cancelled context")
	}

	for _, k := range mr.Keys() {
		if strings.Contains(k, "__purge:tag:") {
			t.Fatalf("leaked purge key %q after failed DeleteByTag", k)
		}
	}

	var got string
	hit, err := store.Get("keep-me", &got)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !hit {
		t.Fatal("value should survive a failed DeleteByTag after restore")
	}

	n, err := store.DeleteByTag(context.Background(), "restore-tag")
	if err != nil {
		t.Fatalf("retry DeleteByTag: %v", err)
	}
	if n != 1 {
		t.Fatalf("retry DeleteByTag count = %d, want 1", n)
	}
	hit, err = store.Get("keep-me", &got)
	if err != nil || hit {
		t.Fatalf("retry should invalidate, hit=%v err=%v", hit, err)
	}
}
