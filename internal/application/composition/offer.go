package composition

import (
	"context"
	"fmt"
	"strings"
	"sync"

	appinventory "github.com/akarso/shopanda/internal/application/inventory"
	apppricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

const (
	// MetaOffer is the single-product sellable snapshot on a PDP composition context.
	MetaOffer = "offer"
	// MetaOffers maps product ID to the card snapshot on a listing composition context.
	MetaOffers = "offers"
	// metaOfferReadFailed is set when the PDP offer step ran and the batch read
	// failed. It is not a sellable snapshot and must not be copied into the
	// public meta map.
	metaOfferReadFailed = "offer_read_failed"
)

// PublicOffer is the price and on-hand availability a storefront can render
// without another round trip. Amount is minor units of the request currency.
// A nil Amount means this currency has no price. Zero is a real price.
// VariantID is set only when Amount is set. InStock is on-hand quantity > 0
// for a fixed product (checkout reservations already reduced that quantity),
// or a dynamic-stock bundle rollup. It is not the cart purchasable-axis gate.
type PublicOffer struct {
	VariantID string `json:"variant_id,omitempty"`
	Amount    *int64 `json:"amount"`
	Currency  string `json:"currency,omitempty"`
	InStock   bool   `json:"in_stock"`
}

// StockBatch loads on-hand quantities for many variants in one read.
// Missing ids are omitted; callers treat a missing key as quantity 0.
// Empty input returns an empty map and no error.
type StockBatch interface {
	GetStocks(ctx context.Context, variantIDs []string) (map[string]inventory.StockEntry, error)
}

// OfferSources are the reads behind public catalog price and availability.
// Variants, Prices, Stock, Stocks, and Products are required. Components is
// required when a product is a dynamic-price or dynamic-stock bundle.
// Log is optional; a per-product resolve failure is logged and that product
// is returned with a null amount and in_stock false.
type OfferSources struct {
	Variants   catalog.VariantRepository
	Prices     pricing.PriceRepository
	Stock      inventory.StockRepository
	Stocks     StockBatch
	Products   catalog.ProductRepository
	Components catalog.BundleComponentRepository
	Log        logger.Logger
}

func (s OfferSources) ready() bool {
	return s.Variants != nil && s.Prices != nil && s.Stock != nil && s.Stocks != nil && s.Products != nil && s.Components != nil
}

// Validate reports a wiring error when a catalog offer cannot run.
// Call it at process start. A missing component repository is not discovered
// per request.
func (s OfferSources) Validate() error {
	if s.ready() {
		return nil
	}
	return fmt.Errorf("catalog offers: variants, prices, stock, products, and components are required")
}

// ProductOffers returns one card offer per product. The amount is the cheapest
// variant in currency (store row, then global). InStock is true when any of
// those variants can sell, which may be a different variant than VariantID.
// A dynamic-price bundle uses ResolveUnitPrice and ignores its own price row.
// A dynamic-stock bundle uses ResolveAvailableQty and ignores its own stock row.
// A grouped parent has a null amount and in_stock false. At most
// maxVariantsForPriceIndication variants per product are considered.
func (s OfferSources) ProductOffers(ctx context.Context, products []*catalog.Product, storeID, currency string) (map[string]PublicOffer, error) {
	if !s.ready() {
		return nil, apperror.Internal("catalog offer is not configured")
	}
	ctx = withOfferLog(apppricing.WithUnitPriceCache(ctx))
	currency = offerCurrency(currency)
	out := make(map[string]PublicOffer, len(products))
	indexed := make([]*catalog.Product, 0, len(products))
	for _, product := range products {
		if product == nil || product.ID == "" {
			continue
		}
		if product.Type == catalog.TypeGrouped {
			out[product.ID] = PublicOffer{}
			continue
		}
		indexed = append(indexed, product)
	}
	if len(indexed) == 0 {
		return out, nil
	}

	ids := make([]string, len(indexed))
	for i, product := range indexed {
		ids[i] = product.ID
	}
	byProduct, err := s.Variants.ListByProductIDs(ctx, ids, maxVariantsForPriceIndication)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "catalog offer", err)
	}
	if byProduct == nil {
		byProduct = map[string][]catalog.Variant{}
	}

	variantIDs := make([]string, 0)
	for _, productVariants := range byProduct {
		for _, variant := range productVariants {
			variantIDs = append(variantIDs, variant.ID)
		}
	}
	prices, err := s.Prices.FindByVariantsCurrencyAndStore(ctx, variantIDs, currency, storeID)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "catalog offer", err)
	}
	if prices == nil {
		prices = map[string]*pricing.Price{}
	}
	stocks, err := s.loadVariantStock(ctx, variantIDs)
	if err != nil {
		return nil, err
	}

	for _, product := range indexed {
		offer, err := s.productOffer(ctx, product, byProduct[product.ID], prices, stocks, storeID, currency)
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			s.note(ctx, err, product.ID)
			out[product.ID] = PublicOffer{}
			continue
		}
		out[product.ID] = offer
	}
	return out, nil
}

