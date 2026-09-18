package localcache_test

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/infrastructure/localcache"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// TestInvalidateOnEvent_EvictsSynchronously pins the direct-domain-event
// wiring path (e.g. catalog.EventCategoryUpdated) used to evict a Store
// when its underlying data changes. bus.On is synchronous, so the
// eviction is guaranteed to have already happened by the time Publish
// returns.
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

// TestInvalidateOnEvent_MatchPredicateFiltersEvents pins that a match
// predicate can reject events sharing the same name for an unrelated
// reason (e.g. category_product_assignment_admin.go republishing
// catalog.EventCategoryUpdated on every product<->category assignment
// change, not just a real category edit) — see NotSourcedBy.
func TestInvalidateOnEvent_MatchPredicateFiltersEvents(t *testing.T) {
	bus := event.NewBus(logger.New("error"))
	store := localcache.New[string](10, time.Hour)
	localcache.InvalidateOnEvent(bus, store, "catalog.category.updated", localcache.NotSourcedBy("category.assignment"))
	store.Set("k", "v")

	if err := bus.Publish(context.Background(), event.New("catalog.category.updated", "category.assignment", nil)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, ok := store.Get("k"); !ok {
		t.Error("store should NOT have been evicted by an event from a filtered-out source")
	}

	if err := bus.Publish(context.Background(), event.New("catalog.category.updated", "test", nil)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, ok := store.Get("k"); ok {
		t.Error("store should have been evicted by an event from a non-filtered source")
	}
}
