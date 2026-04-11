package pricing_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	domain "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/domain/shared"
)

// ── stubs ───────────────────────────────────────────────────────────────

type stubPromotionRepo struct {
	promos []promotion.Promotion
}

func (r *stubPromotionRepo) FindByID(_ context.Context, id string) (*promotion.Promotion, error) {
	for i := range r.promos {
		if r.promos[i].ID == id {
			return &r.promos[i], nil
		}
	}
	return nil, nil
}

func (r *stubPromotionRepo) ListActive(_ context.Context, typ promotion.PromotionType) ([]promotion.Promotion, error) {
	var out []promotion.Promotion
	for _, p := range r.promos {
		if p.Type == typ && p.Active {
			out = append(out, p)
		}
	}
	return out, nil
}

func (r *stubPromotionRepo) Save(_ context.Context, _ *promotion.Promotion) error { return nil }
func (r *stubPromotionRepo) Delete(_ context.Context, _ string) error             { return nil }

type stubCouponRepo struct {
	coupons []promotion.Coupon
}

func (r *stubCouponRepo) FindByCode(_ context.Context, code string) (*promotion.Coupon, error) {
	for i := range r.coupons {
		if r.coupons[i].Code == code {
			return &r.coupons[i], nil
		}
	}
	return nil, nil
}

func (r *stubCouponRepo) FindByID(_ context.Context, id string) (*promotion.Coupon, error) {
	for i := range r.coupons {
		if r.coupons[i].ID == id {
			return &r.coupons[i], nil
		}
	}
	return nil, nil
}

