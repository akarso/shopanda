package pricing_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
)

func TestFinalizeStep_Name(t *testing.T) {
	s := pricing.NewFinalizeStep()
	if s.Name() != "finalize" {
		t.Errorf("Name() = %q, want %q", s.Name(), "finalize")
	}
}

func TestFinalizeStep_SubtotalFromItems(t *testing.T) {
	ctx, _ := pricing.NewPricingContext("EUR")
	item1, _ := pricing.NewPricingItem("v1", 2, shared.MustNewMoney(500, "EUR"))
	item2, _ := pricing.NewPricingItem("v2", 1, shared.MustNewMoney(300, "EUR"))
	ctx.Items = []pricing.PricingItem{item1, item2}

	s := pricing.NewFinalizeStep()
	if err := s.Apply(&ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if ctx.Subtotal.Amount() != 1300 {
		t.Errorf("Subtotal = %d, want 1300", ctx.Subtotal.Amount())
	}
	if ctx.GrandTotal.Amount() != 1300 {
		t.Errorf("GrandTotal = %d, want 1300", ctx.GrandTotal.Amount())
	}
}

func TestFinalizeStep_EmptyItems(t *testing.T) {
	ctx, _ := pricing.NewPricingContext("EUR")
	s := pricing.NewFinalizeStep()
	if err := s.Apply(&ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if ctx.Subtotal.Amount() != 0 {
		t.Errorf("Subtotal = %d, want 0", ctx.Subtotal.Amount())
	}
	if ctx.GrandTotal.Amount() != 0 {
		t.Errorf("GrandTotal = %d, want 0", ctx.GrandTotal.Amount())
	}
}

func TestFinalizeStep_WithContextAdjustments(t *testing.T) {
	ctx, _ := pricing.NewPricingContext("EUR")
	item, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(1000, "EUR"))
	ctx.Items = []pricing.PricingItem{item}

	discount, _ := pricing.NewAdjustment(pricing.AdjustmentDiscount, "PROMO", shared.MustNewMoney(100, "EUR"))
	tax, _ := pricing.NewAdjustment(pricing.AdjustmentTax, "VAT", shared.MustNewMoney(180, "EUR"))
	fee, _ := pricing.NewAdjustment(pricing.AdjustmentFee, "SHIP", shared.MustNewMoney(50, "EUR"))
	ctx.Adjustments = []pricing.Adjustment{discount, tax, fee}

	s := pricing.NewFinalizeStep()
	if err := s.Apply(&ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if ctx.Subtotal.Amount() != 1000 {
		t.Errorf("Subtotal = %d, want 1000", ctx.Subtotal.Amount())
	}
	if ctx.DiscountsTotal.Amount() != 100 {
		t.Errorf("DiscountsTotal = %d, want 100", ctx.DiscountsTotal.Amount())
	}
	if ctx.TaxTotal.Amount() != 180 {
		t.Errorf("TaxTotal = %d, want 180", ctx.TaxTotal.Amount())
	}
	if ctx.FeesTotal.Amount() != 50 {
		t.Errorf("FeesTotal = %d, want 50", ctx.FeesTotal.Amount())
	}
	// GrandTotal = 1000 - 100 + 180 + 50 = 1130
	if ctx.GrandTotal.Amount() != 1130 {
		t.Errorf("GrandTotal = %d, want 1130", ctx.GrandTotal.Amount())
	}
}

func TestFinalizeStep_WithItemAdjustments(t *testing.T) {
	ctx, _ := pricing.NewPricingContext("EUR")
	item, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(1000, "EUR"))
	discount, _ := pricing.NewAdjustment(pricing.AdjustmentDiscount, "ITEM10", shared.MustNewMoney(100, "EUR"))
	item.Adjustments = []pricing.Adjustment{discount}
	ctx.Items = []pricing.PricingItem{item}

	s := pricing.NewFinalizeStep()
	if err := s.Apply(&ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if ctx.DiscountsTotal.Amount() != 100 {
		t.Errorf("DiscountsTotal = %d, want 100", ctx.DiscountsTotal.Amount())
	}
	// GrandTotal = 1000 - 100 = 900
	if ctx.GrandTotal.Amount() != 900 {
		t.Errorf("GrandTotal = %d, want 900", ctx.GrandTotal.Amount())
	}
}
