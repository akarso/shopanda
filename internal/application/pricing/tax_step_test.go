package pricing_test

import (
	"context"
	"errors"
	"testing"

	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/tax"
)

// mockRateRepo is a test double for tax.RateRepository.
type mockRateRepo struct {
	rates map[string]*tax.TaxRate // key: "country:class"
	err   error
}

func (m *mockRateRepo) FindByCountryClassAndStore(_ context.Context, country, class, _ string) (*tax.TaxRate, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.rates[country+":"+class], nil
}
func (m *mockRateRepo) ListByCountry(_ context.Context, _ string) ([]tax.TaxRate, error) {
	return nil, nil
}
func (m *mockRateRepo) Upsert(_ context.Context, _ *tax.TaxRate) error { return nil }
func (m *mockRateRepo) Delete(_ context.Context, _ string) error       { return nil }

func taxPricingContext(t *testing.T, currency, country, mode string, items ...pricing.PricingItem) pricing.PricingContext {
	t.Helper()
	pctx, err := pricing.NewPricingContext(currency)
	if err != nil {
		t.Fatal(err)
	}
	pctx.Items = items
	pctx.Meta["tax_country"] = country
	pctx.Meta["tax_mode"] = mode
	return pctx
}

func TestTaxStep_Name(t *testing.T) {
	step := appPricing.NewTaxStep(&mockRateRepo{}, "standard")
	if step.Name() != "tax" {
		t.Errorf("Name() = %q, want %q", step.Name(), "tax")
	}
}

func TestTaxStep_Exclusive(t *testing.T) {
	repo := &mockRateRepo{rates: map[string]*tax.TaxRate{
		"DE:standard": {ID: "r1", Country: "DE", Class: "standard", Rate: 1900},
	}}
	step := appPricing.NewTaxStep(repo, "standard")

	item, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(10000, "EUR"))
	pctx := taxPricingContext(t, "EUR", "DE", "exclusive", item)

	if err := step.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(pctx.Items[0].Adjustments) != 1 {
		t.Fatalf("adjustments = %d, want 1", len(pctx.Items[0].Adjustments))
	}

	adj := pctx.Items[0].Adjustments[0]
	// 10000 * 1900 / 10000 = 1900
	if adj.Amount.Amount() != 1900 {
		t.Errorf("tax amount = %d, want 1900", adj.Amount.Amount())
	}
	if adj.Included {
		t.Error("expected Included=false for exclusive")
	}
	if adj.Type != pricing.AdjustmentTax {
		t.Errorf("type = %q, want %q", adj.Type, pricing.AdjustmentTax)
	}
	if adj.Code != "tax.DE.standard" {
		t.Errorf("code = %q, want %q", adj.Code, "tax.DE.standard")
	}
	// Item total unchanged in exclusive mode.
	if pctx.Items[0].Total.Amount() != 10000 {
		t.Errorf("item total = %d, want 10000", pctx.Items[0].Total.Amount())
	}
}

func TestTaxStep_Inclusive(t *testing.T) {
	repo := &mockRateRepo{rates: map[string]*tax.TaxRate{
		"DE:standard": {ID: "r1", Country: "DE", Class: "standard", Rate: 1900},
	}}
	step := appPricing.NewTaxStep(repo, "standard")

	// 11900 = net 10000 + 19% tax 1900.
	item, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(11900, "EUR"))
	pctx := taxPricingContext(t, "EUR", "DE", "inclusive", item)

	if err := step.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	adj := pctx.Items[0].Adjustments[0]
	// net = 11900 * 10000 / 11900 = 10000, tax = 11900 - 10000 = 1900
	if adj.Amount.Amount() != 1900 {
		t.Errorf("tax amount = %d, want 1900", adj.Amount.Amount())
	}
	if !adj.Included {
		t.Error("expected Included=true for inclusive")
	}
	// Item total reduced to net.
	if pctx.Items[0].Total.Amount() != 10000 {
		t.Errorf("item total = %d, want 10000 (net)", pctx.Items[0].Total.Amount())
	}
}

func TestTaxStep_MultipleItems(t *testing.T) {
	repo := &mockRateRepo{rates: map[string]*tax.TaxRate{
		"DE:standard": {ID: "r1", Country: "DE", Class: "standard", Rate: 1900},
	}}
	step := appPricing.NewTaxStep(repo, "standard")

	item1, _ := pricing.NewPricingItem("v1", 2, shared.MustNewMoney(5000, "EUR"))
	item2, _ := pricing.NewPricingItem("v2", 1, shared.MustNewMoney(2000, "EUR"))
	pctx := taxPricingContext(t, "EUR", "DE", "exclusive", item1, item2)

	if err := step.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// item1: total = 10000, tax = 10000 * 1900 / 10000 = 1900
	if got := pctx.Items[0].Adjustments[0].Amount.Amount(); got != 1900 {
		t.Errorf("item1 tax = %d, want 1900", got)
	}
	// item2: total = 2000, tax = 2000 * 1900 / 10000 = 380
	if got := pctx.Items[1].Adjustments[0].Amount.Amount(); got != 380 {
		t.Errorf("item2 tax = %d, want 380", got)
	}
}

