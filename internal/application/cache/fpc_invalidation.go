package cache

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/platform/event"
)

// FPCInvalidationSubscriber deletes full-page cache entries by tag when
// catalog / price / stock / CMS entities change (PR-1046). Async on the
// bus so a product save is not blocked by purge latency.
//
// Tag names match what storefront writes via ProductTag / CategoryTag /
// PageTag. Content-block edits have no domain event today — operators
// use purge-by-URL (or clear by cms:/page: tag) until one exists.
type FPCInvalidationSubscriber struct {
	cache cache.Cache
	log   Logger
}

// NewFPCInvalidationSubscriber creates an FPCInvalidationSubscriber.
func NewFPCInvalidationSubscriber(c cache.Cache, log Logger) *FPCInvalidationSubscriber {
	if c == nil {
		panic("cache.NewFPCInvalidationSubscriber: nil cache")
	}
	if log == nil {
		panic("cache.NewFPCInvalidationSubscriber: nil logger")
	}
	return &FPCInvalidationSubscriber{cache: c, log: log}
}

// Register wires async handlers on the given bus.
func (s *FPCInvalidationSubscriber) Register(bus *event.Bus) {
	if bus == nil {
		panic("FPCInvalidationSubscriber.Register: bus is nil")
	}
	bus.OnAsync(catalog.EventProductCreated, s.HandleProductCreated)
	bus.OnAsync(catalog.EventProductUpdated, s.HandleProductUpdated)
	bus.OnAsync(pricing.EventPriceUpserted, s.HandlePriceUpserted)
	bus.OnAsync(inventory.EventStockUpdated, s.HandleStockUpdated)
	bus.OnAsync(catalog.EventCategoryCreated, s.HandleCategoryCreated)
	bus.OnAsync(catalog.EventCategoryUpdated, s.HandleCategoryUpdated)
	bus.OnAsync(catalog.EventCategoryDeleted, s.HandleCategoryDeleted)
	bus.OnAsync(cms.EventPageCreated, s.HandlePageCreated)
	bus.OnAsync(cms.EventPageUpdated, s.HandlePageUpdated)
	bus.OnAsync(cms.EventPageDeleted, s.HandlePageDeleted)
}

func (s *FPCInvalidationSubscriber) HandleProductCreated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(catalog.ProductCreatedData)
	if !ok {
		return fmt.Errorf("cache.fpc_invalidation: unexpected event data type %T", evt.Data)
	}
	// No cached page can carry product:{newID} yet — purge listing shells
	// so new products can appear once search/DB listing sources catch up.
	if err := s.deleteTag(ctx, ListingTag(), "tag", ListingTag()); err != nil {
		return err
	}
	return s.deleteTag(ctx, ProductTag(data.ProductID), "product_id", data.ProductID)
}

func (s *FPCInvalidationSubscriber) HandleProductUpdated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(catalog.ProductUpdatedData)
	if !ok {
		return fmt.Errorf("cache.fpc_invalidation: unexpected event data type %T", evt.Data)
	}
	// Category assignment / status changes can move a product into a PLP
	// that does not yet tag this product ID — clear listing shells too.
	if err := s.deleteTag(ctx, ListingTag(), "tag", ListingTag()); err != nil {
		return err
	}
	return s.deleteTag(ctx, ProductTag(data.ProductID), "product_id", data.ProductID)
}

func (s *FPCInvalidationSubscriber) HandlePriceUpserted(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(pricing.PriceUpsertedData)
	if !ok {
		return fmt.Errorf("cache.fpc_invalidation: unexpected event data type %T", evt.Data)
	}
	return s.deleteTag(ctx, ProductTag(data.ProductID), "product_id", data.ProductID)
}

func (s *FPCInvalidationSubscriber) HandleStockUpdated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(inventory.StockUpdatedData)
	if !ok {
		return fmt.Errorf("cache.fpc_invalidation: unexpected event data type %T", evt.Data)
	}
	return s.deleteTag(ctx, ProductTag(data.ProductID), "product_id", data.ProductID)
}

