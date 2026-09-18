// Package localcache is a bounded, TTL+LRU, process-local (L1) cache
// tier that sits in front of the existing cache.Cache backend ("L2") for
// a narrow, explicitly allowlisted set of low-churn, read-heavy,
// bounded-staleness-tolerant data (PR-1040).
//
// This is deliberately NOT a cache.Cache implementation, and never will
// be — making Store look like a general-purpose, drop-in cache backend
// would make it too easy to accidentally put invalidation-sensitive data
// (anything gated by per-request RBAC context, anything with a tag that
// changes more than a few times a minute, anything containing customer/
// session/cart data) into a tier that only propagates invalidation on a
// best-effort, same-process basis — see the package's own broadcast.go
// for exactly how limited that basis is. Every call site that reaches for
// a Store must justify itself against that rule in its own comment, not
// just use it because it was convenient.
package localcache

import (
	"container/list"
	"sync"
	"time"
)

type entry[T any] struct {
	key       string
	value     T
	expiresAt time.Time
}

// Store is a bounded-size, per-entry-TTL, LRU-eviction cache safe for
// concurrent use. Zero value is not usable — construct with New.
type Store[T any] struct {
	mu         sync.Mutex
	maxEntries int
	ttl        time.Duration
	items      map[string]*list.Element
	order      *list.List // front = most recently used
	// generation counts invalidations (Clear/Delete) — see GetOrLoad's
	// own comment for why it needs one.
	generation uint64
}

// New returns an empty Store bounded to maxEntries, with every entry
// expiring ttl after it was last written (Set resets the deadline; Get
// does not — this is a write-through freshness bound, not a sliding
// idle timer, so a hot key can't stay stale forever just by being read
// often).
func New[T any](maxEntries int, ttl time.Duration) *Store[T] {
	if maxEntries <= 0 {
		panic("localcache: maxEntries must be positive")
	}
	if ttl <= 0 {
		panic("localcache: ttl must be positive")
	}
	return &Store[T]{
		maxEntries: maxEntries,
		ttl:        ttl,
		items:      make(map[string]*list.Element),
		order:      list.New(),
	}
}

// Get returns the value cached for key and true, or the zero value and
// false if key is absent or its entry has expired. A hit moves key to
// the front of the LRU order.
func (s *Store[T]) Get(key string) (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	el, ok := s.items[key]
	if !ok {
		var zero T
		return zero, false
	}
	e := el.Value.(*entry[T])
	if time.Now().After(e.expiresAt) {
		s.removeElementLocked(el)
		var zero T
		return zero, false
	}
	s.order.MoveToFront(el)
	return e.value, true
}

// Set stores value under key, resetting its TTL, and evicts the
// least-recently-used entry if this write pushes the store past
// maxEntries.
func (s *Store[T]) Set(key string, value T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setLocked(key, value)
}

// setLocked is Set's own body, factored out so GetOrLoad can skip the
// write conditionally (see its own comment). Callers must hold s.mu.
func (s *Store[T]) setLocked(key string, value T) {
	now := time.Now()
	if el, ok := s.items[key]; ok {
		e := el.Value.(*entry[T])
		e.value = value
		e.expiresAt = now.Add(s.ttl)
		s.order.MoveToFront(el)
		return
	}
	e := &entry[T]{key: key, value: value, expiresAt: now.Add(s.ttl)}
	el := s.order.PushFront(e)
	s.items[key] = el
	if s.order.Len() > s.maxEntries {
		if oldest := s.order.Back(); oldest != nil {
			s.removeElementLocked(oldest)
		}
	}
}

// GetOrLoad returns the cached value for key if present and unexpired;
// otherwise it calls loader and returns its result, caching it only if
// no Clear/Delete happened anywhere in the store while loader was
// running. Without that check, a Clear (e.g. from a concurrent broadcast
// eviction — see broadcast.go) racing an in-flight load could otherwise
// be immediately undone: the load, having started before the Clear and
// so still reflecting pre-invalidation state, would write itself back
// into the store right after the Clear completed, silently defeating the
// eviction for every reader until the next TTL expiry. loader's return
// value is still returned to THIS caller either way — its own read
// genuinely started before the concurrent invalidation, so there's no
// way to make it observe the new state; the fix only prevents that stale
// read from being handed to every OTHER reader too via the cache.
//
// A loader error is returned as-is and never cached. Concurrent misses
// for the same key are not deduplicated (no singleflight) — acceptable
// for this package's low-frequency, admin/nav-scale call sites; a
// hot-path consumer needing that guarantee should not use this package
// (see the package doc comment's allowlist reasoning).
func (s *Store[T]) GetOrLoad(key string, loader func() (T, error)) (T, error) {
	if v, ok := s.Get(key); ok {
		return v, nil
	}
	s.mu.Lock()
	gen := s.generation
	s.mu.Unlock()

	v, err := loader()
	if err != nil {
		var zero T
		return zero, err
	}

	s.mu.Lock()
	if s.generation == gen {
		s.setLocked(key, v)
	}
	s.mu.Unlock()
	return v, nil
}

// Delete removes key, if present. A missing key is not an error, but the
// generation still advances either way — a GetOrLoad racing this call for
// the same key must be invalidated regardless of whether key happened to
// be present yet, or it can write back the exact value this Delete meant
// to prevent from ever being cached.
func (s *Store[T]) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	el, ok := s.items[key]
	if !ok {
		s.generation++
		return
	}
	s.removeElementLocked(el)
	s.generation++
}

// Clear removes every entry. Used as the broadcast-eviction response
// (see broadcast.go) since a Store holds few, coarse-grained entries at
// the allowlisted call sites this package targets — indexing entries by
// which upstream tag/prefix invalidated them would add real complexity
// for no benefit at that scale.
func (s *Store[T]) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make(map[string]*list.Element)
	s.order.Init()
	s.generation++
}

// Len returns the current number of entries, including any not yet
// lazily expired by a Get. Intended for tests/introspection.
func (s *Store[T]) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.order.Len()
}

// removeElementLocked removes el from both the LRU list and the index.
// Callers must hold s.mu.
func (s *Store[T]) removeElementLocked(el *list.Element) {
	e := el.Value.(*entry[T])
	delete(s.items, e.key)
	s.order.Remove(el)
}
