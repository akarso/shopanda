package promotion_test

import (
	"context"
	"testing"

	domainpricing "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/domain/shared"
)

func TestEvaluatorRegistry_RegisterCatalogCondition(t *testing.T) {
	reg := promotion.NewEvaluatorRegistry()
	if err := reg.RegisterCatalogCondition("custom_cond", "acme.demo", func(_ context.Context, _ []byte, _ *domainpricing.PricingItem) (bool, error) {
		return true, nil
	}); err != nil {
		t.Fatalf("RegisterCatalogCondition: %v", err)
	}
	if !reg.HasCatalogCondition("custom_cond") {
		t.Fatal("expected catalog condition to be registered")
	}
	if err := reg.RegisterCatalogCondition("custom_cond", "other", func(_ context.Context, _ []byte, _ *domainpricing.PricingItem) (bool, error) {
		return false, nil
	}); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}

func TestEvaluatorRegistry_EvalCatalogCondition(t *testing.T) {
	reg := promotion.NewEvaluatorRegistry()
	item := &domainpricing.PricingItem{Quantity: 3}
	if err := reg.RegisterCatalogCondition("qty_at_least", "acme.demo", func(_ context.Context, config []byte, item *domainpricing.PricingItem) (bool, error) {
		ruleType, err := promotion.RuleTypeFromConfig(config)
		if err != nil {
			return false, err
		}
		if ruleType != "qty_at_least" {
			return false, nil
		}
		return item.Quantity >= 2, nil
	}); err != nil {
		t.Fatalf("RegisterCatalogCondition: %v", err)
	}
	ok, err := reg.EvalCatalogCondition(context.Background(), "qty_at_least", []byte(`{"type":"qty_at_least"}`), item)
	if err != nil {
		t.Fatalf("EvalCatalogCondition: %v", err)
	}
	if !ok {
		t.Fatal("expected condition to match")
	}
}

func TestEvaluatorRegistry_EvalCatalogAction(t *testing.T) {
	reg := promotion.NewEvaluatorRegistry()
	if err := reg.RegisterCatalogAction("flat_bonus", "acme.demo", func(_ context.Context, _ []byte, _ *domainpricing.PricingItem, currency string) (shared.Money, error) {
		return shared.NewMoney(250, currency)
	}); err != nil {
		t.Fatalf("RegisterCatalogAction: %v", err)
	}
	got, err := reg.EvalCatalogAction(context.Background(), "flat_bonus", []byte(`{"type":"flat_bonus"}`), &domainpricing.PricingItem{}, "USD")
	if err != nil {
		t.Fatalf("EvalCatalogAction: %v", err)
	}
	if got.Amount() != 250 || got.Currency() != "USD" {
		t.Fatalf("discount = %+v", got)
	}
}

func TestRuleTypeFromConfig(t *testing.T) {
	ruleType, err := promotion.RuleTypeFromConfig([]byte(`{"type":"min_line_total","value":1000}`))
	if err != nil {
		t.Fatalf("RuleTypeFromConfig: %v", err)
	}
	if ruleType != "min_line_total" {
		t.Fatalf("type = %q", ruleType)
	}
	if _, err := promotion.RuleTypeFromConfig([]byte(`{"value":1}`)); err == nil {
		t.Fatal("expected error for missing type")
	}
}
