package pluginsdk

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/pkg/extapi"
)

// AdaptPricingStep wraps a public PricingStep as a domain pricing.PricingStep
// for registration on the core pipeline.
func AdaptPricingStep(step extapi.PricingStep) pricing.PricingStep {
	if step == nil {
		panic("pluginsdk: AdaptPricingStep: step must not be nil")
	}
	return &pricingStepBridge{step: step}
}

type pricingStepBridge struct {
	step extapi.PricingStep
}

func (b *pricingStepBridge) Name() string { return b.step.Name() }

func (b *pricingStepBridge) Apply(ctx context.Context, pctx *pricing.PricingContext) error {
	view := &pricingContextView{pctx: pctx}
	return b.step.Apply(ctx, view)
}

type pricingContextView struct {
	pctx *pricing.PricingContext
}

func (v *pricingContextView) Currency() string { return v.pctx.Currency }

func (v *pricingContextView) Items() []extapi.PricingLine {
	out := make([]extapi.PricingLine, len(v.pctx.Items))
	for i, item := range v.pctx.Items {
		out[i] = extapi.PricingLine{
			VariantID:   item.VariantID,
			Quantity:    item.Quantity,
			UnitPrice:   MoneyFromShared(item.UnitPrice),
			Total:       MoneyFromShared(item.Total),
			Adjustments: adjustmentsFromDomain(item.Adjustments),
		}
	}
	return out
}

// Adjustments returns cart-level adjustments (not line adjustments — see Items()).
func (v *pricingContextView) Adjustments() []extapi.PricingAdjustment {
	return adjustmentsFromDomain(v.pctx.Adjustments)
}

func (v *pricingContextView) Subtotal() extapi.Money   { return MoneyFromShared(v.pctx.Subtotal) }
func (v *pricingContextView) Discounts() extapi.Money  { return MoneyFromShared(v.pctx.DiscountsTotal) }
func (v *pricingContextView) Tax() extapi.Money        { return MoneyFromShared(v.pctx.TaxTotal) }
func (v *pricingContextView) Fees() extapi.Money       { return MoneyFromShared(v.pctx.FeesTotal) }
func (v *pricingContextView) GrandTotal() extapi.Money { return MoneyFromShared(v.pctx.GrandTotal) }

func adjustmentFromDomain(adj pricing.Adjustment) extapi.PricingAdjustment {
	return extapi.PricingAdjustment{
		Type:        extapi.AdjustmentType(adj.Type),
		Code:        adj.Code,
		Description: adj.Description,
		Amount:      MoneyFromShared(adj.Amount),
		Included:    adj.Included,
		Meta:        adj.Meta,
	}
}

func adjustmentsFromDomain(adjs []pricing.Adjustment) []extapi.PricingAdjustment {
	if len(adjs) == 0 {
		return nil
	}
	out := make([]extapi.PricingAdjustment, len(adjs))
	for i, adj := range adjs {
		out[i] = adjustmentFromDomain(adj)
	}
	return out
}

func (v *pricingContextView) AddAdjustment(adj extapi.PricingAdjustment) error {
	amount, err := MoneyToShared(adj.Amount)
	if err != nil {
		return err
	}
	domainAdj, err := pricing.NewAdjustment(pricing.AdjustmentType(adj.Type), adj.Code, amount)
	if err != nil {
		return err
	}
	domainAdj.Description = adj.Description
	domainAdj.Included = adj.Included
	if adj.Meta != nil {
		domainAdj.Meta = adj.Meta
	}
	v.pctx.Adjustments = append(v.pctx.Adjustments, domainAdj)
	return nil
}

func (v *pricingContextView) AddItemAdjustment(index int, adj extapi.PricingAdjustment) error {
	if index < 0 || index >= len(v.pctx.Items) {
		return fmt.Errorf("pluginsdk: pricing context: item index %d out of range", index)
	}
	amount, err := MoneyToShared(adj.Amount)
	if err != nil {
		return err
	}
	domainAdj, err := pricing.NewAdjustment(pricing.AdjustmentType(adj.Type), adj.Code, amount)
	if err != nil {
		return err
	}
	domainAdj.Description = adj.Description
	domainAdj.Included = adj.Included
	if adj.Meta != nil {
		domainAdj.Meta = adj.Meta
	}
	v.pctx.Items[index].Adjustments = append(v.pctx.Items[index].Adjustments, domainAdj)
	return nil
}

func (v *pricingContextView) SetItemPrice(index int, unitPrice, total extapi.Money) error {
	if index < 0 || index >= len(v.pctx.Items) {
		return fmt.Errorf("pluginsdk: pricing context: item index %d out of range", index)
	}
	unit, err := MoneyToShared(unitPrice)
	if err != nil {
		return err
	}
	tot, err := MoneyToShared(total)
	if err != nil {
		return err
	}
	v.pctx.Items[index].UnitPrice = unit
	v.pctx.Items[index].Total = tot
	return nil
}

func (v *pricingContextView) MetaGet(key string) (interface{}, bool) {
	if v.pctx.Meta == nil {
		return nil, false
	}
	val, ok := v.pctx.Meta[key]
	return val, ok
}

func (v *pricingContextView) MetaSet(key string, value interface{}) {
	if v.pctx.Meta == nil {
		v.pctx.Meta = make(map[string]interface{})
	}
	v.pctx.Meta[key] = value
}

// coercePricingStep returns a domain PricingStep if step is public or already domain.
func coercePricingStep(step any) (pricing.PricingStep, error) {
	if step == nil {
		return nil, fmt.Errorf("pluginsdk: pricing step must not be nil")
	}
	if ps, ok := step.(extapi.PricingStep); ok {
		return AdaptPricingStep(ps), nil
	}
	if ps, ok := step.(pricing.PricingStep); ok {
		return ps, nil
	}
	return nil, fmt.Errorf("pluginsdk: expected extapi.PricingStep or pricing.PricingStep, got %T", step)
}
