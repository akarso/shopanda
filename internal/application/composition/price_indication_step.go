package composition

import (
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
	domainlegal "github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
)

// PriceIndicationStep adds a price_indication block to the PDP containing the
// lowest price in the last 30 days (EU Omnibus directive). The block is only
// added when a lower historical price exists and Omnibus is enabled for the store.
type PriceIndicationStep struct {
	variants catalog.VariantRepository
	prices   pricing.PriceRepository
	history  pricing.PriceHistoryRepository
	config   domainlegal.ConfigGetter
}

// NewPriceIndicationStep creates a PriceIndicationStep.
func NewPriceIndicationStep(
	variants catalog.VariantRepository,
	prices pricing.PriceRepository,
	history pricing.PriceHistoryRepository,
	config domainlegal.ConfigGetter,
) *PriceIndicationStep {
	return &PriceIndicationStep{
		variants: variants,
		prices:   prices,
		history:  history,
		config:   config,
	}
}

func (s *PriceIndicationStep) Name() string { return "price_indication" }

func (s *PriceIndicationStep) Apply(ctx *ProductContext) error {
	if ctx == nil || ctx.Product == nil {
		return nil
	}
	// Dynamic bundles have no stored display price until ResolveUnitPrice is wired here.
	if ctx.Product.PricingMode == catalog.PricingModeDynamic {
		return nil
	}

	enabled, err := domainlegal.OmnibusEnabled(ctx.Ctx, s.config, ctx.StoreID)
	if err != nil {
		return fmt.Errorf("price indication: %w", err)
	}
	if !enabled {
		return nil
	}

	// A present meta.offer means ProductOfferStep ran. An unusable snapshot
	// has no amount. A failed batch read sets no offer and OfferReadFailed,
	// so this step does not list variants or read prices. The variant walk
	// is only for a pipeline with no offer step.
	if OfferReadFailed(ctx.Meta) {
		return nil
	}
	if offer, ok := OfferFromMeta(ctx.Meta); ok {
		if offer.Amount == nil || offer.VariantID == "" || offer.Currency == "" {
			return nil
		}
		money, err := shared.NewMoney(*offer.Amount, offer.Currency)
		if err != nil {
			return fmt.Errorf("price indication: %w", err)
		}
		blk, err := buildPriceIndicationBlockFromVariant(ctx.Ctx, s.history, offer.VariantID, ctx.StoreID, ctx.Currency, &pricing.Price{
			VariantID: offer.VariantID,
			Amount:    money,
		})
		if err != nil {
			return err
		}
		if blk != nil {
			ctx.Blocks = append(ctx.Blocks, *blk)
		}
		return nil
	}

	blk, err := buildPriceIndicationBlock(ctx.Ctx, s.variants, s.prices, s.history, ctx.Product.ID, ctx.StoreID, ctx.Currency)
	if err != nil {
		return err
	}
	if blk != nil {
		ctx.Blocks = append(ctx.Blocks, *blk)
	}
	return nil
}
