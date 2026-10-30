package cache

import (
	"context"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/platform/event"
)

// InvalidationSubscriber listens to product and price change events and
// removes related cache entries. Cache keys follow the convention
// "product:<productID>:" so a prefix-based delete can remove all
// store/language/currency variants at once.
type InvalidationSubscriber struct {
	cache cache.Cache
	log   Logger
	// bundleParents, when set, also invalidates parent kit product-cache
	// entries when a leaf product/price changes (PR-1080).
	bundleParents catalog.BundleParentLookup
}

// NewInvalidationSubscriber creates an InvalidationSubscriber.
func NewInvalidationSubscriber(c cache.Cache, log Logger) *InvalidationSubscriber {
	if c == nil {
		panic("cache.NewInvalidationSubscriber: nil cache")
	}
	if log == nil {
		panic("cache.NewInvalidationSubscriber: nil logger")
	}
	return &InvalidationSubscriber{cache: c, log: log}
}

// WithBundleParents enables product-cache fan-out to parent bundles that
// include the changed leaf (PR-1080). Nil clears the lookup. Returns s
// for chaining at wire sites.
func (s *InvalidationSubscriber) WithBundleParents(lookup catalog.BundleParentLookup) *InvalidationSubscriber {
	s.bundleParents = lookup
	return s
}

// Register wires event handlers on the given bus.
func (s *InvalidationSubscriber) Register(bus *event.Bus) {
	if bus == nil {
		panic("InvalidationSubscriber.Register: bus is nil")
	}
	bus.OnAsync(catalog.EventProductUpdated, s.HandleProductUpdated)
	bus.OnAsync(pricing.EventPriceUpserted, s.HandlePriceUpserted)
}

// ProductKeyPrefix returns the cache key prefix for a product.
// All cache entries for a product (across stores, languages, currencies)
// share this prefix. The productID must not contain SQL LIKE
// metacharacters (% or _); such IDs are rejected with a panic because
// they would cause incorrect prefix matching in DeleteByPrefix.
func ProductKeyPrefix(productID string) string {
	if strings.ContainsAny(productID, "%_") {
		panic(fmt.Sprintf("ProductKeyPrefix: productID %q contains LIKE metacharacter", productID))
	}
	return "product:" + productID + ":"
}

// HandleProductUpdated invalidates cache entries for the updated product
// and parent bundles that include it (PR-1080).
func (s *InvalidationSubscriber) HandleProductUpdated(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(catalog.ProductUpdatedData)
	if !ok {
		return fmt.Errorf("cache.invalidation: unexpected event data type %T", evt.Data)
	}
	return s.invalidateProductWithBundleParents(ctx, data.ProductID)
}

// HandlePriceUpserted invalidates cache entries for the product whose
// price changed and parent bundles that include it (PR-1080).
func (s *InvalidationSubscriber) HandlePriceUpserted(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(pricing.PriceUpsertedData)
	if !ok {
		return fmt.Errorf("cache.invalidation: unexpected event data type %T", evt.Data)
	}
	return s.invalidateProductWithBundleParents(ctx, data.ProductID)
}

func (s *InvalidationSubscriber) invalidateProductWithBundleParents(ctx context.Context, productID string) error {
	err := s.invalidateProduct(ctx, productID)
	if s.bundleParents == nil || productID == "" {
		return err
	}
	parents, lookupErr := s.bundleParents.ListBundleIDsByComponentID(ctx, productID)
	if lookupErr != nil {
		s.log.Error("cache.invalidation.bundle_parents_lookup_failed", lookupErr, map[string]interface{}{
			"product_id": productID,
		})
		return err
	}
	for _, parentID := range parents {
		if parentID == "" || parentID == productID {
			continue
		}
		if parentErr := s.invalidateProduct(ctx, parentID); parentErr != nil && err == nil {
			err = parentErr
		}
	}
	return err
}

func (s *InvalidationSubscriber) invalidateProduct(ctx context.Context, productID string) error {
	if productID == "" {
		return nil
	}
	prefix := ProductKeyPrefix(productID)
	if err := s.cache.DeleteByPrefix(ctx, prefix); err != nil {
		s.log.Error("cache.invalidation.error", err, map[string]interface{}{
			"product_id": productID,
		})
		return fmt.Errorf("cache.invalidation: delete prefix %q: %w", prefix, err)
	}
	s.log.Info("cache.invalidation.done", map[string]interface{}{
		"product_id": productID,
		"prefix":     prefix,
	})
	return nil
}
