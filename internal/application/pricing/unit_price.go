package pricing

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/akarso/shopanda/internal/domain/catalog"
	domain "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// componentVariantProbe is how many variants we load to decide that a dynamic
// component has exactly one. Two is enough to reject a multi-variant component
// without paging the whole catalog.
const componentVariantProbe = 2

// UnitPriceSources are the reads ResolveUnitPrice needs.
// Production BasePriceStep and the cart share one value of this struct.
// Prices, Variants, and Products are required. Components is required when the
// variant belongs to a dynamic bundle. ResolveUnitPrice never writes prices
// or price_history.
type UnitPriceSources struct {
	Prices     domain.PriceRepository
	Variants   catalog.VariantRepository
	Products   catalog.ProductRepository
	Components catalog.BundleComponentRepository
}

// MissingUnitPriceError means no store-scoped or global price row exists.
type MissingUnitPriceError struct {
	VariantID string
	Currency  string
}

func (e *MissingUnitPriceError) Error() string {
	return fmt.Sprintf("no price for variant %s in %s", e.VariantID, e.Currency)
}

// IsMissingUnitPrice reports a missing stored price row.
func IsMissingUnitPrice(err error) bool {
	var target *MissingUnitPriceError
	return errors.As(err, &target)
}

type unitPriceCacheKey struct {
	variantID string
	currency  string
	storeID   string
}

type unitPriceCacheEntry struct {
	amount shared.Money
	err    error
}

type unitPriceCache struct {
	mu sync.Mutex
	m  map[unitPriceCacheKey]unitPriceCacheEntry
}

type unitPriceCacheCtxKey struct{}

// WithUnitPriceCache memoizes ResolveUnitPrice for the rest of ctx, keyed by
// variant, currency, and store. A second call on the same ctx returns the
// first result. Cart add and recalculate share one cache so the pipeline does
// not roll a dynamic bundle up twice.
func WithUnitPriceCache(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Value(unitPriceCacheCtxKey{}).(*unitPriceCache); ok {
		return ctx
	}
	return context.WithValue(ctx, unitPriceCacheCtxKey{}, &unitPriceCache{
		m: make(map[unitPriceCacheKey]unitPriceCacheEntry),
	})
}

func unitPriceCacheFrom(ctx context.Context) *unitPriceCache {
	if ctx == nil {
		return nil
	}
	cache, _ := ctx.Value(unitPriceCacheCtxKey{}).(*unitPriceCache)
	return cache
}

func (c *unitPriceCache) get(key unitPriceCacheKey) (shared.Money, error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.m[key]
	if !ok {
		return shared.Money{}, nil, false
	}
	return entry.amount, entry.err, true
}

func (c *unitPriceCache) set(key unitPriceCacheKey, amount shared.Money, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = unitPriceCacheEntry{amount: amount, err: err}
}

// ResolveUnitPrice returns the unit price for variantID in currency.
// storeID empty means the global price. A non-empty store falls back to global
// when that scope has no row.
//
// fixed and every non-bundle (except grouped) use the variant's price row.
// A dynamic bundle sums required components only: each component must be an
// active leaf (unless the context includes non-active products), must have
// exactly one variant, and is priced store-then-global, multiplied by component
// quantity. An empty required set is an error. A sum of 0 is valid. More than
// catalog.MaxBundleRequiredComponents required rows is an error.
// A grouped parent is always a validation error, even when a price row exists.
//
// Nil variants or products is a validation error. Callers must not treat a
// missing catalog as permission to return the stored row. Tests that want
// stored prices only use NewBasePriceStepLegacy.
func ResolveUnitPrice(ctx context.Context, src UnitPriceSources, variantID, currency, storeID string) (shared.Money, error) {
	key := unitPriceCacheKey{variantID: variantID, currency: currency, storeID: storeID}
	if cache := unitPriceCacheFrom(ctx); cache != nil {
		if amount, err, ok := cache.get(key); ok {
			return amount, err
		}
	}
	amount, err := resolveUnitPriceUncached(ctx, src, variantID, currency, storeID)
	if cache := unitPriceCacheFrom(ctx); cache != nil {
		cache.set(key, amount, err)
	}
	return amount, err
}