// VariantOffers returns one offer per variant of product. A dynamic-price
// bundle resolves each requested variant on its own. A dynamic-stock bundle
// shares one rollup across those variants.
func (s OfferSources) VariantOffers(ctx context.Context, product *catalog.Product, variants []catalog.Variant, storeID, currency string) (map[string]PublicOffer, error) {
	if !s.ready() {
		return nil, apperror.Internal("catalog offer is not configured")
	}
	if product == nil {
		return nil, apperror.Internal("catalog offer product is required")
	}
	ctx = withOfferLog(apppricing.WithUnitPriceCache(ctx))
	currency = offerCurrency(currency)
	out := make(map[string]PublicOffer, len(variants))
	if len(variants) == 0 {
		return out, nil
	}
	if product.Type == catalog.TypeGrouped {
		for _, variant := range variants {
			out[variant.ID] = PublicOffer{}
		}
		return out, nil
	}

	ids := make([]string, len(variants))
	for i, variant := range variants {
		ids[i] = variant.ID
	}
	prices, err := s.Prices.FindByVariantsCurrencyAndStore(ctx, ids, currency, storeID)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "catalog offer", err)
	}
	if prices == nil {
		prices = map[string]*pricing.Price{}
	}
	stocks, err := s.loadVariantStock(ctx, ids)
	if err != nil {
		return nil, err
	}

	dynamicPrice := product.Type == catalog.TypeBundle && product.PricingMode == catalog.PricingModeDynamic
	dynamicStock := product.Type == catalog.TypeBundle && product.StockMode == catalog.StockModeDynamic
	sharedInStock := false
	if dynamicStock {
		inStock, stockErr := s.dynamicInStock(ctx, product.ID)
		if stockErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			s.note(ctx, stockErr, product.ID)
		} else {
			sharedInStock = inStock
		}
	}

	for _, variant := range variants {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		offer := PublicOffer{}
		if dynamicPrice {
			amount, _, ok, priceErr := s.resolveDisplayAmount(ctx, variant.ID, currency, storeID, product.ID)
			if priceErr != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				s.note(ctx, priceErr, product.ID)
			} else if ok {
				offer.Amount = &amount
				offer.Currency = currency
			}
		} else if price := prices[variant.ID]; price != nil {
			amount := price.Amount.Amount()
			offer.Amount = &amount
			offer.Currency = currency
		}
		if dynamicStock {
			offer.InStock = sharedInStock
		} else {
			offer.InStock = stocks[variant.ID].Quantity > 0
		}
		out[variant.ID] = offer
	}
	return out, nil
}

func (s OfferSources) productOffer(
	ctx context.Context,
	product *catalog.Product,
	variants []catalog.Variant,
	prices map[string]*pricing.Price,
	stocks map[string]inventory.StockEntry,
	storeID, currency string,
) (PublicOffer, error) {
	offer := PublicOffer{}
	if product.Type == catalog.TypeBundle && product.PricingMode == catalog.PricingModeDynamic {
		for _, variant := range variants {
			if ctx.Err() != nil {
				return PublicOffer{}, ctx.Err()
			}
			amount, _, ok, err := s.resolveDisplayAmount(ctx, variant.ID, currency, storeID, product.ID)
			if err != nil {
				if ctx.Err() != nil {
					return PublicOffer{}, ctx.Err()
				}
				s.note(ctx, err, product.ID)
				continue
			}
			if !ok {
				continue
			}
			offer.VariantID = variant.ID
			offer.Amount = &amount
			offer.Currency = currency
			break
		}
	} else if variantID, price := resolveLowestPriceVariantFromMaps(variants, prices); price != nil {
		amount := price.Amount.Amount()
		offer.VariantID = variantID
		offer.Amount = &amount
		offer.Currency = currency
	}

	inStock, err := s.productInStock(ctx, product, variants, stocks)
	if err != nil {
		if ctx.Err() != nil {
			return PublicOffer{}, ctx.Err()
		}
		s.note(ctx, err, product.ID)
		return offer, nil
	}
	offer.InStock = inStock
	return offer, nil
}

