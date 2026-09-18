package localcache_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/infrastructure/localcache"
)

func TestStore_SetAndGet(t *testing.T) {
	s := localcache.New[string](10, time.Minute)
	s.Set("k1", "v1")

	v, ok := s.Get("k1")
	if !ok || v != "v1" {
		t.Fatalf("Get = (%q, %v), want (v1, true)", v, ok)
	}
}

func TestStore_MissingKey(t *testing.T) {
	s := localcache.New[string](10, time.Minute)
	v, ok := s.Get("missing")
	if ok || v != "" {
		t.Fatalf("Get = (%q, %v), want (\"\", false)", v, ok)
	}
}

func TestStore_Overwrite(t *testing.T) {
	s := localcache.New[string](10, time.Minute)
	s.Set("k1", "first")
	s.Set("k1", "second")

	v, ok := s.Get("k1")
	if !ok || v != "second" {
		t.Fatalf("Get = (%q, %v), want (second, true)", v, ok)
	}
	if s.Len() != 1 {
		t.Fatalf("Len = %d, want 1 (overwrite must not duplicate)", s.Len())
	}
}

func TestStore_TTLExpiry(t *testing.T) {
	s := localcache.New[string](10, 20*time.Millisecond)
	s.Set("k1", "v1")

	if v, ok := s.Get("k1"); !ok || v != "v1" {
		t.Fatalf("Get before expiry = (%q, %v), want (v1, true)", v, ok)
	}

	time.Sleep(40 * time.Millisecond)

	if v, ok := s.Get("k1"); ok {
		t.Fatalf("Get after expiry = (%q, %v), want miss", v, ok)
	}
	if s.Len() != 0 {
		t.Fatalf("Len after expired Get = %d, want 0 (expired entry must be evicted on access)", s.Len())
	}
}

func TestStore_SetResetsTTL(t *testing.T) {
	s := localcache.New[string](10, 30*time.Millisecond)
	s.Set("k1", "v1")
	time.Sleep(20 * time.Millisecond)
	s.Set("k1", "v2") // resets the deadline
	time.Sleep(20 * time.Millisecond)

	// 40ms since the first Set (would have expired a 30ms TTL from then),
	// but only 20ms since the second Set — must still be live.
	v, ok := s.Get("k1")
	if !ok || v != "v2" {
		t.Fatalf("Get = (%q, %v), want (v2, true) — Set must reset the TTL deadline", v, ok)
	}
}

func TestStore_LRUEvictionAtCapacity(t *testing.T) {
	s := localcache.New[int](3, time.Minute)
	s.Set("a", 1)
	s.Set("b", 2)
	s.Set("c", 3)

	// Touch "a" so it's most-recently-used; "b" becomes the LRU victim.
	if _, ok := s.Get("a"); !ok {
		t.Fatal("expected hit for a")
	}
	s.Set("d", 4) // pushes past capacity 3, evicts least-recently-used

	if _, ok := s.Get("b"); ok {
		t.Error("b should have been evicted as least-recently-used")
	}
	for _, k := range []string{"a", "c", "d"} {
		if _, ok := s.Get(k); !ok {
			t.Errorf("%s should still be present", k)
		}
	}
	if s.Len() != 3 {
		t.Fatalf("Len = %d, want 3 (bounded at capacity)", s.Len())
	}
}

func TestStore_Delete(t *testing.T) {
	s := localcache.New[string](10, time.Minute)
	s.Set("k1", "v1")
	s.Delete("k1")

	if _, ok := s.Get("k1"); ok {
		t.Fatal("expected miss after Delete")
	}
}

func TestStore_DeleteMissingIsNoop(t *testing.T) {
	s := localcache.New[string](10, time.Minute)
	s.Delete("nope") // must not panic
}

func TestStore_Clear(t *testing.T) {
	s := localcache.New[string](10, time.Minute)
	s.Set("k1", "v1")
	s.Set("k2", "v2")
	s.Clear()

	if s.Len() != 0 {
		t.Fatalf("Len after Clear = %d, want 0", s.Len())
	}
	if _, ok := s.Get("k1"); ok {
		t.Fatal("expected miss after Clear")
	}
}

func TestStore_GetOrLoad_CachesSuccess(t *testing.T) {
	s := localcache.New[string](10, time.Minute)
	calls := 0
	loader := func() (string, error) {
		calls++
		return "loaded", nil
	}

	v, err := s.GetOrLoad("k1", loader)
	if err != nil || v != "loaded" {
		t.Fatalf("GetOrLoad = (%q, %v), want (loaded, nil)", v, err)
	}
	v, err = s.GetOrLoad("k1", loader)
	if err != nil || v != "loaded" {
		t.Fatalf("second GetOrLoad = (%q, %v), want (loaded, nil)", v, err)
	}
	if calls != 1 {
		t.Fatalf("loader called %d times, want 1 (second call must hit cache)", calls)
	}
}

func TestStore_GetOrLoad_DoesNotCacheError(t *testing.T) {
	s := localcache.New[string](10, time.Minute)
	wantErr := errors.New("load failed")
	calls := 0
	loader := func() (string, error) {
		calls++
		return "", wantErr
	}

	_, err := s.GetOrLoad("k1", loader)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	_, err = s.GetOrLoad("k1", loader)
	if !errors.Is(err, wantErr) {
		t.Fatalf("second err = %v, want %v", err, wantErr)
	}
	if calls != 2 {
		t.Fatalf("loader called %d times, want 2 (a failed load must not be cached)", calls)
	}
}