func resolveUnitPriceUncached(ctx context.Context, src UnitPriceSources, variantID, currency, storeID string) (shared.Money, error) {
	if src.Prices == nil {
		return shared.Money{}, apperror.Internal("price repository is required")
	}
	if src.Variants == nil || src.Products == nil {
		return shared.Money{}, apperror.Validation("catalog repositories are required to resolve a unit price")
	}

	variant, err := src.Variants.FindByID(ctx, variantID)
	if err != nil {
		return shared.Money{}, fmt.Errorf("variant %s: %w", variantID, err)
	}
	if variant == nil {
		return shared.Money{}, apperror.NotFound(fmt.Sprintf("variant %s not found", variantID))
	}
	product, err := src.Products.FindByID(ctx, variant.ProductID)
	if err != nil {
		return shared.Money{}, fmt.Errorf("product %s: %w", variant.ProductID, err)
	}
	if product == nil {
		return shared.Money{}, apperror.NotFound(fmt.Sprintf("product %s not found", variant.ProductID))
	}

	switch {
	case product.Type == catalog.TypeGrouped:
		return shared.Money{}, apperror.Validation(catalog.GroupedPriceRejectedMessage)
	case product.Type == catalog.TypeBundle && product.PricingMode == catalog.PricingModeDynamic:
		return resolveDynamicBundleUnitPrice(ctx, src, product.ID, currency, storeID)
	default:
		return lookupStoredUnitPrice(ctx, src.Prices, variantID, currency, storeID)
	}
}

func resolveDynamicBundleUnitPrice(ctx context.Context, src UnitPriceSources, bundleProductID, currency, storeID string) (shared.Money, error) {
	if src.Components == nil {
		return shared.Money{}, apperror.Validation("bundle component repository is required")
	}
	components, err := src.Components.ListByBundleID(ctx, bundleProductID)
	if err != nil {
		return shared.Money{}, fmt.Errorf("bundle %s components: %w", bundleProductID, err)
	}

	required := make([]catalog.BundleComponent, 0)
	for _, component := range components {
		if !component.Required {
			continue
		}
		if component.Quantity < 1 {
			return shared.Money{}, apperror.Validation(fmt.Sprintf("dynamic bundle component %s has invalid quantity", component.ComponentProductID))
		}
		required = append(required, component)
	}
	if len(required) == 0 {
		return shared.Money{}, apperror.Validation(fmt.Sprintf("dynamic bundle %s has no required components", bundleProductID))
	}
	if len(required) > catalog.MaxBundleRequiredComponents {
		return shared.Money{}, apperror.Validation(fmt.Sprintf(
			"dynamic bundle %s has %d required components; the maximum is %d",
			bundleProductID, len(required), catalog.MaxBundleRequiredComponents,
		))
	}

	loadCtx := ctx
	if !catalog.IncludeNonActiveProducts(ctx) {
		loadCtx = catalog.WithIncludeNonActiveProducts(ctx)
	}
	allowInactive := catalog.IncludeNonActiveProducts(ctx)
	productIDs := make([]string, len(required))
	for i, component := range required {
		productIDs[i] = component.ComponentProductID
		product, err := src.Products.FindByID(loadCtx, component.ComponentProductID)
		if err != nil {
			return shared.Money{}, fmt.Errorf("bundle component %s: %w", component.ComponentProductID, err)
		}
		if product == nil {
			return shared.Money{}, apperror.Validation(fmt.Sprintf("dynamic bundle component %s was not found", component.ComponentProductID))
		}
		if !allowInactive && product.Status != catalog.StatusActive {
			return shared.Money{}, apperror.Validation(fmt.Sprintf("dynamic bundle component %s is not active", component.ComponentProductID))
		}
		if !product.Type.IsBundleComponentLeafType() {
			return shared.Money{}, apperror.Validation(fmt.Sprintf(
				"dynamic bundle component %s must be a simple, virtual, or downloadable product",
				component.ComponentProductID,
			))
		}
	}

	variantsByProduct, err := src.Variants.ListByProductIDs(ctx, productIDs, componentVariantProbe)
	if err != nil {
		return shared.Money{}, fmt.Errorf("bundle %s component variants: %w", bundleProductID, err)
	}
	variantIDs := make([]string, len(required))
	for i, component := range required {
		variants := variantsByProduct[component.ComponentProductID]
		if len(variants) != 1 {
			return shared.Money{}, apperror.Validation(fmt.Sprintf(
				"dynamic bundle component %s must have exactly one variant",
				component.ComponentProductID,
			))
		}
		variantIDs[i] = variants[0].ID
	}

	priced, err := lookupComponentPrices(ctx, src.Prices, variantIDs, currency, storeID)
	if err != nil {
		return shared.Money{}, fmt.Errorf("bundle %s prices: %w", bundleProductID, err)
	}
	total, err := shared.NewMoney(0, currency)
	if err != nil {
		return shared.Money{}, apperror.Validation(fmt.Sprintf("bundle %s currency: %v", bundleProductID, err))
	}
	for i, component := range required {
		price := priced[variantIDs[i]]
		if price == nil {
			return shared.Money{}, apperror.Validation(fmt.Sprintf(
				"dynamic bundle component %s has no price in %s",
				component.ComponentProductID, currency,
			))
		}
		line, err := price.Amount.MulChecked(int64(component.Quantity))
		if err != nil {
			return shared.Money{}, apperror.Validation(fmt.Sprintf("bundle component %s: %v", component.ComponentProductID, err))
		}
		total, err = total.AddChecked(line)
		if err != nil {
			return shared.Money{}, apperror.Validation(fmt.Sprintf("bundle %s: %v", bundleProductID, err))
		}
	}
	return total, nil
}

