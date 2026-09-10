package search

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/platform/event"
)

// productReindexDebounceWindow bounds how often the same product ID may
// re-trigger a reindex from event-driven changes. A burst of edits to one
// product (e.g. a bulk price import repeatedly touching one SKU) enqueues
// at most one reindex per window on the leading edge, plus — see
// scheduleTrailingFlush — exactly one more at the end of the window if
// any event arrived while debounced, so the product's actual latest state
// always gets reindexed within one window, never silently dropped.
//
// This debounce is per-process, not distributed: each server instance
// tracks its own last-fired times independently, so a multi-instance
// deployment can end up enqueueing a few more reindex jobs than a single
// instance would (each instance's own window firing once). Accepted —
// a handful of extra small scoped reindexes is not a correctness problem,
// and coordinating a shared debounce across instances is not worth the
// complexity for this.
const productReindexDebounceWindow = 5 * time.Second

// categoryIndexMaxAttempts bounds how many times a category-document
// engine call (IndexCategory/RemoveCategory) is retried before giving up.
// Unlike product reindexing — a queued, durable job via ReindexService
// with its own retry/backoff — category-document indexing runs as a bare
// async event handler (see HandleCategoryCreated/Updated/Deleted's own
// doc comments for why there's no existing job-queue path to reuse for
// it), and per event.Bus.Publish, an async handler's error is only logged,
// never retried by the bus itself. A short bounded in-process retry
// absorbs a transient failure (a brief Meilisearch blip, a momentary
// connection error) without needing job-queue infrastructure; a truly
// persistent failure still ultimately drops the update, same as before —
// recoverable only by the category's next edit or a manual/full reindex
// (see ROADMAP.md's "Design notes: PR-1037" and this PR's own CR
// retrospective).
const categoryIndexMaxAttempts = 3

// categoryIndexRetryBaseDelay is the delay before the first retry,
// doubled after each subsequent failed attempt.
const categoryIndexRetryBaseDelay = 200 * time.Millisecond

// IndexUpdateSubscriber listens for catalog/pricing/inventory change events
// and enqueues a scoped reindex for the affected product — the "on save"
// mode neither PR-1030's schedule trigger nor PR-1035's manual admin
// trigger covers. Mirrors application/cache.InvalidationSubscriber's shape
// (same package-level pattern, same event sources for the catalog/pricing
// events the two subscribers share).
type IndexUpdateSubscriber struct {
	reindex    *ReindexService
	engine     domainsearch.SearchEngine
	categories domainsearch.CategorySource
	log        Logger
	window     time.Duration // productReindexDebounceWindow; a field (not just the const) so a whitebox test can shrink it for a fast, deterministic trailing-flush test.
	retryDelay time.Duration // categoryIndexRetryBaseDelay; a field for the same reason — a whitebox test shrinks it for a fast retry test.

	mu       sync.Mutex
	lastFire map[string]time.Time
	trailing map[string]*time.Timer

	// categoryLocks holds a *sync.Mutex per category ID (map[string]*sync.Mutex),
	// serializing indexCategory and HandleCategoryDeleted for the same
	// category — see lockCategory's own doc comment for the race this
	// prevents. Lazily populated via LoadOrStore; entries are never
	// removed, but the key space is bounded by the number of distinct
	// categories ever touched (a small catalog dimension, unlike
	// products), so this doesn't grow unboundedly in practice.
	categoryLocks sync.Map
}

// NewIndexUpdateSubscriber creates an IndexUpdateSubscriber. engine and
// categories back the category-document handlers added in PR-1037
// (HandleCategoryCreated/Updated/Deleted) — a category *document* is
// indexed directly via SearchEngine.IndexCategory/RemoveCategory, not
// through ReindexService/the job queue: ScopeCategories only ever resolves
// to the category's member *products*, never to the category document
// itself (see ReindexService's own scope-resolution logic), so there is no
// existing path this could reuse.
func NewIndexUpdateSubscriber(reindex *ReindexService, engine domainsearch.SearchEngine, categories domainsearch.CategorySource, log Logger) *IndexUpdateSubscriber {
	if reindex == nil {
		panic("search.NewIndexUpdateSubscriber: nil reindex service")
	}
	if engine == nil {
		panic("search.NewIndexUpdateSubscriber: nil search engine")
	}
	if categories == nil {
		panic("search.NewIndexUpdateSubscriber: nil category source")
	}
	if log == nil {
		panic("search.NewIndexUpdateSubscriber: nil logger")
	}
	return &IndexUpdateSubscriber{
		reindex:    reindex,
		engine:     engine,
		categories: categories,
		log:        log,
		window:     productReindexDebounceWindow,
		retryDelay: categoryIndexRetryBaseDelay,
		lastFire:   make(map[string]time.Time),
		trailing:   make(map[string]*time.Timer),
	}
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
	bus.OnAsync(catalog.EventCategoryCreated, s.HandleCategoryCreated)
	bus.OnAsync(catalog.EventCategoryUpdated, s.HandleCategoryUpdated)
	bus.OnAsync(catalog.EventCategoryDeleted, s.HandleCategoryDeleted)
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

// HandleCategoryCreated indexes a newly created category document.
func (s *IndexUpdateSubscriber) HandleCategoryCreated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(catalog.CategoryCreatedData)
	if !ok {
		return fmt.Errorf("search.invalidation: unexpected event data type %T", evt.Data)
	}
	return s.indexCategory(ctx, data.CategoryID)
}

