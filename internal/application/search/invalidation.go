package search

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/platform/event"
)

// productReindexDebounceWindow bounds how often the same product ID may
// re-trigger a reindex from event-driven changes. A burst of edits to one
// product (e.g. a bulk price import repeatedly touching one SKU) enqueues
// at most one reindex per window, not one per event — the next save after
// a debounced-away event still triggers reindexing, so at most one
// window's worth of freshness is ever delayed, never lost.
//
// This debounce is per-process, not distributed: each server instance
// tracks its own last-fired times independently, so a multi-instance
// deployment can end up enqueueing a few more reindex jobs than a single
// instance would (each instance's own window firing once). Accepted —
// a handful of extra small scoped reindexes is not a correctness problem,
// and coordinating a shared debounce across instances is not worth the
// complexity for this.
const productReindexDebounceWindow = 5 * time.Second

// IndexUpdateSubscriber listens for catalog/pricing/inventory change events
// and enqueues a scoped reindex for the affected product — the "on save"
// mode neither PR-1030's schedule trigger nor PR-1035's manual admin
// trigger covers. Mirrors application/cache.InvalidationSubscriber's shape
// (same package-level pattern, same event sources for the catalog/pricing
// events the two subscribers share).
type IndexUpdateSubscriber struct {
	reindex *ReindexService
	log     Logger

	mu       sync.Mutex
	lastFire map[string]time.Time
}

// NewIndexUpdateSubscriber creates an IndexUpdateSubscriber.
func NewIndexUpdateSubscriber(reindex *ReindexService, log Logger) *IndexUpdateSubscriber {
	if reindex == nil {
		panic("search.NewIndexUpdateSubscriber: nil reindex service")
	}
	if log == nil {
		panic("search.NewIndexUpdateSubscriber: nil logger")
	}
	return &IndexUpdateSubscriber{reindex: reindex, log: log, lastFire: make(map[string]time.Time)}
}

// Register wires event handlers on the given bus.
//
// catalog.EventProductCreated is included even though PR-1036's own scope
// only names "product/price/stock/category-assignment change events" —
// wire_services.go used to index a newly created product synchronously
// inline (a mechanism this subscriber replaces entirely, see PR-1036.md's
// "Design decisions"); dropping EventProductCreated here would silently
// stop indexing new products until the next scheduled full reindex.
func (s *IndexUpdateSubscriber) Register(bus *event.Bus) {
	if bus == nil {
		panic("IndexUpdateSubscriber.Register: bus is nil")
	}
	bus.OnAsync(catalog.EventProductCreated, s.HandleProductCreated)
	bus.OnAsync(catalog.EventProductUpdated, s.HandleProductUpdated)
	bus.OnAsync(pricing.EventPriceUpserted, s.HandlePriceUpserted)
	bus.OnAsync(inventory.EventStockUpdated, s.HandleStockUpdated)
}

// HandleProductCreated schedules a reindex for a newly created product.
func (s *IndexUpdateSubscriber) HandleProductCreated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(catalog.ProductCreatedData)
	if !ok {
		return fmt.Errorf("search.invalidation: unexpected event data type %T", evt.Data)
	}
	return s.scheduleReindex(ctx, data.ProductID)
}

// HandleProductUpdated schedules a reindex for an updated product.
func (s *IndexUpdateSubscriber) HandleProductUpdated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(catalog.ProductUpdatedData)
	if !ok {
		return fmt.Errorf("search.invalidation: unexpected event data type %T", evt.Data)
	}
	return s.scheduleReindex(ctx, data.ProductID)
}

// HandlePriceUpserted schedules a reindex for the product whose price
// changed. Also fired for a category-assignment change (see
// PR-1036.md's "Design decisions" for why that reuses
// catalog.EventProductUpdated rather than a new event) — handled by
// HandleProductUpdated above, not here.
func (s *IndexUpdateSubscriber) HandlePriceUpserted(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(pricing.PriceUpsertedData)
	if !ok {
		return fmt.Errorf("search.invalidation: unexpected event data type %T", evt.Data)
	}
	return s.scheduleReindex(ctx, data.ProductID)
}

// HandleStockUpdated schedules a reindex for the product whose stock changed.
func (s *IndexUpdateSubscriber) HandleStockUpdated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(inventory.StockUpdatedData)
	if !ok {
		return fmt.Errorf("search.invalidation: unexpected event data type %T", evt.Data)
	}
	return s.scheduleReindex(ctx, data.ProductID)
}

// scheduleReindex enqueues a scoped reindex for productID, unless a
// reindex for the same product was already triggered within
// productReindexDebounceWindow.
//
// allow reserves the debounce slot before Trigger runs (so two events for
// the same product arriving concurrently can't both slip past it), but if
// Trigger then fails, that reservation is released again (evict, not
// left standing) — otherwise a transient failure (queue full, a DB blip)
// would silently swallow every subsequent same-product event for the rest
// of the window even though no reindex ever actually ran, leaving the
// index stale with no recovery path except an unrelated future edit or
// the next scheduled full reindex.
func (s *IndexUpdateSubscriber) scheduleReindex(ctx context.Context, productID string) error {
	if productID == "" {
		return nil
	}
	if !s.allow(productID, time.Now()) {
		return nil
	}
	if _, err := s.reindex.Trigger(ctx, ScopeProducts{IDs: []string{productID}}); err != nil {
		s.evict(productID)
		s.log.Error("search.invalidation.trigger_failed", err, map[string]interface{}{"product_id": productID})
		return fmt.Errorf("search.invalidation: trigger reindex for product %s: %w", productID, err)
	}
	s.log.Info("search.invalidation.triggered", map[string]interface{}{"product_id": productID})
	return nil
}

// allow reports whether productID may fire now — false if it already
// fired within the last productReindexDebounceWindow. Opportunistically
// prunes every entry older than the window on each call, rather than
// running a background sweep goroutine with its own shutdown/leak
// concerns, so lastFire only ever holds recently-active product IDs
// instead of growing unboundedly over the process's lifetime. On a
// restart, lastFire starts empty — at most one debounce window's worth of
// coalescing is lost, not any data (the next save still triggers a
// reindex; see productReindexDebounceWindow's own doc comment).
func (s *IndexUpdateSubscriber) allow(productID string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for pid, last := range s.lastFire {
		if now.Sub(last) > productReindexDebounceWindow {
			delete(s.lastFire, pid)
		}
	}
	if last, ok := s.lastFire[productID]; ok && now.Sub(last) < productReindexDebounceWindow {
		return false
	}
	s.lastFire[productID] = now
	return true
}

// evict releases productID's debounce reservation — used when Trigger
// fails after allow already reserved the slot, so the failed attempt
// doesn't count against the window and the next event for this product
// (whether a retry-worthy follow-up save, or the same one redelivered)
// isn't falsely debounced away.
func (s *IndexUpdateSubscriber) evict(productID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.lastFire, productID)
}
