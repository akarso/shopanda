package auth

import (
	"context"
	"sync"
	"testing"
	"time"
)

type stubCache struct {
	mu   sync.Mutex
	data map[string]stubCached
}

type stubCached struct {
	value any
	exp   time.Time
}

func newStubCache() *stubCache {
	return &stubCache{data: make(map[string]stubCached)}
}

func (c *stubCache) Get(key string, dest any) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.data[key]
	if !ok || (!e.exp.IsZero() && time.Now().After(e.exp)) {
		return false, nil
	}
	ptr, ok := dest.(*lockoutEntry)
	if !ok {
		return false, nil
	}
	*ptr = e.value.(lockoutEntry)
	return true, nil
}

func (c *stubCache) Set(key string, value any, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	c.data[key] = stubCached{value: value, exp: exp}
	return nil
}

func (c *stubCache) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
	return nil
}

func (c *stubCache) DeleteByPrefix(_ context.Context, _ string) error { return nil }

func TestCacheAttemptStore_IncrementAndReset(t *testing.T) {
	store := NewCacheAttemptStore(newStubCache())
	ctx := context.Background()
	key := "1.2.3.4|a@example.com"

	n, err := store.Increment(ctx, key, time.Minute)
	if err != nil || n != 1 {
		t.Fatalf("Increment = (%d, %v), want (1, nil)", n, err)
	}
	got, err := store.Failures(ctx, key)
	if err != nil || got != 1 {
		t.Fatalf("Failures = (%d, %v), want (1, nil)", got, err)
	}
	if err := store.Reset(ctx, key); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	got, err = store.Failures(ctx, key)
	if err != nil || got != 0 {
		t.Fatalf("after Reset Failures = (%d, %v), want (0, nil)", got, err)
	}
}
