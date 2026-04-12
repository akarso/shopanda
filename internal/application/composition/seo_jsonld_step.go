package composition

import (
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
)

// JSONLDProductStep adds a JSON-LD structured data block for the product.
type JSONLDProductStep struct {
	variants catalog.VariantRepository
	prices   pricing.PriceRepository
	stock    inventory.StockRepository
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

	price, err := s.prices.FindByVariantAndCurrency(ctx.Ctx, variants[0].ID, currency)
	if err == nil && price != nil {
		// Schema.org expects a decimal string (e.g. "29.99").
		offer["price"] = fmt.Sprintf("%.2f", float64(price.Amount.Amount())/100.0)
		offer["priceCurrency"] = price.Amount.Currency()
		hasAttrs = true
	}

	stock, err := s.stock.GetStock(ctx.Ctx, variants[0].ID)
	if err == nil {
		if stock.IsAvailable() {
			offer["availability"] = "https://schema.org/InStock"
		} else {
			offer["availability"] = "https://schema.org/OutOfStock"
		}
		hasAttrs = true
	}

	if !hasAttrs {
		return nil
	}
	return offer
}
