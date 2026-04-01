package pricing_test

import (
	"context"
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
	pctx, _ := pricing.NewPricingContext("EUR")
	item1, _ := pricing.NewPricingItem("v1", 2, shared.MustNewMoney(500, "EUR"))
	item2, _ := pricing.NewPricingItem("v2", 1, shared.MustNewMoney(300, "EUR"))
	pctx.Items = []pricing.PricingItem{item1, item2}

	s := pricing.NewFinalizeStep()
	if err := s.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if pctx.Subtotal.Amount() != 1300 {
		t.Errorf("Subtotal = %d, want 1300", pctx.Subtotal.Amount())
	}
	if pctx.GrandTotal.Amount() != 1300 {
		t.Errorf("GrandTotal = %d, want 1300", pctx.GrandTotal.Amount())
	}
}

func TestFinalizeStep_EmptyItems(t *testing.T) {
	pctx, _ := pricing.NewPricingContext("EUR")
	s := pricing.NewFinalizeStep()
	if err := s.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if pctx.Subtotal.Amount() != 0 {
		t.Errorf("Subtotal = %d, want 0", pctx.Subtotal.Amount())
	}
	if pctx.GrandTotal.Amount() != 0 {
		t.Errorf("GrandTotal = %d, want 0", pctx.GrandTotal.Amount())
	}
}

func TestFinalizeStep_WithContextAdjustments(t *testing.T) {
	pctx, _ := pricing.NewPricingContext("EUR")
	item, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(1000, "EUR"))
	pctx.Items = []pricing.PricingItem{item}

	discount, _ := pricing.NewAdjustment(pricing.AdjustmentDiscount, "PROMO", shared.MustNewMoney(100, "EUR"))
	tax, _ := pricing.NewAdjustment(pricing.AdjustmentTax, "VAT", shared.MustNewMoney(180, "EUR"))
	fee, _ := pricing.NewAdjustment(pricing.AdjustmentFee, "SHIP", shared.MustNewMoney(50, "EUR"))
	pctx.Adjustments = []pricing.Adjustment{discount, tax, fee}

	s := pricing.NewFinalizeStep()
	if err := s.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if pctx.Subtotal.Amount() != 1000 {
		t.Errorf("Subtotal = %d, want 1000", pctx.Subtotal.Amount())
	}
	if pctx.DiscountsTotal.Amount() != 100 {
		t.Errorf("DiscountsTotal = %d, want 100", pctx.DiscountsTotal.Amount())
	}
	if pctx.TaxTotal.Amount() != 180 {
		t.Errorf("TaxTotal = %d, want 180", pctx.TaxTotal.Amount())
	}
	if pctx.FeesTotal.Amount() != 50 {
		t.Errorf("FeesTotal = %d, want 50", pctx.FeesTotal.Amount())
	}
	// GrandTotal = 1000 - 100 + 180 + 50 = 1130
	if pctx.GrandTotal.Amount() != 1130 {
		t.Errorf("GrandTotal = %d, want 1130", pctx.GrandTotal.Amount())
	}
}

func TestFinalizeStep_WithItemAdjustments(t *testing.T) {
	pctx, _ := pricing.NewPricingContext("EUR")
	item, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(1000, "EUR"))
	discount, _ := pricing.NewAdjustment(pricing.AdjustmentDiscount, "ITEM10", shared.MustNewMoney(100, "EUR"))
	item.Adjustments = []pricing.Adjustment{discount}
	pctx.Items = []pricing.PricingItem{item}

	s := pricing.NewFinalizeStep()
	if err := s.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if pctx.DiscountsTotal.Amount() != 100 {
		t.Errorf("DiscountsTotal = %d, want 100", pctx.DiscountsTotal.Amount())
	}
	// GrandTotal = 1000 - 100 = 900
	if pctx.GrandTotal.Amount() != 900 {
		t.Errorf("GrandTotal = %d, want 900", pctx.GrandTotal.Amount())
	}
}

func TestFinalizeStep_Idempotent(t *testing.T) {
	pctx, _ := pricing.NewPricingContext("EUR")
	item, _ := pricing.NewPricingItem("v1", 2, shared.MustNewMoney(500, "EUR"))
	discount, _ := pricing.NewAdjustment(pricing.AdjustmentDiscount, "PROMO", shared.MustNewMoney(50, "EUR"))
	pctx.Items = []pricing.PricingItem{item}
	pctx.Adjustments = []pricing.Adjustment{discount}

	s := pricing.NewFinalizeStep()

	// Apply twice — totals must be identical.
	if err := s.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if err := s.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if pctx.Subtotal.Amount() != 1000 {
		t.Errorf("Subtotal = %d, want 1000", pctx.Subtotal.Amount())
	}
	if pctx.DiscountsTotal.Amount() != 50 {
		t.Errorf("DiscountsTotal = %d, want 50", pctx.DiscountsTotal.Amount())
	}
	// GrandTotal = 1000 - 50 = 950
	if pctx.GrandTotal.Amount() != 950 {
		t.Errorf("GrandTotal = %d, want 950", pctx.GrandTotal.Amount())
	}
}