func (s OfferSources) productInStock(
	ctx context.Context,
	product *catalog.Product,
	variants []catalog.Variant,
	stocks map[string]inventory.StockEntry,
) (bool, error) {
	if product.Type == catalog.TypeBundle && product.StockMode == catalog.StockModeDynamic {
		return s.dynamicInStock(ctx, product.ID)
	}
	for _, variant := range variants {
		if stocks[variant.ID].Quantity > 0 {
			return true, nil
		}
	}
	return false, nil
}

func (s OfferSources) dynamicInStock(ctx context.Context, productID string) (bool, error) {
	if s.Components == nil {
		return false, fmt.Errorf("catalog offer: bundle components are required")
	}
	qty, err := appinventory.ResolveAvailableQty(ctx, appinventory.AvailableQtySources{
		Stock:      s.Stock,
		Variants:   s.Variants,
		Products:   s.Products,
		Components: s.Components,
	}, productID, "")
	if err != nil {
		if softOfferErr(err) {
			s.warn(ctx, productID, err)
			return false, nil
		}
		return false, fmt.Errorf("catalog offer: stock: %w", err)
	}
	return qty > 0, nil
}

func (s OfferSources) resolveDisplayAmount(ctx context.Context, variantID, currency, storeID, productID string) (int64, string, bool, error) {
	if s.Components == nil {
		return 0, "", false, fmt.Errorf("catalog offer: bundle components are required")
	}
	money, err := apppricing.ResolveUnitPrice(ctx, apppricing.UnitPriceSources{
		Prices:     s.Prices,
		Variants:   s.Variants,
		Products:   s.Products,
		Components: s.Components,
	}, variantID, currency, storeID)
	if err != nil {
		if apppricing.IsMissingUnitPrice(err) {
			return 0, "", false, nil
		}
		if softOfferErr(err) {
			s.warn(ctx, productID, err)
			return 0, "", false, nil
		}
		return 0, "", false, fmt.Errorf("catalog offer: price: %w", err)
	}
	return money.Amount(), money.Currency(), true, nil
}

func softOfferErr(err error) bool {
	return apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeNotFound)
}

func offerCurrency(currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		return defaultPriceIndicationCurrency
	}
	return currency
}

func (s OfferSources) loadVariantStock(ctx context.Context, variantIDs []string) (map[string]inventory.StockEntry, error) {
	if len(variantIDs) == 0 {
		return map[string]inventory.StockEntry{}, nil
	}
	entries, err := s.Stocks.GetStocks(ctx, variantIDs)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "catalog offer", err)
	}
	if entries == nil {
		return map[string]inventory.StockEntry{}, nil
	}
	return entries, nil
}

func (s OfferSources) note(ctx context.Context, err error, productID string) {
	if s.Log == nil || err == nil || ctx.Err() != nil || !claimOfferLog(ctx, productID, "error") {
		return
	}
	s.Log.Error("catalog.offer.product", err, map[string]interface{}{
		"product_id": productID,
	})
}

func (s OfferSources) warn(ctx context.Context, productID string, err error) {
	if s.Log == nil || err == nil || ctx.Err() != nil || !claimOfferLog(ctx, productID, "warn") {
		return
	}
	s.Log.Warn("catalog.offer.product", map[string]interface{}{
		"product_id": productID,
		"reason":     err.Error(),
	})
}

type offerLogKey struct{}

type offerLogOnce struct {
	mu     sync.Mutex
	warns  map[string]struct{}
	errors map[string]struct{}
}

func withOfferLog(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Value(offerLogKey{}).(*offerLogOnce); ok {
		return ctx
	}
	return context.WithValue(ctx, offerLogKey{}, &offerLogOnce{
		warns:  map[string]struct{}{},
		errors: map[string]struct{}{},
	})
}

