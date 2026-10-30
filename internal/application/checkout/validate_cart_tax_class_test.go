package checkout_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/application/checkout"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/tax"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func TestValidateCartStep_SetsTaxClassesByProductType(t *testing.T) {
	variants := &mockVariantRepo{variants: map[string]*catalog.Variant{
		"v-simple":       mustVariant(t, "v-simple", "prod-simple", "SKU-S"),
		"v-virtual":      mustVariant(t, "v-virtual", "prod-virtual", "SKU-V"),
		"v-downloadable": mustVariant(t, "v-downloadable", "prod-downloadable", "SKU-D"),
		"v-bundle":       mustVariant(t, "v-bundle", "prod-bundle", "SKU-B"),
	}}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-simple": {
			ID: "prod-simple", Status: catalog.StatusActive, Type: catalog.TypeSimple,
		},
		"prod-virtual": {
			ID: "prod-virtual", Status: catalog.StatusActive, Type: catalog.TypeVirtual,
		},
		"prod-downloadable": {
			ID: "prod-downloadable", Status: catalog.StatusActive, Type: catalog.TypeDownloadable,
		},
		"prod-bundle": {
			ID: "prod-bundle", Status: catalog.StatusActive, Type: catalog.TypeBundle,
		},
	}}
	step := newValidateCartStep(variants, products).
		WithDownloadableFiles(stubCheckoutDownloadableFileCount{n: 1})

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems(t, "cust-1", "v-simple", "v-virtual", "v-downloadable", "v-bundle")

	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw, ok := cctx.GetMeta("tax_classes")
	if !ok {
		t.Fatal("expected tax_classes meta")
	}
	classes, ok := raw.(map[string]string)
	if !ok {
		t.Fatalf("tax_classes type = %T", raw)
	}
	want := map[string]string{
		"v-simple":       tax.TaxClassStandard,
		"v-virtual":      tax.TaxClassStandard,
		"v-downloadable": tax.TaxClassDigital,
		"v-bundle":       tax.TaxClassStandard, // parent stays standard even if leaves are downloadable
	}
	for vid, class := range want {
		if got := classes[vid]; got != class {
			t.Errorf("tax_classes[%q] = %q, want %q", vid, got, class)
		}
	}
}

func TestValidateCartStep_TaxClassesPreservesExistingKeys(t *testing.T) {
	variants := &mockVariantRepo{variants: map[string]*catalog.Variant{
		"v1": mustVariant(t, "v1", "prod-1", "SKU-1"),
		"v2": mustVariant(t, "v2", "prod-2", "SKU-2"),
	}}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeSimple},
		"prod-2": {ID: "prod-2", Status: catalog.StatusActive, Type: catalog.TypeDownloadable},
	}}
	step := newValidateCartStep(variants, products).
		WithDownloadableFiles(stubCheckoutDownloadableFileCount{n: 1})

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems(t, "cust-1", "v1", "v2")
	cctx.SetMeta("tax_classes", map[string]string{"v1": tax.TaxClassReduced})

	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	classes := cctx.Meta["tax_classes"].(map[string]string)
	if classes["v1"] != tax.TaxClassReduced {
		t.Errorf("v1 = %q, want reduced (pre-set preserved)", classes["v1"])
	}
	if classes["v2"] != tax.TaxClassDigital {
		t.Errorf("v2 = %q, want digital (filled for missing)", classes["v2"])
	}
}

func TestValidateCartStep_TaxClassesRejectsWrongTypedMap(t *testing.T) {
	variants := &mockVariantRepo{variants: map[string]*catalog.Variant{
		"v1": mustVariant(t, "v1", "prod-1", "SKU-1"),
	}}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeSimple},
	}}
	step := newValidateCartStep(variants, products)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems(t, "cust-1", "v1")
	cctx.SetMeta("tax_classes", []string{"bogus"})

	err := step.Execute(context.Background(), cctx)
	if err == nil {
		t.Fatal("expected error for wrong-typed tax_classes")
	}
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("want validation error, got %v", err)
	}
}

