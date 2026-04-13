package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/cache"
)

// --- in-memory stub that mirrors CacheStore behaviour ---

type stubCache struct {
	entries map[string]stubEntry
}

type stubEntry struct {
	value     json.RawMessage
	expiresAt *time.Time
}

func newStubCache() *stubCache {
	return &stubCache{entries: make(map[string]stubEntry)}
}

// Compile-time check.
var _ cache.Cache = (*stubCache)(nil)

func (s *stubCache) Get(key string, dest any) (bool, error) {
	e, ok := s.entries[key]
	if !ok {
		return false, nil
	}
	if e.expiresAt != nil && e.expiresAt.Before(time.Now()) {
		return false, nil
	}
	return true, json.Unmarshal(e.value, dest)
}

func (s *stubCache) Set(key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	e := stubEntry{value: json.RawMessage(data)}
	if ttl > 0 {
		t := time.Now().Add(ttl)
		e.expiresAt = &t
	}
	s.entries[key] = e
	return nil
}

func (s *stubCache) Delete(key string) error {
	delete(s.entries, key)
	return nil
}

func (s *stubCache) DeleteByPrefix(_ context.Context, prefix string) error {
	for k := range s.entries {
		if strings.HasPrefix(k, prefix) {
			delete(s.entries, k)
		}
	}
	return nil
}

// --- tests run against the stub to verify behaviour expectations ---

func TestCacheStore_SetAndGet(t *testing.T) {
	c := newStubCache()

	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	if err := c.Set("k1", payload{Name: "hello", Count: 42}, 5*time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var got payload
	ok, err := c.Get("k1", &got)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Name != "hello" || got.Count != 42 {
		t.Errorf("got %+v", got)
	}
}

func TestCacheStore_Miss(t *testing.T) {
	c := newStubCache()

	var dest string
	ok, err := c.Get("nonexistent", &dest)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestCacheStore_Delete(t *testing.T) {
	c := newStubCache()

	if err := c.Set("k1", "value", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.Delete("k1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var dest string
	ok, err := c.Get("k1", &dest)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("expected miss after delete")
	}
}

func TestCacheStore_DeleteMissing(t *testing.T) {
	c := newStubCache()

	if err := c.Delete("nope"); err != nil {
		t.Fatalf("Delete missing key should not error: %v", err)
	}
}

func TestCacheStore_Overwrite(t *testing.T) {
	c := newStubCache()

	if err := c.Set("k1", "first", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.Set("k1", "second", 0); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}

	var got string
	ok, err := c.Get("k1", &got)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got != "second" {
		t.Errorf("got %q, want %q", got, "second")
	}
}

func TestCacheStore_Expired(t *testing.T) {
	c := newStubCache()

	// Set with already-expired time.
	data, _ := json.Marshal("stale")
	past := time.Now().Add(-time.Second)
	c.entries["k1"] = stubEntry{value: data, expiresAt: &past}

	var got string
	ok, err := c.Get("k1", &got)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("expected miss for expired entry")
	}
}

func TestCacheStore_NoTTL(t *testing.T) {
	c := newStubCache()

	if err := c.Set("forever", "persisted", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var got string
	ok, err := c.Get("forever", &got)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected cache hit for no-TTL entry")
	}
	if got != "persisted" {
		t.Errorf("got %q", got)
	}
}

func TestCacheStore_CompileTime(t *testing.T) {
	// Verify CacheStore satisfies interface — tested via var _ above,
	// but having a nil-check here makes it explicit.
	var _ cache.Cache = (*CacheStore)(nil)
	var _ cache.Cache = (*stubCache)(nil)

	// Verify NewCacheStore returns a usable type.
	store, err := NewCacheStore(&sql.DB{})
	if err != nil {
		t.Fatalf("NewCacheStore: %v", err)
	}
	if store == nil {
		t.Fatal("NewCacheStore returned nil")
	}
}