func claimOfferLog(ctx context.Context, productID, kind string) bool {
	if ctx == nil {
		return true
	}
	state, _ := ctx.Value(offerLogKey{}).(*offerLogOnce)
	if state == nil {
		return true
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	seen := state.errors
	if kind == "warn" {
		seen = state.warns
	}
	if _, ok := seen[productID]; ok {
		return false
	}
	seen[productID] = struct{}{}
	return true
}

// OffersFromMeta reads the listing card map written by ListingOfferStep.
func OffersFromMeta(meta map[string]interface{}) map[string]PublicOffer {
	if meta == nil {
		return nil
	}
	raw, ok := meta[MetaOffers]
	if !ok || raw == nil {
		return nil
	}
	typed, ok := raw.(map[string]PublicOffer)
	if !ok {
		return nil
	}
	return typed
}

// OfferReadFailed reports that ProductOfferStep ran and the batch read failed.
// meta.offer is absent in that case. Price indication and JSON-LD must not
// treat the absence as "the offer step never ran".
func OfferReadFailed(meta map[string]interface{}) bool {
	if meta == nil {
		return false
	}
	failed, _ := meta[metaOfferReadFailed].(bool)
	return failed
}

// PublicMeta is the composition meta map safe to put in a response.
// The batch-failure flag is removed so a timeout is not cached as an offer.
func PublicMeta(meta map[string]interface{}) map[string]interface{} {
	if meta == nil || !OfferReadFailed(meta) {
		return meta
	}
	out := make(map[string]interface{}, len(meta)-1)
	for k, v := range meta {
		if k == metaOfferReadFailed {
			continue
		}
		out[k] = v
	}
	return out
}

// OfferFromMeta reads the PDP snapshot written by ProductOfferStep.
func OfferFromMeta(meta map[string]interface{}) (PublicOffer, bool) {
	if meta == nil {
		return PublicOffer{}, false
	}
	raw, ok := meta[MetaOffer]
	if !ok || raw == nil {
		return PublicOffer{}, false
	}
	typed, ok := raw.(PublicOffer)
	if !ok {
		return PublicOffer{}, false
	}
	return typed, true
}

// ProductOfferStep adds MetaOffer for the PDP product.
type ProductOfferStep struct {
	sources OfferSources
}

// NewProductOfferStep creates a ProductOfferStep. Unwired sources no-op.
func NewProductOfferStep(sources OfferSources) *ProductOfferStep {
	return &ProductOfferStep{sources: sources}
}

func (s *ProductOfferStep) Name() string { return "product_offer" }

func (s *ProductOfferStep) Apply(ctx *ProductContext) error {
	if ctx == nil || ctx.Product == nil || !s.sources.ready() {
		return nil
	}
	reqCtx := ctx.Ctx
	if reqCtx == nil {
		reqCtx = context.Background()
	}
	reqCtx = withOfferLog(reqCtx)
	offers, err := s.sources.ProductOffers(reqCtx, []*catalog.Product{ctx.Product}, ctx.StoreID, ctx.Currency)
	if ctx.Meta == nil {
		ctx.Meta = map[string]interface{}{}
	}
	if err != nil {
		// Do not publish a zero offer. Clients cannot tell it from a product
		// that really has no price and no stock, and the guest cache would
		// keep that lie for max-age.
		s.sources.note(reqCtx, err, ctx.Product.ID)
		ctx.Meta[metaOfferReadFailed] = true
		return nil
	}
	offer, ok := offers[ctx.Product.ID]
	if !ok {
		ctx.Meta[metaOfferReadFailed] = true
		return nil
	}
	ctx.Meta[MetaOffer] = offer
	return nil
}

// ListingOfferStep adds MetaOffers for every product on a listing.
type ListingOfferStep struct {
	sources OfferSources
}

// NewListingOfferStep creates a ListingOfferStep. Unwired sources no-op.
func NewListingOfferStep(sources OfferSources) *ListingOfferStep {
	return &ListingOfferStep{sources: sources}
}

func (s *ListingOfferStep) Name() string { return "listing_offer" }

func (s *ListingOfferStep) Apply(ctx *ListingContext) error {
	if ctx == nil || len(ctx.Products) == 0 || !s.sources.ready() {
		return nil
	}
	reqCtx := ctx.Ctx
	if reqCtx == nil {
		reqCtx = context.Background()
	}
	offers, err := s.sources.ProductOffers(reqCtx, ctx.Products, ctx.StoreID, ctx.Currency)
	if err != nil {
		return err
	}
	if ctx.Meta == nil {
		ctx.Meta = map[string]interface{}{}
	}
	ctx.Meta[MetaOffers] = offers
	return nil
}