func (r *stubCouponRepo) ListByPromotion(_ context.Context, promoID string) ([]promotion.Coupon, error) {
	var out []promotion.Coupon
	for _, c := range r.coupons {
		if c.PromotionID == promoID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (r *stubCouponRepo) Save(_ context.Context, _ *promotion.Coupon) error { return nil }
func (r *stubCouponRepo) Delete(_ context.Context, _ string) error          { return nil }

// ── helpers ─────────────────────────────────────────────────────────────

func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func makePromo(id, name string, active bool, couponBound bool, cond, act interface{}) promotion.Promotion {
	now := time.Now()
	return promotion.Promotion{
		ID:          id,
		Name:        name,
		Type:        promotion.TypeCatalog,
		Active:      active,
		CouponBound: couponBound,
		Conditions:  mustJSON(cond),
		Actions:     mustJSON(act),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func makePricingCtx(currency string, items ...domain.PricingItem) *domain.PricingContext {
	pctx, _ := domain.NewPricingContext(currency)
	pctx.Items = items
	return &pctx
}

func makeItem(variantID string, qty int, unitPrice int64, currency string) domain.PricingItem {
	up := shared.MustNewMoney(unitPrice, currency)
	item, _ := domain.NewPricingItem(variantID, qty, up)
	return item
}

// ── tests ───────────────────────────────────────────────────────────────

func TestCatalogPromotionStep_Name(t *testing.T) {
	step := appPricing.NewCatalogPromotionStep(&stubPromotionRepo{}, &stubCouponRepo{})
	if step.Name() != "catalog_promotions" {
		t.Fatalf("expected name %q, got %q", "catalog_promotions", step.Name())
	}
}

func TestCatalogPromotionStep_NoPromotions(t *testing.T) {
	step := appPricing.NewCatalogPromotionStep(&stubPromotionRepo{}, &stubCouponRepo{})
	pctx := makePricingCtx("USD", makeItem("v1", 2, 1000, "USD"))

	if err := step.Apply(context.Background(), pctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pctx.Items[0].Adjustments) != 0 {
		t.Fatalf("expected no adjustments, got %d", len(pctx.Items[0].Adjustments))
	}
}

func TestCatalogPromotionStep_PercentageDiscount(t *testing.T) {
	promos := &stubPromotionRepo{promos: []promotion.Promotion{
		makePromo("p1", "10% off", true, false,
			map[string]string{"type": "always"},
			map[string]interface{}{"type": "percentage", "percentage": 10}),
	}}
	step := appPricing.NewCatalogPromotionStep(promos, &stubCouponRepo{})
	pctx := makePricingCtx("USD", makeItem("v1", 2, 1000, "USD"))
	// item total = 2 * 1000 = 2000; 10% of 2000 = 200

	if err := step.Apply(context.Background(), pctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pctx.Items[0].Adjustments) != 1 {
		t.Fatalf("expected 1 adjustment, got %d", len(pctx.Items[0].Adjustments))
	}
	adj := pctx.Items[0].Adjustments[0]
	if adj.Amount.Amount() != 200 {
		t.Errorf("expected discount 200, got %d", adj.Amount.Amount())
	}
	if adj.Type != domain.AdjustmentDiscount {
		t.Errorf("expected type discount, got %s", adj.Type)
	}
	if adj.Code != "promo.p1" {
		t.Errorf("expected code promo.p1, got %s", adj.Code)
	}
	// Item total should be reduced.
	if pctx.Items[0].Total.Amount() != 1800 {
		t.Errorf("expected item total 1800, got %d", pctx.Items[0].Total.Amount())
	}
}

func TestCatalogPromotionStep_FixedDiscount(t *testing.T) {
	promos := &stubPromotionRepo{promos: []promotion.Promotion{
		makePromo("p2", "$2 off each", true, false,
			map[string]string{"type": "always"},
			map[string]interface{}{"type": "fixed", "amount": 200}),
	}}
	step := appPricing.NewCatalogPromotionStep(promos, &stubCouponRepo{})
	pctx := makePricingCtx("USD", makeItem("v1", 3, 1000, "USD"))
	// item total = 3 * 1000 = 3000; fixed $2 per item * 3 = 600

	if err := step.Apply(context.Background(), pctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	adj := pctx.Items[0].Adjustments[0]
	if adj.Amount.Amount() != 600 {
		t.Errorf("expected discount 600, got %d", adj.Amount.Amount())
	}
	if pctx.Items[0].Total.Amount() != 2400 {
		t.Errorf("expected item total 2400, got %d", pctx.Items[0].Total.Amount())
	}
}

func TestCatalogPromotionStep_FixedDiscount_CappedAtTotal(t *testing.T) {
	promos := &stubPromotionRepo{promos: []promotion.Promotion{
		makePromo("p3", "$50 off each", true, false,
			map[string]string{"type": "always"},
			map[string]interface{}{"type": "fixed", "amount": 5000}),
	}}
	step := appPricing.NewCatalogPromotionStep(promos, &stubCouponRepo{})
	pctx := makePricingCtx("USD", makeItem("v1", 1, 1000, "USD"))
	// item total = 1000; fixed 5000 per item capped at 1000

	if err := step.Apply(context.Background(), pctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	adj := pctx.Items[0].Adjustments[0]
	if adj.Amount.Amount() != 1000 {
		t.Errorf("expected capped discount 1000, got %d", adj.Amount.Amount())
	}
	if pctx.Items[0].Total.Amount() != 0 {
		t.Errorf("expected item total 0, got %d", pctx.Items[0].Total.Amount())
	}
}

func TestCatalogPromotionStep_MinQuantityCondition(t *testing.T) {
	promos := &stubPromotionRepo{promos: []promotion.Promotion{
		makePromo("p4", "Buy 3+ get 10% off", true, false,
			map[string]interface{}{"type": "min_quantity", "value": 3},
			map[string]interface{}{"type": "percentage", "percentage": 10}),
	}}
	step := appPricing.NewCatalogPromotionStep(promos, &stubCouponRepo{})

	t.Run("quantity below threshold", func(t *testing.T) {
		pctx := makePricingCtx("USD", makeItem("v1", 2, 1000, "USD"))
		if err := step.Apply(context.Background(), pctx); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pctx.Items[0].Adjustments) != 0 {
			t.Errorf("expected no adjustments, got %d", len(pctx.Items[0].Adjustments))
		}
	})

	t.Run("quantity at threshold", func(t *testing.T) {
		pctx := makePricingCtx("USD", makeItem("v1", 3, 1000, "USD"))
		if err := step.Apply(context.Background(), pctx); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pctx.Items[0].Adjustments) != 1 {
			t.Fatalf("expected 1 adjustment, got %d", len(pctx.Items[0].Adjustments))
		}
		if pctx.Items[0].Adjustments[0].Amount.Amount() != 300 {
			t.Errorf("expected 300, got %d", pctx.Items[0].Adjustments[0].Amount.Amount())
		}
	})
}

func TestCatalogPromotionStep_InactivePromotionSkipped(t *testing.T) {
	promos := &stubPromotionRepo{promos: []promotion.Promotion{
		makePromo("p5", "inactive", false, false,
			map[string]string{"type": "always"},
			map[string]interface{}{"type": "percentage", "percentage": 50}),
	}}
	step := appPricing.NewCatalogPromotionStep(promos, &stubCouponRepo{})
	pctx := makePricingCtx("USD", makeItem("v1", 1, 1000, "USD"))

	if err := step.Apply(context.Background(), pctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Inactive promotions are filtered by ListActive stub, so no adjustments.
	if len(pctx.Items[0].Adjustments) != 0 {
		t.Errorf("expected no adjustments, got %d", len(pctx.Items[0].Adjustments))
	}
}

func TestCatalogPromotionStep_ExpiredPromotionSkipped(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour)
	p := makePromo("p6", "expired", true, false,
		map[string]string{"type": "always"},
		map[string]interface{}{"type": "percentage", "percentage": 50})
	p.EndAt = &past
	promos := &stubPromotionRepo{promos: []promotion.Promotion{p}}
	step := appPricing.NewCatalogPromotionStep(promos, &stubCouponRepo{})
	pctx := makePricingCtx("USD", makeItem("v1", 1, 1000, "USD"))

	if err := step.Apply(context.Background(), pctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pctx.Items[0].Adjustments) != 0 {
		t.Errorf("expected no adjustments for expired promo, got %d", len(pctx.Items[0].Adjustments))
	}
}

func TestCatalogPromotionStep_CouponBound_NoCoupon(t *testing.T) {
	promos := &stubPromotionRepo{promos: []promotion.Promotion{
		makePromo("p7", "coupon only", true, true,
			map[string]string{"type": "always"},
			map[string]interface{}{"type": "percentage", "percentage": 20}),
	}}
	step := appPricing.NewCatalogPromotionStep(promos, &stubCouponRepo{})
	pctx := makePricingCtx("USD", makeItem("v1", 1, 1000, "USD"))
	// No coupon_code in Meta → coupon-bound promo is skipped.

	if err := step.Apply(context.Background(), pctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pctx.Items[0].Adjustments) != 0 {
		t.Errorf("expected no adjustments, got %d", len(pctx.Items[0].Adjustments))
	}
}

func TestCatalogPromotionStep_CouponBound_ValidCoupon(t *testing.T) {
	promoID := "00000000-0000-0000-0000-000000000007"
	promos := &stubPromotionRepo{promos: []promotion.Promotion{
		makePromo(promoID, "20% with coupon", true, true,
			map[string]string{"type": "always"},
			map[string]interface{}{"type": "percentage", "percentage": 20}),
	}}
	coupons := &stubCouponRepo{coupons: []promotion.Coupon{
		{ID: "c1", Code: "SAVE20", PromotionID: promoID, Active: true},
	}}
	step := appPricing.NewCatalogPromotionStep(promos, coupons)
	pctx := makePricingCtx("USD", makeItem("v1", 1, 1000, "USD"))
	pctx.Meta["coupon_code"] = "SAVE20"

	if err := step.Apply(context.Background(), pctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pctx.Items[0].Adjustments) != 1 {
		t.Fatalf("expected 1 adjustment, got %d", len(pctx.Items[0].Adjustments))
	}
	if pctx.Items[0].Adjustments[0].Amount.Amount() != 200 {
		t.Errorf("expected 200, got %d", pctx.Items[0].Adjustments[0].Amount.Amount())
	}
}

func TestCatalogPromotionStep_CouponBound_WrongPromotion(t *testing.T) {
	promoID := "00000000-0000-0000-0000-000000000008"
	promos := &stubPromotionRepo{promos: []promotion.Promotion{
		makePromo(promoID, "20% with coupon", true, true,
			map[string]string{"type": "always"},
			map[string]interface{}{"type": "percentage", "percentage": 20}),
	}}
	coupons := &stubCouponRepo{coupons: []promotion.Coupon{
		{ID: "c2", Code: "OTHER", PromotionID: "other-promo-id", Active: true},
	}}
	step := appPricing.NewCatalogPromotionStep(promos, coupons)
	pctx := makePricingCtx("USD", makeItem("v1", 1, 1000, "USD"))
	pctx.Meta["coupon_code"] = "OTHER"

	if err := step.Apply(context.Background(), pctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pctx.Items[0].Adjustments) != 0 {
		t.Errorf("expected no adjustments (coupon for wrong promo), got %d", len(pctx.Items[0].Adjustments))
	}
}

func TestCatalogPromotionStep_MultipleItems(t *testing.T) {
	promos := &stubPromotionRepo{promos: []promotion.Promotion{
		makePromo("p10", "10% off all", true, false,
			map[string]string{"type": "always"},
			map[string]interface{}{"type": "percentage", "percentage": 10}),
	}}
	step := appPricing.NewCatalogPromotionStep(promos, &stubCouponRepo{})
	pctx := makePricingCtx("USD",
		makeItem("v1", 2, 1000, "USD"), // total 2000, discount 200
		makeItem("v2", 1, 500, "USD"),  // total 500, discount 50
	)

	if err := step.Apply(context.Background(), pctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pctx.Items[0].Adjustments[0].Amount.Amount() != 200 {
		t.Errorf("item 0: expected 200, got %d", pctx.Items[0].Adjustments[0].Amount.Amount())
	}
	if pctx.Items[1].Adjustments[0].Amount.Amount() != 50 {
		t.Errorf("item 1: expected 50, got %d", pctx.Items[1].Adjustments[0].Amount.Amount())
	}
}

func TestCatalogPromotionStep_MultiplePromotions(t *testing.T) {
	promos := &stubPromotionRepo{promos: []promotion.Promotion{
		makePromo("pa", "10% off", true, false,
			map[string]string{"type": "always"},
			map[string]interface{}{"type": "percentage", "percentage": 10}),
		makePromo("pb", "$1 off", true, false,
			map[string]string{"type": "always"},
			map[string]interface{}{"type": "fixed", "amount": 100}),
	}}
	step := appPricing.NewCatalogPromotionStep(promos, &stubCouponRepo{})
	pctx := makePricingCtx("USD", makeItem("v1", 1, 1000, "USD"))
	// promo A: 10% of 1000 = 100 → total becomes 900
	// promo B: $1 * 1 = 100 → total becomes 800

	if err := step.Apply(context.Background(), pctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pctx.Items[0].Adjustments) != 2 {
		t.Fatalf("expected 2 adjustments, got %d", len(pctx.Items[0].Adjustments))
	}
	if pctx.Items[0].Total.Amount() != 800 {
		t.Errorf("expected final total 800, got %d", pctx.Items[0].Total.Amount())
	}
}

func TestCatalogPromotionStep_NilConditions_DefaultsToAlways(t *testing.T) {
	promos := &stubPromotionRepo{promos: []promotion.Promotion{{
		ID:     "pn",
		Name:   "null cond",
		Type:   promotion.TypeCatalog,
		Active: true,
		Actions: mustJSON(map[string]interface{}{
			"type": "percentage", "percentage": 5,
		}),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}}}
	step := appPricing.NewCatalogPromotionStep(promos, &stubCouponRepo{})
	pctx := makePricingCtx("USD", makeItem("v1", 1, 2000, "USD"))

	if err := step.Apply(context.Background(), pctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pctx.Items[0].Adjustments) != 1 {
		t.Fatalf("expected 1 adjustment, got %d", len(pctx.Items[0].Adjustments))
	}
	if pctx.Items[0].Adjustments[0].Amount.Amount() != 100 {
		t.Errorf("expected 100, got %d", pctx.Items[0].Adjustments[0].Amount.Amount())
	}
}

func TestCatalogPromotionStep_InvalidAction_Error(t *testing.T) {
	promos := &stubPromotionRepo{promos: []promotion.Promotion{
		makePromo("pe", "bad action", true, false,
			map[string]string{"type": "always"},
			map[string]interface{}{"type": "unknown_action"}),
	}}
	step := appPricing.NewCatalogPromotionStep(promos, &stubCouponRepo{})
	pctx := makePricingCtx("USD", makeItem("v1", 1, 1000, "USD"))

	if err := step.Apply(context.Background(), pctx); err == nil {
		t.Fatal("expected error for unknown action type")
	}
}

func TestCatalogPromotionStep_FullPipeline(t *testing.T) {
	promos := &stubPromotionRepo{promos: []promotion.Promotion{
		makePromo("pf", "10% off", true, false,
			map[string]string{"type": "always"},
			map[string]interface{}{"type": "percentage", "percentage": 10}),
	}}
	step := appPricing.NewCatalogPromotionStep(promos, &stubCouponRepo{})
	finalize := domain.NewFinalizeStep()
	pipeline := domain.NewPipeline(step, finalize)

	pctx := makePricingCtx("USD", makeItem("v1", 2, 1000, "USD"))
	// Base total: 2000, discount: 200, item total after step: 1800
	// Finalize: Subtotal=1800, DiscountsTotal=200, GrandTotal=1800-200=1600

	if err := pipeline.Execute(context.Background(), pctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pctx.Subtotal.Amount() != 1800 {
		t.Errorf("expected subtotal 1800, got %d", pctx.Subtotal.Amount())
	}
	if pctx.DiscountsTotal.Amount() != 200 {
		t.Errorf("expected discounts 200, got %d", pctx.DiscountsTotal.Amount())
	}
	if pctx.GrandTotal.Amount() != 1600 {
		t.Errorf("expected grand total 1600, got %d", pctx.GrandTotal.Amount())
	}
}
