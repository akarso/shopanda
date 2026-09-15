package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/cache/tagtest"
)

// --- in-memory stub that mirrors CacheStore behaviour ---

type stubCache struct {
	mu      sync.Mutex
	entries map[string]stubEntry
	tags    map[string]map[string]struct{}
}

type stubEntry struct {
	value     json.RawMessage
	expiresAt *time.Time
}

func newStubCache() *stubCache {
	return &stubCache{
		entries: make(map[string]stubEntry),
		tags:    make(map[string]map[string]struct{}),
	}
}

// Compile-time check.
var _ cache.Cache = (*stubCache)(nil)

func (s *stubCache) Get(key string, dest any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setLocked(key, value, ttl)
}

func (s *stubCache) setLocked(key string, value any, ttl time.Duration) error {
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

func (s *stubCache) Incr(key string, delta int64, ttl time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	var n int64
	if e, ok := s.entries[key]; ok {
		if e.expiresAt == nil || !e.expiresAt.Before(now) {
			if err := json.Unmarshal(e.value, &n); err != nil {
				var obj struct {
					Count int64 `json:"count"`
				}
				if err := json.Unmarshal(e.value, &obj); err == nil {
					n = obj.Count
				}
			}
		}
	}
	n += delta
	data, err := json.Marshal(n)
	if err != nil {
		return 0, err
	}
	e := stubEntry{value: json.RawMessage(data)}
	if ttl > 0 {
		t := now.Add(ttl)
		e.expiresAt = &t
	}
	s.entries[key] = e
	return n, nil
}

func (s *stubCache) CompareAndSubtract(key string, expected int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected <= 0 {
		return 0, nil
	}
	now := time.Now()
	e, ok := s.entries[key]
	if !ok || (e.expiresAt != nil && e.expiresAt.Before(now)) {
		delete(s.entries, key)
		return 0, nil
	}
	var n int64
	if err := json.Unmarshal(e.value, &n); err != nil {
		var obj struct {
			Count int64 `json:"count"`
		}
		if err := json.Unmarshal(e.value, &obj); err != nil {
			return 0, nil
		}
		n = obj.Count
	}
	if n < expected {
		return n, nil
	}
	n -= expected
	if n == 0 {
		delete(s.entries, key)
		return 0, nil
	}
	data, err := json.Marshal(n)
	if err != nil {
		return 0, err
	}
	s.entries[key] = stubEntry{value: json.RawMessage(data), expiresAt: e.expiresAt}
	return n, nil
}

// untagLocked removes key from every tag's membership set, dropping a tag
// entirely once it has no members left. Callers must hold s.mu.
func (s *stubCache) untagLocked(key string) {
	for tag, keys := range s.tags {
		delete(keys, key)
		if len(keys) == 0 {
			delete(s.tags, tag)
		}
	}
}

func (s *stubCache) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
	s.untagLocked(key)
	return nil
}

func (s *stubCache) DeleteByPrefix(_ context.Context, prefix string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.entries {
		if strings.HasPrefix(k, prefix) {
			delete(s.entries, k)
			s.untagLocked(k)
		}
	}
	return nil
}

func (s *stubCache) SetWithTags(ctx context.Context, key string, value any, ttl time.Duration, tags ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.setLocked(key, value, ttl); err != nil {
		return err
	}
	if s.tags == nil {
		s.tags = make(map[string]map[string]struct{})
	}
	for _, tag := range cache.UniqueTags(tags) {
		keys, ok := s.tags[tag]
		if !ok {
			keys = make(map[string]struct{})
			s.tags[tag] = keys
		}
		keys[key] = struct{}{}
	}
	return nil
}

func (s *stubCache) DeleteByTag(_ context.Context, tag string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tag = cache.NormalizeTag(tag)
	if tag == "" {
		return 0, nil
	}
	keys := s.tags[tag]
	delete(s.tags, tag)
	var n int64
	for key := range keys {
		delete(s.entries, key)
		s.untagLocked(key)
		n++
	}
	return n, nil
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

func TestStubCache_DeleteByPrefix(t *testing.T) {
	c := newStubCache()

	for _, key := range []string{
		"product:123:en:EUR",
		"product:123:de:EUR",
		"product:456:en:EUR",
		"other:key",
	} {
		if err := c.Set(key, "v", 0); err != nil {
			t.Fatalf("Set(%q): %v", key, err)
		}
	}

	if err := c.DeleteByPrefix(context.Background(), "product:123:"); err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}

	// product:123:* should be deleted.
	for _, key := range []string{"product:123:en:EUR", "product:123:de:EUR"} {
		if _, ok := c.entries[key]; ok {
			t.Errorf("%q should be deleted", key)
		}
	}
	// Others should remain.
	for _, key := range []string{"product:456:en:EUR", "other:key"} {
		if _, ok := c.entries[key]; !ok {
			t.Errorf("%q should remain", key)
		}
	}
}

func TestStubCache_TagInvalidation(t *testing.T) {
	tagtest.Run(t, newStubCache())
}
