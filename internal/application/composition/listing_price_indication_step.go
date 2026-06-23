package composition

import (
	"fmt"

	domainlegal "github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
)

// ListingPriceIndicationStep adds per-product Omnibus data to listing Meta for PLP cards.
type ListingPriceIndicationStep struct {
	variants catalog.VariantRepository
	prices   pricing.PriceRepository
	history  pricing.PriceHistoryRepository
	config   domainlegal.ConfigGetter
}

// NewListingPriceIndicationStep creates a ListingPriceIndicationStep.
func NewListingPriceIndicationStep(
	variants catalog.VariantRepository,
	prices pricing.PriceRepository,
	history pricing.PriceHistoryRepository,
	config domainlegal.ConfigGetter,
) *ListingPriceIndicationStep {
	return &ListingPriceIndicationStep{
		variants: variants,
		prices:   prices,
		history:  history,
		config:   config,
	}
}

func (s *ListingPriceIndicationStep) Name() string { return "listing_price_indication" }

func (s *ListingPriceIndicationStep) Apply(ctx *ListingContext) error {
	if ctx == nil || len(ctx.Products) == 0 {
		return nil
	}

	enabled, err := domainlegal.OmnibusEnabled(ctx.Ctx, s.config, ctx.StoreID)
	if err != nil {
		return fmt.Errorf("listing price indication: %w", err)
	}
	if !enabled {
		return nil
	}

	indications := make(map[string]map[string]interface{}, len(ctx.Products))
	for _, product := range ctx.Products {
		if product == nil {
			continue
		}
		blk, err := buildPriceIndicationBlock(ctx.Ctx, s.variants, s.prices, s.history, product.ID, ctx.StoreID, ctx.Currency)
		if err != nil {
			return err
		}
		if blk != nil {
			indications[product.ID] = blk.Data
		}
	}
	if len(indications) == 0 {
		return nil
	}
	if ctx.Meta == nil {
		ctx.Meta = make(map[string]interface{})
	}
	ctx.Meta[metaPriceIndications] = indications
	return nil
}
