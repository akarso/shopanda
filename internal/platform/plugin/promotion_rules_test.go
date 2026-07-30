package plugin_test

import (
	"context"
	"io"
	"testing"

	domainpricing "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
)

func TestApp_PromotionRules_RegisterCatalogCondition(t *testing.T) {
	reg := promotion.NewEvaluatorRegistry()
	app := &plugin.App{Logger: logger.NewWithWriter(io.Discard, "error")}
	app.SetPromotionEvaluatorRegistry(reg)

	if err := app.PromotionRules("acme.demo").RegisterCatalogCondition("line_over", func(_ context.Context, _ []byte, item *extapi.PromotionPricingItem) (bool, error) {
		return item.TotalAmount >= 5000, nil
	}); err != nil {
		t.Fatalf("RegisterCatalogCondition: %v", err)
	}

	item, err := domainpricing.NewPricingItem("v1", 1, shared.MustNewMoney(6000, "USD"))
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	ok, err := reg.EvalCatalogCondition(context.Background(), "line_over", []byte(`{"type":"line_over"}`), &item)
	if err != nil {
		t.Fatalf("EvalCatalogCondition: %v", err)
	}
	if !ok {
		t.Fatal("expected condition to match")
	}
}

func TestApp_PromotionRules_RegisterCatalogAction(t *testing.T) {
	reg := promotion.NewEvaluatorRegistry()
	app := &plugin.App{Logger: logger.NewWithWriter(io.Discard, "error")}
	app.SetPromotionEvaluatorRegistry(reg)

	if err := app.PromotionRules("acme.demo").RegisterCatalogAction("bonus", func(_ context.Context, _ []byte, item *extapi.PromotionPricingItem) (int64, error) {
		return item.TotalAmount / 10, nil
	}); err != nil {
		t.Fatalf("RegisterCatalogAction: %v", err)
	}

	item, err := domainpricing.NewPricingItem("v1", 1, shared.MustNewMoney(1000, "USD"))
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	got, err := reg.EvalCatalogAction(context.Background(), "bonus", []byte(`{"type":"bonus"}`), &item, "USD")
	if err != nil {
		t.Fatalf("EvalCatalogAction: %v", err)
	}
	if got.Amount() != 100 {
		t.Fatalf("discount = %d", got.Amount())
	}
}

func TestApp_PromotionRules_RejectsNegativeDiscount(t *testing.T) {
	reg := promotion.NewEvaluatorRegistry()
	app := &plugin.App{Logger: logger.NewWithWriter(io.Discard, "error")}
	app.SetPromotionEvaluatorRegistry(reg)

	if err := app.PromotionRules("acme.demo").RegisterCatalogAction("bad", func(_ context.Context, _ []byte, _ *extapi.PromotionPricingItem) (int64, error) {
		return -1, nil
	}); err != nil {
		t.Fatalf("RegisterCatalogAction: %v", err)
	}

	item, err := domainpricing.NewPricingItem("v1", 1, shared.MustNewMoney(1000, "USD"))
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	if _, err := reg.EvalCatalogAction(context.Background(), "bad", []byte(`{"type":"bad"}`), &item, "USD"); err == nil {
		t.Fatal("expected negative discount error")
	}
}
