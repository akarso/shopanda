package pluginsdk_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// capturingPricingStep records the PricingContext view it was given so the
// test can assert on the public surface after seeding domain state.
type capturingPricingStep struct {
	got extapi.PricingContext
}

func (s *capturingPricingStep) Name() string { return "capture" }

func (s *capturingPricingStep) Apply(_ context.Context, pctx extapi.PricingContext) error {
	s.got = pctx
	return nil
}

func TestPricingContextView_ItemsIncludeLineAdjustments(t *testing.T) {
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	item, err := pricing.NewPricingItem("v1", 2, shared.MustNewMoney(500, "EUR"))
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	lineAdj, err := pricing.NewAdjustment(pricing.AdjustmentTax, "vat", shared.MustNewMoney(100, "EUR"))
	if err != nil {
		t.Fatalf("NewAdjustment: %v", err)
	}
	item.Adjustments = []pricing.Adjustment{lineAdj}
	pctx.Items = []pricing.PricingItem{item}

	cartAdj, err := pricing.NewAdjustment(pricing.AdjustmentDiscount, "promo", shared.MustNewMoney(-50, "EUR"))
	if err != nil {
		t.Fatalf("NewAdjustment: %v", err)
	}
	pctx.Adjustments = []pricing.Adjustment{cartAdj}
	pctx.Subtotal = shared.MustNewMoney(1000, "EUR")
	pctx.DiscountsTotal = shared.MustNewMoney(50, "EUR")
	pctx.TaxTotal = shared.MustNewMoney(100, "EUR")
	pctx.FeesTotal = shared.MustNewMoney(0, "EUR")
	pctx.GrandTotal = shared.MustNewMoney(1050, "EUR")

	step := &capturingPricingStep{}
	domainStep := pluginsdk.AdaptPricingStep(step)
	if err := domainStep.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	items := step.got.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d, want 1", len(items))
	}
	if len(items[0].Adjustments) != 1 || items[0].Adjustments[0].Code != "vat" {
		t.Fatalf("Items()[0].Adjustments = %+v, want [vat]", items[0].Adjustments)
	}
	if items[0].Adjustments[0].Amount.Amount() != 100 {
		t.Fatalf("line adjustment amount = %d, want 100", items[0].Adjustments[0].Amount.Amount())
	}

	cartAdjustments := step.got.Adjustments()
	if len(cartAdjustments) != 1 || cartAdjustments[0].Code != "promo" {
		t.Fatalf("Adjustments() = %+v, want [promo]", cartAdjustments)
	}

	if got := step.got.Subtotal().Amount(); got != 1000 {
		t.Errorf("Subtotal() = %d, want 1000", got)
	}
	if got := step.got.Discounts().Amount(); got != 50 {
		t.Errorf("Discounts() = %d, want 50", got)
	}
	if got := step.got.Tax().Amount(); got != 100 {
		t.Errorf("Tax() = %d, want 100", got)
	}
	if got := step.got.Fees().Amount(); got != 0 {
		t.Errorf("Fees() = %d, want 0", got)
	}
	if got := step.got.GrandTotal().Amount(); got != 1050 {
		t.Errorf("GrandTotal() = %d, want 1050", got)
	}
}
