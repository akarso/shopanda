package admin_test

import (
	"context"
	"testing"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	domain "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestPromotionRuleForm_RoundTrip(t *testing.T) {
	form := adminApp.PromotionRuleForm{
		ConditionType:    "min_quantity",
		ConditionValue:   3,
		ActionType:       "percentage",
		ActionPercentage: 15,
	}
	conditions, actions, err := adminApp.EncodePromotionRules(promotion.TypeCatalog, form)
	if err != nil {
		t.Fatalf("EncodePromotionRules: %v", err)
	}
	got, err := adminApp.DecodePromotionRules(promotion.TypeCatalog, conditions, actions)
	if err != nil {
		t.Fatalf("DecodePromotionRules: %v", err)
	}
	if got.ConditionType != form.ConditionType || got.ConditionValue != form.ConditionValue {
		t.Fatalf("condition round-trip = %+v, want %+v", got, form)
	}
	if got.ActionType != form.ActionType || got.ActionPercentage != form.ActionPercentage {
		t.Fatalf("action round-trip = %+v, want %+v", got, form)
	}
}

func TestDecodePromotionRules_RejectsInvalidStoredValues(t *testing.T) {
	_, err := adminApp.DecodePromotionRules(
		promotion.TypeCatalog,
		[]byte(`{"type":"min_quantity","value":0}`),
		[]byte(`{"type":"percentage","percentage":10}`),
	)
	if err == nil {
		t.Fatal("expected error for non-positive min_quantity")
	}

	_, err = adminApp.DecodePromotionRules(
		promotion.TypeCatalog,
		[]byte(`{"type":"always"}`),
		[]byte(`{"type":"percentage","percentage":0}`),
	)
	if err == nil {
		t.Fatal("expected error for invalid percentage")
	}
}

func TestEncodePromotionRules_AppliedByCatalogPromotionStep(t *testing.T) {
	conditions, actions, err := adminApp.EncodePromotionRules(promotion.TypeCatalog, adminApp.PromotionRuleForm{
		ConditionType:    "always",
		ActionType:       "percentage",
		ActionPercentage: 10,
	})
	if err != nil {
		t.Fatalf("EncodePromotionRules: %v", err)
	}

	p := promotion.Promotion{
		ID:         id.New(),
		Name:       "10% off",
		Type:       promotion.TypeCatalog,
		Active:     true,
		Conditions: conditions,
		Actions:    actions,
	}

	step := appPricing.NewCatalogPromotionStep(&singlePromotionRepo{promo: p}, &noopCouponRepo{})
	up := shared.MustNewMoney(1000, "USD")
	item, err := domain.NewPricingItem("v1", 2, up)
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	pctx, err := domain.NewPricingContext("USD")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	pctx.Items = []domain.PricingItem{item}

	if err := step.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(pctx.Items[0].Adjustments) != 1 {
		t.Fatalf("expected 1 adjustment, got %d", len(pctx.Items[0].Adjustments))
	}
	if pctx.Items[0].Adjustments[0].Amount.Amount() != 200 {
		t.Fatalf("discount = %d, want 200", pctx.Items[0].Adjustments[0].Amount.Amount())
	}
}

func TestPromotionRuleForm_TieredRoundTrip(t *testing.T) {
	form := adminApp.PromotionRuleForm{
		ConditionType: "always",
		ActionType:    "tiered",
		ActionTiers: []adminApp.PromotionTierForm{
			{MinQty: 2, Percentage: 5},
			{MinQty: 5, Percentage: 15},
		},
	}
	conditions, actions, err := adminApp.EncodePromotionRules(promotion.TypeCatalog, form)
	if err != nil {
		t.Fatalf("EncodePromotionRules: %v", err)
	}
	got, err := adminApp.DecodePromotionRules(promotion.TypeCatalog, conditions, actions)
	if err != nil {
		t.Fatalf("DecodePromotionRules: %v", err)
	}
	if got.ActionType != "tiered" || len(got.ActionTiers) != 2 {
		t.Fatalf("got = %+v", got)
	}
	if got.ActionTiers[0].MinQty != 2 || got.ActionTiers[0].Percentage != 5 {
		t.Fatalf("tier[0] = %+v, want min_qty=2 percentage=5", got.ActionTiers[0])
	}
	if got.ActionTiers[1].MinQty != 5 || got.ActionTiers[1].Percentage != 15 {
		t.Fatalf("tier[1] = %+v, want min_qty=5 percentage=15", got.ActionTiers[1])
	}
}