// HandleCategoryUpdated re-indexes an updated category document.
func (s *IndexUpdateSubscriber) HandleCategoryUpdated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(catalog.CategoryUpdatedData)
	if !ok {
		return fmt.Errorf("search.invalidation: unexpected event data type %T", evt.Data)
	}
	return s.indexCategory(ctx, data.CategoryID)
}

// HandleCategoryDeleted removes a deleted category's document from the
// index. Unlike the create/update path, this needs no fresh read: there
// is nothing left to fetch, so it calls RemoveCategory directly.
func (s *IndexUpdateSubscriber) HandleCategoryDeleted(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(catalog.CategoryDeletedData)
	if !ok {
		return fmt.Errorf("search.invalidation: unexpected event data type %T", evt.Data)
	}
	unlock := s.lockCategory(data.CategoryID)
	defer unlock()
	if err := s.retryCategoryEngineCall(ctx, func() error { return s.engine.RemoveCategory(ctx, data.CategoryID) }); err != nil {
		s.log.Error("search.invalidation.category_remove_failed", err, map[string]interface{}{"category_id": data.CategoryID})
		return fmt.Errorf("search.invalidation: remove category %s: %w", data.CategoryID, err)
	}
	return nil
}

// indexCategory re-reads categoryID's current state (not a snapshot from
// event time, so a burst of quick successive edits still ends up with the
// latest state indexed) and pushes it to the search engine. No debounce:
// unlike product edits, category writes go through a single synchronous
// admin request each, not a bulk-import path — see PR-1036's debounce,
// which exists specifically for that bulk case.
//
// Holds categoryID's lock (see lockCategory) across both the GetByID read
// and the engine call: an Update and a Delete for the same category are
// published from two independent HTTP requests, each dispatched to its
// own async goroutine with no ordering guarantee between them. Without
// serializing here, an Update handler whose GetByID ran before a
// concurrent Delete actually removed the row could still push that
// now-stale category to IndexCategory *after* the Delete handler's
// RemoveCategory already ran — resurrecting a deleted category as
// searchable, permanently (nothing else will ever remove it again). With
// the lock, whichever handler runs second does its GetByID read (still
// live, still inside the lock) after the first has fully finished: if
// Delete ran first, the second GetByID correctly observes found=false and
// no-ops; if Update ran first, Delete's subsequent RemoveCategory still
// correctly clears whatever Update just indexed.
func (s *IndexUpdateSubscriber) indexCategory(ctx context.Context, categoryID string) error {
	unlock := s.lockCategory(categoryID)
	defer unlock()

	c, found, err := s.categories.GetByID(ctx, categoryID)
	if err != nil {
		return fmt.Errorf("search.invalidation: get category %s: %w", categoryID, err)
	}
	if !found {
		// Deleted between the event firing and this handler running;
		// nothing to index. Not an error — see CategorySource.GetByID's
		// own doc comment.
		return nil
	}
	if err := s.retryCategoryEngineCall(ctx, func() error { return s.engine.IndexCategory(ctx, c) }); err != nil {
		s.log.Error("search.invalidation.category_index_failed", err, map[string]interface{}{"category_id": categoryID})
		return fmt.Errorf("search.invalidation: index category %s: %w", categoryID, err)
	}
	s.log.Info("search.invalidation.category_indexed", map[string]interface{}{"category_id": categoryID})
	return nil
}

