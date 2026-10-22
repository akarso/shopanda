package composition

import (
	"fmt"

	appinventory "github.com/akarso/shopanda/internal/application/inventory"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
)

// JSONLDProductStep adds a JSON-LD structured data block for the product.
type JSONLDProductStep struct {
	variants   catalog.VariantRepository
	prices     pricing.PriceRepository
	stock      inventory.StockRepository
	products   catalog.ProductRepository
	components catalog.BundleComponentRepository
}

// NewJSONLDProductStep creates a JSONLDProductStep.
func NewJSONLDProductStep(
	variants catalog.VariantRepository,
	prices pricing.PriceRepository,
	stock inventory.StockRepository,
) *JSONLDProductStep {
	return &JSONLDProductStep{
		variants: variants,
		prices:   prices,
		stock:    stock,
	}
}

// WithBundleStockRollup wires the reads used when a fixed-price bundle's
// stock_mode is dynamic. Availability then comes from one
// ResolveAvailableQty call (required-component floor; a digital-only kit is
// capped at catalog.MaxDigitalBundleLineQty), not a second component walk
// and not the bundle variant's own stock row. Nil repos, or a nil
// ProductContext.Ctx, fail closed (OutOfStock). PDP composition does not
// load visibility facts into Meta, so this step does not read a
// quantity_positive fact.
func (s *JSONLDProductStep) WithBundleStockRollup(products catalog.ProductRepository, components catalog.BundleComponentRepository) *JSONLDProductStep {
	s.products = products
	s.components = components
	return s
}

func (s *JSONLDProductStep) Name() string { return "seo_jsonld" }

func (s *JSONLDProductStep) Apply(ctx *ProductContext) error {
	if ctx == nil || ctx.Product == nil {
		return nil
	}

	ld := map[string]interface{}{
		"@context": "https://schema.org",
		"@type":    "Product",
		"name":     ctx.Product.Name,
	}
	if ctx.Product.Description != "" {
		ld["description"] = ctx.Product.Description
	}
	if ctx.Product.Slug != "" {
		ld["url"] = "/products/" + ctx.Product.Slug
	}

	offer := s.buildOffer(ctx)
	if offer != nil {
		ld["offers"] = offer
	}

	ctx.Blocks = append(ctx.Blocks, Block{
		Type: "jsonld",
		Data: map[string]interface{}{"jsonld": ld},
	})

	return nil
}

func (s *JSONLDProductStep) buildOffer(ctx *ProductContext) map[string]interface{} {
	// A dynamic bundle has no sellable stored price until ResolveUnitPrice
	// feeds this step. Skip the Offer entirely, including a stock-only Offer.
	if ctx.Product.PricingMode == catalog.PricingModeDynamic {
		return nil
	}

	variants, err := s.variants.ListByProductID(ctx.Ctx, ctx.Product.ID, 0, 1)
	if err != nil || len(variants) == 0 {
		return nil
	}

	offer := map[string]interface{}{
		"@type": "Offer",
	}
	hasAttrs := false

	currency := ctx.Currency
	if currency == "" {
		currency = "EUR"
	}

	price, err := s.prices.FindByVariantCurrencyAndStore(ctx.Ctx, variants[0].ID, currency, ctx.StoreID)
	if err == nil && price == nil && ctx.StoreID != "" {
		// Fall back to global price when store-scoped price not found.
		price, err = s.prices.FindByVariantCurrencyAndStore(ctx.Ctx, variants[0].ID, currency, "")
	}
	if err == nil && price != nil {
		// Schema.org expects a decimal string (e.g. "29.99").
		offer["price"] = fmt.Sprintf("%.2f", float64(price.Amount.Amount())/100.0)
		offer["priceCurrency"] = price.Amount.Currency()
		hasAttrs = true
	}

	if ctx.Product.Type == catalog.TypeBundle && ctx.Product.StockMode == catalog.StockModeDynamic {
		if s.dynamicBundleInStock(ctx) {
			offer["availability"] = "https://schema.org/InStock"
		} else {
			offer["availability"] = "https://schema.org/OutOfStock"
		}
		hasAttrs = true
	} else {
		stock, err := s.stock.GetStock(ctx.Ctx, variants[0].ID)
		if err == nil {
			if stock.IsAvailable() {
				offer["availability"] = "https://schema.org/InStock"
			} else {
				offer["availability"] = "https://schema.org/OutOfStock"
			}
			hasAttrs = true
		}
	}

	if !hasAttrs {
		return nil
	}
	return offer
}

// dynamicBundleInStock reports whether a dynamic-stock bundle can sell at
// least one kit. One ResolveAvailableQty call uses the wired stock and
// component repositories. Errors and a non-positive quantity are OutOfStock.
func (s *JSONLDProductStep) dynamicBundleInStock(ctx *ProductContext) bool {
	if s == nil || ctx == nil || ctx.Product == nil {
		return false
	}
	if s.components == nil || s.products == nil || s.variants == nil || s.stock == nil {
		return false
	}
	if ctx.Ctx == nil {
		return false
	}
	qty, err := appinventory.ResolveAvailableQty(ctx.Ctx, appinventory.AvailableQtySources{
		Stock:      s.stock,
		Variants:   s.variants,
		Products:   s.products,
		Components: s.components,
	}, ctx.Product.ID, "")
	return err == nil && qty > 0
}
