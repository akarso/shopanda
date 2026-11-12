package composition

// JSONLDProductStep adds a JSON-LD structured data block for the product.
// Price and availability come only from meta.offer, written by ProductOfferStep.
// There is no second price or stock lookup in this step.
type JSONLDProductStep struct{}

// NewJSONLDProductStep creates a JSONLDProductStep.
func NewJSONLDProductStep() *JSONLDProductStep {
	return &JSONLDProductStep{}
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

	if !OfferReadFailed(ctx.Meta) {
		if offer, ok := OfferFromMeta(ctx.Meta); ok {
			if schema := schemaOfferFromPublic(offer); schema != nil {
				ld["offers"] = schema
			}
		}
	}

	ctx.Blocks = append(ctx.Blocks, Block{
		Type: "jsonld",
		Data: map[string]interface{}{"jsonld": ld},
	})

	return nil
}

// schemaOfferFromPublic formats the catalog offer. Amount nil means no Offer:
// a stock-only Offer is rejected by Google rich results. The price is the
// catalog card price (cheapest of the considered variants). Availability is
// that card's in_stock flag, which can come from a different variant.
func schemaOfferFromPublic(offer PublicOffer) map[string]interface{} {
	if offer.Amount == nil {
		return nil
	}
	out := map[string]interface{}{
		"@type":        "Offer",
		"price":        formatMajorUnits(*offer.Amount),
		"availability": schemaAvailability(offer.InStock),
	}
	if offer.Currency != "" {
		out["priceCurrency"] = offer.Currency
	}
	return out
}

func schemaAvailability(inStock bool) string {
	if inStock {
		return "https://schema.org/InStock"
	}
	return "https://schema.org/OutOfStock"
}