func TestTaxStep_ZeroRate(t *testing.T) {
	repo := &mockRateRepo{rates: map[string]*tax.TaxRate{
		"DE:zero": {ID: "r1", Country: "DE", Class: "zero", Rate: 0},
	}}
	step := appPricing.NewTaxStep(repo, "zero")

	item, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(10000, "EUR"))
	pctx := taxPricingContext(t, "EUR", "DE", "exclusive", item)

	if err := step.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(pctx.Items[0].Adjustments) != 0 {
		t.Errorf("adjustments = %d, want 0 for zero rate", len(pctx.Items[0].Adjustments))
	}
}

func TestTaxStep_NoRate(t *testing.T) {
	repo := &mockRateRepo{rates: map[string]*tax.TaxRate{}}
	step := appPricing.NewTaxStep(repo, "standard")

	item, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(10000, "EUR"))
	pctx := taxPricingContext(t, "EUR", "DE", "exclusive", item)

	if err := step.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(pctx.Items[0].Adjustments) != 0 {
		t.Errorf("adjustments = %d, want 0 when no rate found", len(pctx.Items[0].Adjustments))
	}
}

func TestTaxStep_PerVariantClassOverride(t *testing.T) {
	repo := &mockRateRepo{rates: map[string]*tax.TaxRate{
		"DE:standard": {ID: "r1", Country: "DE", Class: "standard", Rate: 1900},
		"DE:reduced":  {ID: "r2", Country: "DE", Class: "reduced", Rate: 700},
	}}
	step := appPricing.NewTaxStep(repo, "standard")

	item1, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(10000, "EUR"))
	item2, _ := pricing.NewPricingItem("v2", 1, shared.MustNewMoney(10000, "EUR"))
	pctx := taxPricingContext(t, "EUR", "DE", "exclusive", item1, item2)
	pctx.Meta["tax_classes"] = map[string]string{"v2": "reduced"}

	if err := step.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// v1 uses default "standard" at 19%: 10000 * 1900 / 10000 = 1900
	if got := pctx.Items[0].Adjustments[0].Amount.Amount(); got != 1900 {
		t.Errorf("v1 tax = %d, want 1900", got)
	}
	if pctx.Items[0].Adjustments[0].Code != "tax.DE.standard" {
		t.Errorf("v1 code = %q, want %q", pctx.Items[0].Adjustments[0].Code, "tax.DE.standard")
	}
	// v2 overridden to "reduced" at 7%: 10000 * 700 / 10000 = 700
	if got := pctx.Items[1].Adjustments[0].Amount.Amount(); got != 700 {
		t.Errorf("v2 tax = %d, want 700", got)
	}
	if pctx.Items[1].Adjustments[0].Code != "tax.DE.reduced" {
		t.Errorf("v2 code = %q, want %q", pctx.Items[1].Adjustments[0].Code, "tax.DE.reduced")
	}
}

func TestTaxStep_MissingCountryMeta(t *testing.T) {
	step := appPricing.NewTaxStep(&mockRateRepo{}, "standard")

	item, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(100, "EUR"))
	pctx := taxPricingContext(t, "EUR", "DE", "exclusive", item)
	delete(pctx.Meta, "tax_country")

	err := step.Apply(context.Background(), &pctx)
	if err == nil {
		t.Fatal("expected error for missing tax_country")
	}
}

func TestTaxStep_MissingModeMeta(t *testing.T) {
	step := appPricing.NewTaxStep(&mockRateRepo{}, "standard")

	item, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(100, "EUR"))
	pctx := taxPricingContext(t, "EUR", "DE", "exclusive", item)
	delete(pctx.Meta, "tax_mode")

	err := step.Apply(context.Background(), &pctx)
	if err == nil {
		t.Fatal("expected error for missing tax_mode")
	}
}

func TestTaxStep_InvalidMode(t *testing.T) {
	step := appPricing.NewTaxStep(&mockRateRepo{}, "standard")

	item, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(100, "EUR"))
	pctx := taxPricingContext(t, "EUR", "DE", "bogus", item)

	err := step.Apply(context.Background(), &pctx)
	if err == nil {
		t.Fatal("expected error for invalid tax_mode")
	}
}

