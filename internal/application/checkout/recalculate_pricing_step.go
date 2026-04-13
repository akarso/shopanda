package checkout

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/pricing"
)

// RecalculatePricingStep runs the pricing pipeline against the current
// cart items and stores the result in the checkout context metadata.
type RecalculatePricingStep struct {
	pipeline pricing.Pipeline
}

// NewRecalculatePricingStep creates a RecalculatePricingStep.
func NewRecalculatePricingStep(pipeline pricing.Pipeline) *RecalculatePricingStep {
	return &RecalculatePricingStep{pipeline: pipeline}
}

func (s *RecalculatePricingStep) Name() string { return "recalculate_pricing" }

// Execute builds a PricingContext from the cart, runs the pipeline,
// and stores the result in cctx.Meta["pricing"].
func (s *RecalculatePricingStep) Execute(cctx *Context) error {
	if v, ok := cctx.GetMeta("priced"); ok {
		if b, isBool := v.(bool); isBool && b {
			return nil // idempotent: already priced
		}
	}

	if cctx.Cart == nil {
		return fmt.Errorf("recalculate_pricing: cart not loaded")
	}

	pctx, err := pricing.NewPricingContext(cctx.Currency)
	if err != nil {
		return fmt.Errorf("recalculate_pricing: %w", err)
	}

	items := make([]pricing.PricingItem, 0, len(cctx.Cart.Items))
	for _, ci := range cctx.Cart.Items {
		pi, err := pricing.NewPricingItem(ci.VariantID, ci.Quantity, ci.UnitPrice)
		if err != nil {
			return fmt.Errorf("recalculate_pricing: item %s: %w", ci.VariantID, err)
		}
		items = append(items, pi)
	}
	pctx.Items = items

	// Forward tax configuration and store scope from checkout context to pricing context.
	for _, key := range []string{"tax_country", "tax_mode", "tax_classes", "store_id"} {
		if v, ok := cctx.GetMeta(key); ok {
			pctx.Meta[key] = v
		}
	}

	if err := s.pipeline.Execute(context.Background(), &pctx); err != nil {
		return fmt.Errorf("recalculate_pricing: %w", err)
	}

	cctx.SetMeta("pricing", &pctx)
	cctx.SetMeta("priced", true)
	return nil
}