func TestStore_ConcurrentAccessIsRaceFree(t *testing.T) {
	s := localcache.New[int](50, 50*time.Millisecond)
	var wg sync.WaitGroup
	const workers = 32
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			key := "k"
			for j := 0; j < 200; j++ {
				s.Set(key, i)
				s.Get(key)
				s.Delete(key)
				_, _ = s.GetOrLoad(key, func() (int, error) { return i, nil })
				s.Len()
			}
			s.Clear()
		}()
	}
	wg.Wait()
}

// TestStore_GetOrLoad_DiscardsStaleWriteAfterConcurrentClear pins the
// code review fix: a Clear racing an in-flight GetOrLoad must not have
// its eviction silently undone when that load finishes and writes back.
// Deterministic (channel-synchronized, no sleep) so it can't pass by
// accident on a machine/CI run where the race window is too narrow to
// hit — the ordering is enforced explicitly: the load's own loader only
// returns AFTER Clear has already completed.
func TestStore_GetOrLoad_DiscardsStaleWriteAfterConcurrentClear(t *testing.T) {
	s := localcache.New[string](10, time.Minute)

	loaderStarted := make(chan struct{})
	clearDone := make(chan struct{})
	resultCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		v, err := s.GetOrLoad("k", func() (string, error) {
			close(loaderStarted)
			<-clearDone // block until the concurrent Clear below has finished
			return "stale", nil
		})
		resultCh <- v
		errCh <- err
	}()

	<-loaderStarted
	s.Clear() // invalidation races the in-flight load above
	close(clearDone)

	v := <-resultCh
	if err := <-errCh; err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	if v != "stale" {
		t.Fatalf("GetOrLoad returned %q, want %q — the caller's own in-flight load must still see its own result", v, "stale")
	}

	if cached, ok := s.Get("k"); ok {
		t.Fatalf("cache contains %q after a concurrent Clear raced the load that produced it — the stale write was not discarded", cached)
	}
}

// TestStore_GetOrLoad_DiscardsStaleWriteAfterConcurrentDelete mirrors the
// above for Delete instead of Clear.
func TestStore_GetOrLoad_DiscardsStaleWriteAfterConcurrentDelete(t *testing.T) {
	s := localcache.New[string](10, time.Minute)
	s.Set("k", "will-be-deleted") // Delete only bumps the generation when it actually removes something

	loaderStarted := make(chan struct{})
	deleteDone := make(chan struct{})
	resultCh := make(chan string, 1)
	errCh := make(chan error, 1)

	s.Delete("k") // start from a miss so GetOrLoad's loader actually runs
	go func() {
		v, err := s.GetOrLoad("k", func() (string, error) {
			close(loaderStarted)
			<-deleteDone
			return "stale", nil
		})
		resultCh <- v
		errCh <- err
	}()

	<-loaderStarted
	s.Set("k", "fresh") // a write for a DIFFERENT reason, then...
	s.Delete("k")       // ...an invalidation, racing the in-flight load above
	close(deleteDone)

	v := <-resultCh
	if err := <-errCh; err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	if v != "stale" {
		t.Fatalf("GetOrLoad returned %q, want %q", v, "stale")
	}

	if cached, ok := s.Get("k"); ok {
		t.Fatalf("cache contains %q after a concurrent Delete raced the load that produced it — the stale write was not discarded", cached)
	}
}

// TestStore_GetOrLoad_DiscardsStaleWriteAfterConcurrentDeleteOfAbsentKey pins
// the code review fix: Delete on a key that isn't present is a no-op for
// the map itself, but must still advance the generation. Without that, a
// Delete racing an in-flight GetOrLoad for a key that was never cached (or
// already evicted) would silently do nothing to stop that load from
// writing back the exact value the Delete meant to invalidate.
func TestStore_GetOrLoad_DiscardsStaleWriteAfterConcurrentDeleteOfAbsentKey(t *testing.T) {
	s := localcache.New[string](10, time.Minute)

	loaderStarted := make(chan struct{})
	deleteDone := make(chan struct{})
	resultCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		v, err := s.GetOrLoad("k", func() (string, error) {
			close(loaderStarted)
			<-deleteDone
			return "stale", nil
		})
		resultCh <- v
		errCh <- err
	}()

	<-loaderStarted
	s.Delete("k") // key was never present — must still bump generation
	close(deleteDone)

	v := <-resultCh
	if err := <-errCh; err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	if v != "stale" {
		t.Fatalf("GetOrLoad returned %q, want %q", v, "stale")
	}

	if cached, ok := s.Get("k"); ok {
		t.Fatalf("cache contains %q after a concurrent Delete of an absent key raced the load that produced it — the stale write was not discarded", cached)
	}
}

func TestNew_PanicsOnNonPositiveMaxEntries(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for maxEntries <= 0")
		}
	}()
	localcache.New[string](0, time.Minute)
}

func TestNew_PanicsOnNonPositiveTTL(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for ttl <= 0")
		}
	}()
	localcache.New[string](10, 0)
}