// lockCategory locks categoryID's per-category mutex (creating it on first
// use) and returns the matching Unlock func. Callers must defer the
// returned func. See indexCategory's own doc comment for the race this
// serialization prevents between an Update and a Delete for the same
// category racing across two independent async event handlers.
func (s *IndexUpdateSubscriber) lockCategory(categoryID string) func() {
	muIface, _ := s.categoryLocks.LoadOrStore(categoryID, &sync.Mutex{})
	mu := muIface.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// retryCategoryEngineCall runs fn up to categoryIndexMaxAttempts times,
// waiting s.retryDelay (doubling after each failed attempt) in between,
// and returns the last error if every attempt fails. A wait aborts early
// (returning the error from the most recent attempt) if ctx is cancelled
// — the async handler's ctx is the bus's shutdownCtx, live during Drain's
// grace window and then cancelled, so a stuck retry loop doesn't hang
// process shutdown indefinitely.
func (s *IndexUpdateSubscriber) retryCategoryEngineCall(ctx context.Context, fn func() error) error {
	var err error
	delay := s.retryDelay
	for attempt := 1; attempt <= categoryIndexMaxAttempts; attempt++ {
		if err = fn(); err == nil {
			return nil
		}
		if attempt == categoryIndexMaxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(delay):
		}
		delay *= 2
	}
	return err
}

// scheduleReindex enqueues a scoped reindex for productID, unless a
// reindex for the same product was already triggered within
// s.window — in which case it instead ensures a trailing reindex fires
// once that window ends (scheduleTrailingFlush), so the product's actual
// latest state is never silently dropped just because the leading-edge
// job for it happened to finish before the window did (a real risk: a
// single-product reindex is fast, easily faster than the multi-second
// debounce window meant to coalesce a burst of edits to it).
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
		s.scheduleTrailingFlush(ctx, productID)
		return nil
	}
	if err := s.trigger(ctx, productID); err != nil {
		return fmt.Errorf("search.invalidation: trigger reindex for product %s: %w", productID, err)
	}
	return nil
}

// trigger calls ReindexService.Trigger for productID, evicting its
// debounce reservation on failure (see scheduleReindex's own doc comment
// for why) and logging either outcome. Shared by the immediate
// (leading-edge) path and the trailing flush below.
func (s *IndexUpdateSubscriber) trigger(ctx context.Context, productID string) error {
	if _, err := s.reindex.Trigger(ctx, ScopeProducts{IDs: []string{productID}}); err != nil {
		s.evict(productID)
		s.log.Error("search.invalidation.trigger_failed", err, map[string]interface{}{"product_id": productID})
		return err
	}
	s.log.Info("search.invalidation.triggered", map[string]interface{}{"product_id": productID})
	return nil
}

// scheduleTrailingFlush guarantees one more Trigger call for productID
// once the current debounce window elapses, unless one is already
// scheduled. Without this, an event debounced away is only ever
// reflected if some *later* event happens to arrive and itself lands
// outside the window — if nothing else touches this product again, the
// index would silently keep showing whatever state the leading-edge job
// indexed, forever (until an unrelated future edit or the next scheduled
// full reindex), even though a real edit was debounced in the meantime.
// The trailing flush removes that dependency on a next event ever
// arriving: it fires unconditionally at window-end and picks up
// whatever the product's state is *then* (ReindexHandler reads live
// data, not a snapshot from event time), so the latest state is always
// caught within one window of the last edit, not just the first.
func (s *IndexUpdateSubscriber) scheduleTrailingFlush(ctx context.Context, productID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, pending := s.trailing[productID]; pending {
		return
	}
	delay := s.window
	if last, ok := s.lastFire[productID]; ok {
		if remaining := s.window - time.Since(last); remaining > 0 {
			delay = remaining
		} else {
			delay = 0
		}
	}
	s.trailing[productID] = time.AfterFunc(delay, func() { s.flushTrailing(ctx, productID) })
}

// flushTrailing is the trailing timer's callback: it resets the debounce
// window from now (so a further burst right after this flush is itself
// coalesced, not fired immediately) and performs the guaranteed reindex.
func (s *IndexUpdateSubscriber) flushTrailing(ctx context.Context, productID string) {
	s.mu.Lock()
	delete(s.trailing, productID)
	s.lastFire[productID] = time.Now()
	s.mu.Unlock()

	if err := s.trigger(ctx, productID); err != nil {
		s.log.Error("search.invalidation.trailing_trigger_failed", err, map[string]interface{}{"product_id": productID})
	}
}

// allow reports whether productID may fire now — false if it already
// fired within the last s.window. Opportunistically prunes every entry
// older than the window on each call, rather than running a background
// sweep goroutine with its own shutdown/leak concerns, so lastFire only
// ever holds recently-active product IDs instead of growing unboundedly
// over the process's lifetime. On a restart, lastFire starts empty — at
// most one debounce window's worth of coalescing is lost, not any data
// (a trailing flush is scheduled again on the next event, same as
// before a restart; see scheduleTrailingFlush's own doc comment).
func (s *IndexUpdateSubscriber) allow(productID string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for pid, last := range s.lastFire {
		if now.Sub(last) > s.window {
			delete(s.lastFire, pid)
		}
	}
	if last, ok := s.lastFire[productID]; ok && now.Sub(last) < s.window {
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
