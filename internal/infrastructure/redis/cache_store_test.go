package redis_test

import (
	"context"
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
