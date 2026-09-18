package localcache

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/platform/event"
)

// Evictor is the narrow capability InvalidateOnTag/InvalidateOnPrefix
// need. *Store[T], for any T, satisfies it.
type Evictor interface {
	Clear()
}

// InvalidateOnTag registers an async handler on bus so that store.Clear()
// runs whenever cache.EventInvalidated fires for exactly tag — wiring a
// Store up to L2's own DeleteByTag calls (see cache.EventInvalidated's
// own doc comment for why this only ever reaches subscribers within the
// SAME process, not other server replicas; store's own TTL remains the
// actual cross-replica staleness bound, this only shortens same-process
// staleness).
func InvalidateOnTag(bus *event.Bus, store Evictor, tag string) {
	bus.OnAsync(cache.EventInvalidated, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(cache.InvalidatedData)
		if !ok || data.Tag != tag {
			return nil
		}
		store.Clear()
		return nil
	})
}

// InvalidateOnPrefix is InvalidateOnTag for a DeleteByPrefix-triggered
// cache.EventInvalidated instead of a DeleteByTag-triggered one.
func InvalidateOnPrefix(bus *event.Bus, store Evictor, prefix string) {
	bus.OnAsync(cache.EventInvalidated, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(cache.InvalidatedData)
		if !ok || data.Prefix != prefix {
			return nil
		}
		store.Clear()
		return nil
	})
}

// InvalidateOnEvent registers a synchronous handler on bus so that
// store.Clear() runs whenever eventName fires and, if any match
// predicates are given, every one of them returns true for the fired
// event — for wiring a Store up to an existing, already-specific domain
// event (e.g. catalog.EventCategoryCreated/Updated/Deleted) rather than
// the generic cache.EventInvalidated, when the underlying data doesn't
// actually go through a cache.Cache DeleteByTag/DeleteByPrefix call at
// all. Synchronous (bus.On, not OnAsync) so the local eviction is
// guaranteed to have happened by the time the request that triggered
// eventName finishes — this process's own next read can never observe
// its own stale write; other processes still rely solely on store's TTL.
//
// match matters because the SAME event name can be reused by more than
// one call site for unrelated reasons — e.g.
// category_product_assignment_admin.go republishes
// catalog.EventCategoryUpdated (source "category.assignment") on every
// single product↔category Assign/Unassign call, not on an actual
// category edit. Without a way to tell those apart, a Store meant to
// react only to real category changes would instead clear on every
// assignment change too, defeating the "nothing with a tag that changes
// more than a few times a minute" rule this package's own doc comment
// states as a design rule, not just an implementation detail.
func InvalidateOnEvent(bus *event.Bus, store Evictor, eventName string, match ...func(event.Event) bool) {
	bus.On(eventName, func(_ context.Context, evt event.Event) error {
		for _, m := range match {
			if !m(evt) {
				return nil
			}
		}
		store.Clear()
		return nil
	})
}

// NotSourcedBy returns a match predicate for InvalidateOnEvent that
// rejects events published with exactly this source string.
func NotSourcedBy(source string) func(event.Event) bool {
	return func(evt event.Event) bool { return evt.Source != source }
}