// lookupComponentPrices loads component variant prices with the same
// store-then-global rule as lookupStoredUnitPrice. FindByVariantsCurrencyAndStore
// is required to fall back itself; any variant still missing after that call
// is read again with the singular global lookup so a batch helper that only
// returns the exact store row cannot hide a global component price.
func lookupComponentPrices(ctx context.Context, prices domain.PriceRepository, variantIDs []string, currency, storeID string) (map[string]*domain.Price, error) {
	priced, err := prices.FindByVariantsCurrencyAndStore(ctx, variantIDs, currency, storeID)
	if err != nil {
		return nil, err
	}
	if priced == nil {
		priced = map[string]*domain.Price{}
	}
	if storeID == "" {
		return priced, nil
	}
	for _, variantID := range variantIDs {
		if priced[variantID] != nil {
			continue
		}
		global, err := prices.FindByVariantCurrencyAndStore(ctx, variantID, currency, "")
		if err != nil {
			return nil, err
		}
		if global != nil {
			priced[variantID] = global
		}
	}
	return priced, nil
}

func lookupStoredUnitPrice(ctx context.Context, prices domain.PriceRepository, variantID, currency, storeID string) (shared.Money, error) {
	price, err := prices.FindByVariantCurrencyAndStore(ctx, variantID, currency, storeID)
	if err != nil {
		return shared.Money{}, fmt.Errorf("variant %s: %w", variantID, err)
	}
	if price == nil && storeID != "" {
		price, err = prices.FindByVariantCurrencyAndStore(ctx, variantID, currency, "")
		if err != nil {
			return shared.Money{}, fmt.Errorf("variant %s (fallback): %w", variantID, err)
		}
	}
	if price == nil {
		return shared.Money{}, &MissingUnitPriceError{VariantID: variantID, Currency: currency}
	}
	return price.Amount, nil
}
