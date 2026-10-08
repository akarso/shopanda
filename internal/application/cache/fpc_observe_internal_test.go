package cache

import (
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/metrics"
)

func TestFPCObserver_CompareAndDeleteKeepsRefill(t *testing.T) {
	obs := NewFPCObserver(metrics.Noop())
	key := "fpc:v1:k"
	old := time.Now().UTC().Add(-time.Second)
	obs.pages.Store(key, old)

	// Concurrent refill after Range would have read `old`.
	fresh := time.Now().UTC().Add(time.Hour)
	obs.pages.Store(key, fresh)

	if obs.pages.CompareAndDelete(key, old) {
		t.Fatal("CompareAndDelete must not drop a refreshed expiry")
	}
	if got := obs.livePages(time.Now().UTC()); got != 1 {
		t.Fatalf("pages_stored = %d, want 1 after failed prune of refilled key", got)
	}
}

func TestFPCObserver_MaybePruneWithoutSnapshot(t *testing.T) {
	obs := NewFPCObserver(metrics.Noop())
	obs.pages.Store("gone", time.Now().UTC().Add(-time.Second))
	obs.pages.Store("live", time.Now().UTC().Add(time.Hour))
	// Force prune path (interval already elapsed).
	obs.lastPrune.Store(0)
	obs.PageStored("live", time.Now().UTC().Add(time.Hour))

	if _, ok := obs.pages.Load("gone"); ok {
		t.Fatal("expired key must be pruned on PageStored without Snapshot")
	}
	if _, ok := obs.pages.Load("live"); !ok {
		t.Fatal("live key must remain")
	}
}

func TestFPCObserver_MaybePruneConcurrentRefill(t *testing.T) {
	obs := NewFPCObserver(metrics.Noop())
	key := "race"
	obs.pages.Store(key, time.Now().UTC().Add(-time.Millisecond))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		obs.lastPrune.Store(0)
		obs.maybePruneExpired()
	}()
	go func() {
		defer wg.Done()
		obs.PageStored(key, time.Now().UTC().Add(time.Hour))
	}()
	wg.Wait()

	// Either prune ran first (then refill restored) or refill won CompareAndDelete.
	if got := obs.Snapshot().PagesStored; got != 1 {
		t.Fatalf("pages_stored = %d, want 1", got)
	}
}