func TestValidateThenRecalculate_AppliesDigitalRate(t *testing.T) {
	variants := &mockVariantRepo{variants: map[string]*catalog.Variant{
		"v-dig": mustVariant(t, "v-dig", "prod-dig", "SKU-D"),
	}}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-dig": {ID: "prod-dig", Status: catalog.StatusActive, Type: catalog.TypeDownloadable},
	}}
	validate := newValidateCartStep(variants, products).
		WithDownloadableFiles(stubCheckoutDownloadableFileCount{n: 1})

	repo := &checkoutTaxRateRepo{rates: map[string]*tax.TaxRate{
		"FR:digital:": {ID: "r1", Country: "FR", Class: tax.TaxClassDigital, Rate: 2000},
	}}
	pipeline := pricing.NewPipeline(
		appPricing.NewTaxStep(appPricing.NewRateTableTaxCalculator(repo, "standard")),
		pricing.NewFinalizeStep(),
	)
	recalc := checkout.NewRecalculatePricingStep(pipeline)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems(t, "cust-1", "v-dig")
	cctx.Cart.Items[0].UnitPrice = shared.MustNewMoney(10000, "EUR")
	cctx.SetMeta("tax_country", "FR")
	cctx.SetMeta("tax_mode", string(tax.ModeExclusive))

	if err := validate.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("validate: %v", err)
	}
	classes := cctx.Meta["tax_classes"].(map[string]string)
	if classes["v-dig"] != tax.TaxClassDigital {
		t.Fatalf("tax_classes[v-dig] = %q, want digital", classes["v-dig"])
	}

	if err := recalc.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("recalculate: %v", err)
	}
	raw, ok := cctx.GetMeta("pricing")
	if !ok {
		t.Fatal("expected pricing meta")
	}
	pctx := raw.(*pricing.PricingContext)
	if pctx.TaxTotal.Amount() != 2000 {
		t.Errorf("TaxTotal = %d, want 2000 (20%% digital FR)", pctx.TaxTotal.Amount())
	}
	if got := pctx.Items[0].Adjustments[0].Code; got != "tax.FR.digital" {
		t.Errorf("adjustment code = %q, want tax.FR.digital", got)
	}
}

func TestValidateThenRecalculate_DigitalMissingDestinationRate_FailsClosed(t *testing.T) {
	variants := &mockVariantRepo{variants: map[string]*catalog.Variant{
		"v-dig": mustVariant(t, "v-dig", "prod-dig", "SKU-D"),
	}}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-dig": {ID: "prod-dig", Status: catalog.StatusActive, Type: catalog.TypeDownloadable},
	}}
	validate := newValidateCartStep(variants, products).
		WithDownloadableFiles(stubCheckoutDownloadableFileCount{n: 1})

	repo := &checkoutTaxRateRepo{rates: map[string]*tax.TaxRate{
		"DE:digital:": {ID: "r1", Country: "DE", Class: tax.TaxClassDigital, Rate: 1900},
	}}
	pipeline := pricing.NewPipeline(
		appPricing.NewTaxStep(appPricing.NewRateTableTaxCalculator(repo, "standard")),
		pricing.NewFinalizeStep(),
	)
	recalc := checkout.NewRecalculatePricingStep(pipeline)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems(t, "cust-1", "v-dig")
	cctx.Cart.Items[0].UnitPrice = shared.MustNewMoney(10000, "EUR")
	cctx.SetMeta("tax_country", "FR")
	cctx.SetMeta("tax_mode", string(tax.ModeExclusive))

	if err := validate.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("validate: %v", err)
	}
	err := recalc.Execute(context.Background(), cctx)
	if err == nil {
		t.Fatal("expected recalculate to fail closed when FR digital rate missing")
	}
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("want wrapped validation error (not 0%% VAT), got %v", err)
	}
}

type checkoutTaxRateRepo struct {
	rates map[string]*tax.TaxRate
}

func (m *checkoutTaxRateRepo) FindByCountryClassAndStore(_ context.Context, country, class, storeID string) (*tax.TaxRate, error) {
	return m.rates[country+":"+class+":"+storeID], nil
}
func (m *checkoutTaxRateRepo) ListByCountry(_ context.Context, _ string) ([]tax.TaxRate, error) {
	return nil, nil
}
func (m *checkoutTaxRateRepo) ListByClass(_ context.Context, _ string) ([]tax.TaxRate, error) {
	return nil, nil
}
func (m *checkoutTaxRateRepo) Upsert(_ context.Context, _ *tax.TaxRate) error { return nil }
func (m *checkoutTaxRateRepo) CreateIfNotExists(_ context.Context, _ *tax.TaxRate) (bool, error) {
	return false, nil
}
func (m *checkoutTaxRateRepo) Delete(_ context.Context, _ string) error { return nil }

func mustVariant(t *testing.T, id, productID, sku string) *catalog.Variant {
	t.Helper()
	v, err := catalog.NewVariant(id, productID, sku)
	if err != nil {
		t.Fatalf("NewVariant: %v", err)
	}
	return &v
}