func TestPromotionRuleForm_BuyXGetYRoundTrip(t *testing.T) {
	form := adminApp.PromotionRuleForm{
		ConditionType: "always",
		ActionType:    "buy_x_get_y",
		ActionBuyQty:  2,
		ActionGetQty:  1,
	}
	conditions, actions, err := adminApp.EncodePromotionRules(promotion.TypeCatalog, form)
	if err != nil {
		t.Fatalf("EncodePromotionRules: %v", err)
	}
	got, err := adminApp.DecodePromotionRules(promotion.TypeCatalog, conditions, actions)
	if err != nil {
		t.Fatalf("DecodePromotionRules: %v", err)
	}
	if got.ActionType != "buy_x_get_y" || got.ActionBuyQty != 2 || got.ActionGetQty != 1 {
		t.Fatalf("got = %+v, want buy_x_get_y 2/1", got)
	}
}

func TestDecodePromotionRules_RejectsEmptyTieredAction(t *testing.T) {
	_, err := adminApp.DecodePromotionRules(
		promotion.TypeCatalog,
		[]byte(`{"type":"always"}`),
		[]byte(`{"type":"tiered","tiers":[]}`),
	)
	if err == nil {
		t.Fatal("expected error for empty tiered action")
	}
}

func TestEncodePromotionRules_RejectsCatalogActionOnCart(t *testing.T) {
	_, _, err := adminApp.EncodePromotionRules(promotion.TypeCart, adminApp.PromotionRuleForm{
		ConditionType:  "min_cart_total",
		ConditionValue: 5000,
		ActionType:     "tiered",
		ActionTiers: []adminApp.PromotionTierForm{
			{MinQty: 2, Percentage: 5},
		},
	})
	if err == nil {
		t.Fatal("expected error for tiered cart promotion")
	}
}

func TestEncodePromotionRules_TieredAppliedByCatalogPromotionStep(t *testing.T) {
	conditions, actions, err := adminApp.EncodePromotionRules(promotion.TypeCatalog, adminApp.PromotionRuleForm{
		ConditionType: "always",
		ActionType:    "tiered",
		ActionTiers: []adminApp.PromotionTierForm{
			{MinQty: 2, Percentage: 5},
			{MinQty: 5, Percentage: 15},
		},
	})
	if err != nil {
		t.Fatalf("EncodePromotionRules: %v", err)
	}
	p := promotion.Promotion{
		ID:         id.New(),
		Name:       "Tiered",
		Type:       promotion.TypeCatalog,
		Active:     true,
		Conditions: conditions,
		Actions:    actions,
	}
	step := appPricing.NewCatalogPromotionStep(&singlePromotionRepo{promo: p}, &noopCouponRepo{})
	up := shared.MustNewMoney(1000, "USD")
	item, err := domain.NewPricingItem("v1", 5, up)
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	pctx, err := domain.NewPricingContext("USD")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	pctx.Items = []domain.PricingItem{item}
	if err := step.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(pctx.Items[0].Adjustments) != 1 {
		t.Fatalf("expected 1 adjustment, got %d", len(pctx.Items[0].Adjustments))
	}
	if pctx.Items[0].Adjustments[0].Amount.Amount() != 750 {
		t.Fatalf("discount = %d, want 750 (15%% of 5000)", pctx.Items[0].Adjustments[0].Amount.Amount())
	}
}

type singlePromotionRepo struct {
	promo promotion.Promotion
}

func (r *singlePromotionRepo) FindByID(context.Context, string) (*promotion.Promotion, error) {
	return nil, nil
}

func (r *singlePromotionRepo) ListActive(_ context.Context, typ promotion.PromotionType) ([]promotion.Promotion, error) {
	if typ == promotion.TypeCatalog {
		return []promotion.Promotion{r.promo}, nil
	}
	return nil, nil
}

func (r *singlePromotionRepo) List(context.Context, int, int) ([]promotion.Promotion, error) {
	return nil, nil
}

func (r *singlePromotionRepo) Save(context.Context, *promotion.Promotion) error { return nil }
func (r *singlePromotionRepo) Delete(context.Context, string) error             { return nil }

type noopCouponRepo struct{}

func (noopCouponRepo) FindByCode(context.Context, string) (*promotion.Coupon, error) {
	return nil, nil
}
func (noopCouponRepo) FindByID(context.Context, string) (*promotion.Coupon, error) { return nil, nil }
func (noopCouponRepo) ListByPromotion(context.Context, string) ([]promotion.Coupon, error) {
	return nil, nil
}
func (noopCouponRepo) List(context.Context, int, int) ([]promotion.Coupon, error) { return nil, nil }
func (noopCouponRepo) Save(context.Context, *promotion.Coupon) error              { return nil }
func (noopCouponRepo) Delete(context.Context, string) error                       { return nil }