func (s *FPCInvalidationSubscriber) HandleCategoryCreated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(catalog.CategoryCreatedData)
	if !ok {
		return fmt.Errorf("cache.fpc_invalidation: unexpected event data type %T", evt.Data)
	}
	if err := s.deleteTag(ctx, NavigationTag(), "tag", NavigationTag()); err != nil {
		return err
	}
	return s.deleteTag(ctx, CategoryTag(data.CategoryID), "category_id", data.CategoryID)
}

func (s *FPCInvalidationSubscriber) HandleCategoryUpdated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(catalog.CategoryUpdatedData)
	if !ok {
		return fmt.Errorf("cache.fpc_invalidation: unexpected event data type %T", evt.Data)
	}
	if err := s.deleteTag(ctx, NavigationTag(), "tag", NavigationTag()); err != nil {
		return err
	}
	return s.deleteTag(ctx, CategoryTag(data.CategoryID), "category_id", data.CategoryID)
}

func (s *FPCInvalidationSubscriber) HandleCategoryDeleted(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(catalog.CategoryDeletedData)
	if !ok {
		return fmt.Errorf("cache.fpc_invalidation: unexpected event data type %T", evt.Data)
	}
	if err := s.deleteTag(ctx, NavigationTag(), "tag", NavigationTag()); err != nil {
		return err
	}
	return s.deleteTag(ctx, CategoryTag(data.CategoryID), "category_id", data.CategoryID)
}

// AfterProductsIndexed clears FPC entries once search has applied product
// changes (PR-1046 race: event-time purge can refill from a still-stale
// Meilisearch/index before the reindex job finishes). productIDs nil means
// a full-scan reindex — only the shared listing shell is purged.
func (s *FPCInvalidationSubscriber) AfterProductsIndexed(ctx context.Context, productIDs []string) error {
	if s == nil {
		return nil
	}
	if err := s.deleteTag(ctx, ListingTag(), "tag", ListingTag()); err != nil {
		return err
	}
	for _, id := range productIDs {
		if err := s.deleteTag(ctx, ProductTag(id), "product_id", id); err != nil {
			return err
		}
	}
	return nil
}

func (s *FPCInvalidationSubscriber) HandlePageCreated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(cms.PageCreatedData)
	if !ok {
		return fmt.Errorf("cache.fpc_invalidation: unexpected event data type %T", evt.Data)
	}
	return s.deleteTag(ctx, PageTag(data.PageID), "page_id", data.PageID)
}

func (s *FPCInvalidationSubscriber) HandlePageUpdated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(cms.PageUpdatedData)
	if !ok {
		return fmt.Errorf("cache.fpc_invalidation: unexpected event data type %T", evt.Data)
	}
	return s.deleteTag(ctx, PageTag(data.PageID), "page_id", data.PageID)
}

func (s *FPCInvalidationSubscriber) HandlePageDeleted(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(cms.PageDeletedData)
	if !ok {
		return fmt.Errorf("cache.fpc_invalidation: unexpected event data type %T", evt.Data)
	}
	return s.deleteTag(ctx, PageTag(data.PageID), "page_id", data.PageID)
}

func (s *FPCInvalidationSubscriber) deleteTag(ctx context.Context, tag, idKey, id string) error {
	tag = cache.NormalizeTag(tag)
	if tag == "" || id == "" {
		return nil
	}
	n, err := s.cache.DeleteByTag(ctx, tag)
	if err != nil {
		s.log.Error("cache.fpc_invalidation.error", err, map[string]interface{}{
			idKey: id,
			"tag": tag,
		})
		return fmt.Errorf("cache.fpc_invalidation: delete tag %q: %w", tag, err)
	}
	if n > 0 {
		s.log.Info("cache.fpc_invalidation.done", map[string]interface{}{
			idKey:     id,
			"tag":     tag,
			"deleted": n,
		})
	}
	return nil
}
