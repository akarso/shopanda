package pricing_test

import (
	"context"
	"testing"

	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	domain "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/promotion"
)

func makeCartPromo(id, name string, active bool, couponBound bool, cond, act interface{}) promotion.Promotion {
	p := makePromo(id, name, active, couponBound, cond, act)
	p.Type = promotion.TypeCart
	return p
}

func TestCartPromotionStep_MinCartTotalFixed(t *testing.T) {
	promos := &stubPromotionRepo{promos: []promotion.Promotion{
		makeCartPromo("pc1", "Cart $5 off", true, false,
			map[string]interface{}{"type": "min_cart_total", "value": 5000},
			map[string]interface{}{"type": "fixed", "amount": 500}),
	}}
	step := appPricing.NewCartPromotionStep(promos, &stubCouponRepo{})
	pctx := makePricingCtx(t, "USD", makeItem(t, "v1", 2, 3000, "USD"))

	if err := step.Apply(context.Background(), pctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pctx.Adjustments) != 1 {
		t.Fatalf("expected 1 cart adjustment, got %d", len(pctx.Adjustments))
	}
	if pctx.Adjustments[0].Amount.Amount() != 500 {
		t.Errorf("expected discount 500, got %d", pctx.Adjustments[0].Amount.Amount())
	}
}

func TestCartPromotionStep_MinCartTotalNotMet(t *testing.T) {
	promos := &stubPromotionRepo{promos: []promotion.Promotion{
		makeCartPromo("pc2", "Cart $5 off", true, false,
			map[string]interface{}{"type": "min_cart_total", "value": 10000},
			map[string]interface{}{"type": "fixed", "amount": 500}),
	}}
	step := appPricing.NewCartPromotionStep(promos, &stubCouponRepo{})
	pctx := makePricingCtx(t, "USD", makeItem(t, "v1", 1, 3000, "USD"))

	if err := step.Apply(context.Background(), pctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pctx.Adjustments) != 0 {
		t.Fatalf("expected no cart adjustment, got %d", len(pctx.Adjustments))
	}
}

func TestCartPromotionStep_FullPipeline(t *testing.T) {
	promos := &stubPromotionRepo{promos: []promotion.Promotion{
		makeCartPromo("pc3", "10% cart off", true, false,
			map[string]interface{}{"type": "min_cart_total", "value": 5000},
			map[string]interface{}{"type": "percentage", "percentage": 10}),
	}}
	catalog := appPricing.NewCatalogPromotionStep(&stubPromotionRepo{}, &stubCouponRepo{})
	cart := appPricing.NewCartPromotionStep(promos, &stubCouponRepo{})
	finalize := domain.NewFinalizeStep()
	pipeline := domain.NewPipeline(catalog, cart, finalize)

	pctx := makePricingCtx(t, "USD", makeItem(t, "v1", 2, 3000, "USD"))
	if err := pipeline.Execute(context.Background(), pctx); err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if pctx.DiscountsTotal.Amount() != 600 {
		t.Errorf("expected discounts 600, got %d", pctx.DiscountsTotal.Amount())
	}
	if pctx.GrandTotal.Amount() != 5400 {
		t.Errorf("expected grand total 5400, got %d", pctx.GrandTotal.Amount())
	}
}
