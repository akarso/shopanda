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