func TestTaxStep_RepoError(t *testing.T) {
	repo := &mockRateRepo{err: errors.New("db down")}
	step := appPricing.NewTaxStep(repo, "standard")

	item, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(100, "EUR"))
	pctx := taxPricingContext(t, "EUR", "DE", "exclusive", item)

	err := step.Apply(context.Background(), &pctx)
	if err == nil {
		t.Fatal("expected error from repo")
	}
}

func TestTaxStep_FullPipeline_Exclusive(t *testing.T) {
	repo := &mockRateRepo{rates: map[string]*tax.TaxRate{
		"DE:standard": {ID: "r1", Country: "DE", Class: "standard", Rate: 2100},
	}}

	// Build full pipeline: base prices are already set, so we skip BasePriceStep
	// and just use TaxStep → FinalizeStep.
	taxStep := appPricing.NewTaxStep(repo, "standard")
	finalizeStep := pricing.NewFinalizeStep()
	pipeline := pricing.NewPipeline(taxStep, finalizeStep)

	pctx, _ := pricing.NewPricingContext("EUR")
	item, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(10000, "EUR"))
	pctx.Items = []pricing.PricingItem{item}
	pctx.Meta["tax_country"] = "DE"
	pctx.Meta["tax_mode"] = "exclusive"

	if err := pipeline.Execute(context.Background(), &pctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Tax = 10000 * 2100 / 10000 = 2100
	if pctx.TaxTotal.Amount() != 2100 {
		t.Errorf("TaxTotal = %d, want 2100", pctx.TaxTotal.Amount())
	}
	// Subtotal = 10000 (unchanged in exclusive)
	if pctx.Subtotal.Amount() != 10000 {
		t.Errorf("Subtotal = %d, want 10000", pctx.Subtotal.Amount())
	}
	// GrandTotal = 10000 + 2100 = 12100
	if pctx.GrandTotal.Amount() != 12100 {
		t.Errorf("GrandTotal = %d, want 12100", pctx.GrandTotal.Amount())
	}
}

func TestTaxStep_FullPipeline_Inclusive(t *testing.T) {
	repo := &mockRateRepo{rates: map[string]*tax.TaxRate{
		"DE:standard": {ID: "r1", Country: "DE", Class: "standard", Rate: 2100},
	}}

	taxStep := appPricing.NewTaxStep(repo, "standard")
	finalizeStep := pricing.NewFinalizeStep()
	pipeline := pricing.NewPipeline(taxStep, finalizeStep)

	// Item priced at 12100 (inclusive of 21% tax).
	// net = 12100 * 10000 / 12100 = 10000, tax = 2100
	pctx, _ := pricing.NewPricingContext("EUR")
	item, _ := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(12100, "EUR"))
	pctx.Items = []pricing.PricingItem{item}
	pctx.Meta["tax_country"] = "DE"
	pctx.Meta["tax_mode"] = "inclusive"

	if err := pipeline.Execute(context.Background(), &pctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if pctx.TaxTotal.Amount() != 2100 {
		t.Errorf("TaxTotal = %d, want 2100", pctx.TaxTotal.Amount())
	}
	// Subtotal = net = 10000 (item.Total adjusted by tax step)
	if pctx.Subtotal.Amount() != 10000 {
		t.Errorf("Subtotal = %d, want 10000", pctx.Subtotal.Amount())
	}
	// GrandTotal = 10000 + 2100 = 12100 (same as original price)
	if pctx.GrandTotal.Amount() != 12100 {
		t.Errorf("GrandTotal = %d, want 12100", pctx.GrandTotal.Amount())
	}
}

func TestTaxStep_Inclusive_Quantity(t *testing.T) {
	repo := &mockRateRepo{rates: map[string]*tax.TaxRate{
		"DE:standard": {ID: "r1", Country: "DE", Class: "standard", Rate: 1900},
	}}
	step := appPricing.NewTaxStep(repo, "standard")

	// 2 items at 1190 each (inclusive), total = 2380
	// net total = 2380 * 10000 / 11900 = 2000, tax = 380
	item, _ := pricing.NewPricingItem("v1", 2, shared.MustNewMoney(1190, "EUR"))
	pctx := taxPricingContext(t, "EUR", "DE", "inclusive", item)

	if err := step.Apply(context.Background(), &pctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	adj := pctx.Items[0].Adjustments[0]
	if adj.Amount.Amount() != 380 {
		t.Errorf("tax = %d, want 380", adj.Amount.Amount())
	}
	if pctx.Items[0].Total.Amount() != 2000 {
		t.Errorf("net total = %d, want 2000", pctx.Items[0].Total.Amount())
	}
}
