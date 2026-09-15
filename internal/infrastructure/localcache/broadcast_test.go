package localcache_test

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/infrastructure/localcache"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// TestInvalidateOnTag_EvictsAcrossSimulatedInstances is PR-1040's own
// planned validation: two independent Store instances (standing in for
// two separate server instances/replicas, each with their own L1 cache)
// both subscribe to the SAME bus. This is the real-world limit of what
// can be demonstrated here: internal/platform/event.Bus has no
// cross-process delivery (see cache.EventInvalidated's own doc comment)
// — a real second replica runs its own independent bus, unreachable from
// this one. A single shared bus is the closest honest stand-in for
// "another subscriber reacting to the same invalidation," which is
// exactly what this test proves: publishing cache.EventInvalidated once
// evicts every registered Store, not just whichever one's own
// DeleteByTag caused it.
func TestInvalidateOnTag_EvictsAcrossSimulatedInstances(t *testing.T) {
	bus := event.NewBus(logger.New("error"))

	instanceA := localcache.New[string](10, time.Hour)
	instanceB := localcache.New[string](10, time.Hour)
	localcache.InvalidateOnTag(bus, instanceA, "permissions")
	localcache.InvalidateOnTag(bus, instanceB, "permissions")

	instanceA.Set("catalog", "stale-a")
	instanceB.Set("catalog", "stale-b")

	if err := bus.Publish(context.Background(), event.New(cache.EventInvalidated, "test", cache.InvalidatedData{Tag: "permissions"})); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	waitUntil(t, func() bool {
		_, aHit := instanceA.Get("catalog")
		_, bHit := instanceB.Get("catalog")
		return !aHit && !bHit
	}, "both instances evicted by the shared invalidation broadcast")
}

// TestInvalidateOnTag_IgnoresOtherTags pins that InvalidateOnTag only
// reacts to its own tag — a Store registered for "permissions" must not
// be cleared by an invalidation for an unrelated tag.
func TestInvalidateOnTag_IgnoresOtherTags(t *testing.T) {
	bus := event.NewBus(logger.New("error"))
	store := localcache.New[string](10, time.Hour)
	localcache.InvalidateOnTag(bus, store, "permissions")
	store.Set("k", "v")

	if err := bus.Publish(context.Background(), event.New(cache.EventInvalidated, "test", cache.InvalidatedData{Tag: "unrelated"})); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Give any (incorrect) async eviction a chance to happen before
	// asserting it didn't.
	time.Sleep(20 * time.Millisecond)
	if _, ok := store.Get("k"); !ok {
		t.Error("store should NOT have been evicted by an invalidation for a different tag")
	}
}

// TestInvalidateOnPrefix_Evicts pins the DeleteByPrefix-triggered path.
func TestInvalidateOnPrefix_Evicts(t *testing.T) {
	bus := event.NewBus(logger.New("error"))
	store := localcache.New[string](10, time.Hour)
	localcache.InvalidateOnPrefix(bus, store, "product:123:")
	store.Set("k", "v")

	if err := bus.Publish(context.Background(), event.New(cache.EventInvalidated, "test", cache.InvalidatedData{Prefix: "product:123:"})); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	waitUntil(t, func() bool {
		_, ok := store.Get("k")
		return !ok
	}, "store evicted by the prefix invalidation")
}

// TestInvalidateOnPrefix_IgnoresOtherPrefixes mirrors
// TestInvalidateOnTag_IgnoresOtherTags for the prefix path.
func TestInvalidateOnPrefix_IgnoresOtherPrefixes(t *testing.T) {
	bus := event.NewBus(logger.New("error"))
	store := localcache.New[string](10, time.Hour)
	localcache.InvalidateOnPrefix(bus, store, "product:123:")
	store.Set("k", "v")

	if err := bus.Publish(context.Background(), event.New(cache.EventInvalidated, "test", cache.InvalidatedData{Prefix: "product:456:"})); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	if _, ok := store.Get("k"); !ok {
		t.Error("store should NOT have been evicted by an invalidation for a different prefix")
	}
}

// TestInvalidateOnEvent_EvictsSynchronously pins the direct-domain-event
// wiring path (e.g. catalog.EventCategoryUpdated) used when the
// underlying data doesn't go through cache.Cache's own DeleteByTag/
// DeleteByPrefix at all. bus.On is synchronous, so the eviction is
// guaranteed to have already happened by the time Publish returns.
func TestInvalidateOnEvent_EvictsSynchronously(t *testing.T) {
	bus := event.NewBus(logger.New("error"))
	store := localcache.New[string](10, time.Hour)
	localcache.InvalidateOnEvent(bus, store, "catalog.category.updated")
	store.Set("k", "v")

	if err := bus.Publish(context.Background(), event.New("catalog.category.updated", "test", nil)); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if _, ok := store.Get("k"); ok {
		t.Error("store should have been evicted synchronously by Publish")
	}
}

// TestInvalidateOnTag_PublishAsyncAlsoWorks confirms the payload shape
// both cache.Cache backends actually publish (via PublishAsync, not
// Publish — see each backend's own SetBus doc comment) is exactly what
// InvalidateOnTag expects; each backend's own tests exercise its real
// DeleteByTag producing this event, this only pins the consuming side.
func TestInvalidateOnTag_PublishAsyncAlsoWorks(t *testing.T) {
	bus := event.NewBus(logger.New("error"))
	store := localcache.New[string](10, time.Hour)
	localcache.InvalidateOnTag(bus, store, "cms:7")
	store.Set("page:a", "html")

	bus.PublishAsync(event.New(cache.EventInvalidated, "cache_store.postgres", cache.InvalidatedData{Tag: "cms:7"}))

	waitUntil(t, func() bool {
		_, ok := store.Get("page:a")
		return !ok
	}, "store evicted via PublishAsync")
}

// waitUntil polls cond for up to one second, failing the test with msg if
// it never becomes true. Bus async handlers run in their own goroutines
// (see Bus.Publish/PublishAsync's own doc comments) so a test observing
// their side effect must poll, not assume the effect landed as soon as
// Publish/PublishAsync returns.
func waitUntil(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if !cond() {
		t.Fatalf("timed out waiting for: %s", msg)
	}
}
