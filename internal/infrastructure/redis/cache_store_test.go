package redis_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/cache"
	inredis "github.com/akarso/shopanda/internal/infrastructure/redis"
	"github.com/alicebob/miniredis/v2"
)

func setupRedisCache(t *testing.T, keyPrefix string) (*miniredis.Miniredis, cache.Cache) {
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

func TestCacheStore_DeleteExpiredNoOp(t *testing.T) {
	_, store := setupRedisCache(t, "")

	deleted, err := store.(interface {
		DeleteExpired(context.Context) (int64, error)
	}).DeleteExpired(context.Background())
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
