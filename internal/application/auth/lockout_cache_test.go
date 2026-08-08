package auth

import (
	"context"
	"encoding/json"
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
	b, err := json.Marshal(e.value)
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(b, dest)
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

func (c *stubCache) Incr(key string, delta int64, ttl time.Duration) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	var n int64
	if e, ok := c.data[key]; ok && (e.exp.IsZero() || !now.After(e.exp)) {
		b, err := json.Marshal(e.value)
		if err != nil {
			return 0, err
		}
		if err := json.Unmarshal(b, &n); err != nil {
			var obj struct {
				Count int64 `json:"count"`
			}
			if err := json.Unmarshal(b, &obj); err == nil {
				n = obj.Count
			}
		}
	}
	n += delta
	var exp time.Time
	if ttl > 0 {
		exp = now.Add(ttl)
	}
	c.data[key] = stubCached{value: n, exp: exp}
	return n, nil
}

func (c *stubCache) CompareAndSubtract(key string, expected int64) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if expected <= 0 {
		return 0, nil
	}
	now := time.Now()
	e, ok := c.data[key]
	if !ok || (!e.exp.IsZero() && now.After(e.exp)) {
		delete(c.data, key)
		return 0, nil
	}
	b, err := json.Marshal(e.value)
	if err != nil {
		return 0, err
	}
	var n int64
	if err := json.Unmarshal(b, &n); err != nil {
		var obj struct {
			Count int64 `json:"count"`
		}
		if err := json.Unmarshal(b, &obj); err != nil {
			return 0, nil
		}
		n = obj.Count
	}
	if n < expected {
		return n, nil
	}
	n -= expected
	if n == 0 {
		delete(c.data, key)
		return 0, nil
	}
	c.data[key] = stubCached{value: n, exp: e.exp}
	return n, nil
}

func (c *stubCache) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
	return nil
}

func (c *stubCache) DeleteByPrefix(_ context.Context, _ string) error { return nil }

func TestCacheAttemptStore_IncrementAndReset(t *testing.T) {
	store := NewCacheAttemptStore(newStubCache(), nil)
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
	if err := store.ResetIf(ctx, key, 1); err != nil {
		t.Fatalf("ResetIf: %v", err)
	}
	got, err = store.Failures(ctx, key)
	if err != nil || got != 0 {
		t.Fatalf("after ResetIf Failures = (%d, %v), want (0, nil)", got, err)
	}
}

func TestCacheAttemptStore_TTLExpiry(t *testing.T) {
	store := NewCacheAttemptStore(newStubCache(), nil)
	ctx := context.Background()
	key := "5.5.5.5|b@example.com"
	ttl := 20 * time.Millisecond

	if _, err := store.Increment(ctx, key, ttl); err != nil {
		t.Fatalf("Increment: %v", err)
	}
	got, err := store.Failures(ctx, key)
	if err != nil || got != 1 {
		t.Fatalf("Failures before expiry = (%d, %v), want (1, nil)", got, err)
	}

	time.Sleep(ttl + 10*time.Millisecond)
	got, err = store.Failures(ctx, key)
	if err != nil || got != 0 {
		t.Fatalf("Failures after TTL = (%d, %v), want (0, nil)", got, err)
	}
}

func TestCacheAttemptStore_LegacyObjectShape(t *testing.T) {
	c := newStubCache()
	store := NewCacheAttemptStore(c, nil)
	ctx := context.Background()
	key := "1.1.1.1|legacy@example.com"
	ck := store.cacheKey(key)

	// Simulate pre-Incr wire format still present in shared cache.
	if err := c.Set(ck, map[string]any{"count": 7}, time.Minute); err != nil {
		t.Fatalf("Set legacy: %v", err)
	}
	got, err := store.Failures(ctx, key)
	if err != nil || got != 7 {
		t.Fatalf("Failures legacy = (%d, %v), want (7, nil)", got, err)
	}
	n, err := store.Increment(ctx, key, time.Minute)
	if err != nil || n != 8 {
		t.Fatalf("Increment from legacy = (%d, %v), want (8, nil)", n, err)
	}
}

func TestParseLockoutCount_RequiresCountField(t *testing.T) {
	if _, err := parseLockoutCount([]byte(`{}`)); err == nil {
		t.Fatal("empty object should error")
	}
	if _, err := parseLockoutCount([]byte(`{"other":1}`)); err == nil {
		t.Fatal("object without count should error")
	}
	n, err := parseLockoutCount([]byte(`{"count":3}`))
	if err != nil || n != 3 {
		t.Fatalf("got (%d, %v), want (3, nil)", n, err)
	}
	n, err = parseLockoutCount([]byte(`9`))
	if err != nil || n != 9 {
		t.Fatalf("number got (%d, %v), want (9, nil)", n, err)
	}
}

func TestCacheAttemptStore_ResetIfPreservesConcurrentIncrement(t *testing.T) {
	store := NewCacheAttemptStore(newStubCache(), nil)
	ctx := context.Background()
	key := "3.3.3.3|cas@example.com"

	if _, err := store.Increment(ctx, key, time.Minute); err != nil {
		t.Fatalf("Increment: %v", err)
	}
	// Simulate concurrent failure after Failures observed 1 → counter becomes 2.
	if _, err := store.Increment(ctx, key, time.Minute); err != nil {
		t.Fatalf("Increment #2: %v", err)
	}
	if err := store.ResetIf(ctx, key, 1); err != nil {
		t.Fatalf("ResetIf: %v", err)
	}
	// Subtract observed 1; preserve the concurrent failure → 1 remaining.
	got, err := store.Failures(ctx, key)
	if err != nil || got != 1 {
		t.Fatalf("Failures after ResetIf subtract = (%d, %v), want (1, nil)", got, err)
	}
}

// TestCacheAttemptStore_StubConcurrentIncrementsPreserved checks the in-memory
// stub's mutex Incr — not postgres/redis atomicity (see infrastructure tests).
func TestCacheAttemptStore_StubConcurrentIncrementsPreserved(t *testing.T) {
	store := NewCacheAttemptStore(newStubCache(), nil)
	ctx := context.Background()
	key := "9.9.9.9|race@example.com"
	const workers = 50

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := store.Increment(ctx, key, time.Minute); err != nil {
				t.Errorf("Increment: %v", err)
			}
		}()
	}
	wg.Wait()

	got, err := store.Failures(ctx, key)
	if err != nil {
		t.Fatalf("Failures: %v", err)
	}
	if got != workers {
		t.Fatalf("Failures = %d, want %d (lost increments under concurrency)", got, workers)
	}
}
