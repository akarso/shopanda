package pricing

import (
	"context"
	"errors"
	"fmt"
	"sync"

	catalogapp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domain "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

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

// PriceNotAvailableMessage is the customer-facing text when a non-bundle
// variant has no stored price. It does not name the variant.
const PriceNotAvailableMessage = "price is not available"

// MissingUnitPriceError means no store-scoped or global price row exists.
// ProductType is set when ResolveUnitPrice already loaded the product.
// hasProductType is false for the legacy stored lookup, which has no product.
type MissingUnitPriceError struct {
	VariantID      string
	Currency       string
	ProductType    catalog.Type
	hasProductType bool
}

func (e *MissingUnitPriceError) Error() string {
	return fmt.Sprintf("no price for variant %s in %s", e.VariantID, e.Currency)
}

// KnownProductType reports the product type when the resolver loaded it.
func (e *MissingUnitPriceError) KnownProductType() (catalog.Type, bool) {
	if e == nil || !e.hasProductType {
		return "", false
	}
	return e.ProductType, true
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
		amount, err := lookupStoredUnitPrice(ctx, src.Prices, variantID, currency, storeID)
		var missing *MissingUnitPriceError
		if errors.As(err, &missing) && missing != nil {
			missing.ProductType = product.Type
			missing.hasProductType = true
		}
		return amount, err
	}
}

func resolveDynamicBundleUnitPrice(ctx context.Context, src UnitPriceSources, bundleProductID, currency, storeID string) (shared.Money, error) {
	leaves, err := catalogapp.RequiredLeafVariants(ctx, src.Products, src.Variants, src.Components, bundleProductID)
	if err != nil {
		return shared.Money{}, err
	}
	variantIDs := make([]string, len(leaves))
	for i, leaf := range leaves {
		variantIDs[i] = leaf.Variant.ID
	}

	priced, err := lookupComponentPrices(ctx, src.Prices, variantIDs, currency, storeID)
	if err != nil {
		// Internal read. Leave the bundle id off Error() so a copied log line
		// does not show it to a customer.
		return shared.Money{}, fmt.Errorf("bundle prices: %w", err)
	}
	total, err := shared.NewMoney(0, currency)
	if err != nil {
		return shared.Money{}, bundleCustomerPriceError(fmt.Sprintf("bundle %s currency: %v", bundleProductID, err))
	}
	for i, leaf := range leaves {
		price := priced[variantIDs[i]]
		if price == nil {
			return shared.Money{}, bundleCustomerPriceError(fmt.Sprintf(
				"dynamic bundle component %s has no price in %s",
				leaf.Component.ComponentProductID, currency,
			))
		}
		if price.Amount.Currency() != currency {
			return shared.Money{}, bundleCustomerPriceError(fmt.Sprintf(
				"bundle component %s currency %s does not match %s",
				leaf.Component.ComponentProductID, price.Amount.Currency(), currency,
			))
		}
		line, err := price.Amount.MulChecked(int64(leaf.Component.Quantity))
		if err != nil {
			return shared.Money{}, bundleCustomerPriceError(fmt.Sprintf("bundle component %s: %v", leaf.Component.ComponentProductID, err))
		}
		total, err = total.AddChecked(line)
		if err != nil {
			return shared.Money{}, bundleCustomerPriceError(fmt.Sprintf("bundle %s: %v", bundleProductID, err))
		}
	}
	return total, nil
}

// bundleCustomerPriceError hides a component or bundle id behind
// catalog.BundleCannotBeReservedMessage. The formatted detail stays on Unwrap.
func bundleCustomerPriceError(detail string) error {
	return catalogapp.BundleCustomerError(apperror.Validation(detail))
}

// LookupComponentPrices loads store-then-global stored prices for variantIDs.
// It does not roll a dynamic bundle up from its components. Exploded order
// lines are simple, virtual, or downloadable, so the stored row is the unit
// price. FindByVariantsCurrencyAndStore is one round trip; a non-empty store
// still falls back per missing variant to the global row.
func LookupComponentPrices(ctx context.Context, prices domain.PriceRepository, variantIDs []string, currency, storeID string) (map[string]*domain.Price, error) {
	return lookupComponentPrices(ctx, prices, variantIDs, currency, storeID)
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
