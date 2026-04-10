package checkout_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"
)

// ============================================================
// Mock variant repository
// ============================================================

type mockVariantRepo struct {
	variants map[string]*catalog.Variant // keyed by ID
	err      error
}

func (r *mockVariantRepo) FindByID(_ context.Context, vid string) (*catalog.Variant, error) {
	if r.err != nil {
		return nil, r.err
	}
	v, ok := r.variants[vid]
	if !ok {
		return nil, nil
	}
	return v, nil
}

// unused — satisfy interface
func (r *mockVariantRepo) FindBySKU(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (r *mockVariantRepo) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return nil, nil
}
func (r *mockVariantRepo) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (r *mockVariantRepo) Update(_ context.Context, _ *catalog.Variant) error { return nil }
func (r *mockVariantRepo) WithTx(_ *sql.Tx) catalog.VariantRepository         { return r }

// ============================================================
// Mock price repository (for pipeline)
// ============================================================

type mockPriceRepo036 struct {
	prices map[string]*pricing.Price // keyed by variantID
	err    error
}

func (r *mockPriceRepo036) FindByVariantAndCurrency(_ context.Context, variantID, _ string) (*pricing.Price, error) {
	if r.err != nil {
		return nil, r.err
	}
	p, ok := r.prices[variantID]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (r *mockPriceRepo036) ListByVariantID(_ context.Context, _ string) ([]pricing.Price, error) {
	return nil, nil
}
func (r *mockPriceRepo036) List(_ context.Context, _, _ int) ([]pricing.Price, error) {
	return nil, nil
}
func (r *mockPriceRepo036) Upsert(_ context.Context, _ *pricing.Price) error { return nil }

// ============================================================
// Helpers
// ============================================================

func cartWithItems(t *testing.T, customerID string, variantIDs ...string) *cart.Cart {
	t.Helper()
	c, err := cart.NewCart(id.New(), "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID(customerID); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	for _, vid := range variantIDs {
		price := shared.MustNewMoney(1000, "EUR")
		if err := c.AddItem(vid, 1, price); err != nil {
			t.Fatalf("AddItem(%s): %v", vid, err)
		}
	}
	return &c
}

func variantMap(ids ...string) map[string]*catalog.Variant {
	m := make(map[string]*catalog.Variant, len(ids))
	for _, vid := range ids {
		v, _ := catalog.NewVariant(vid, "prod-1", fmt.Sprintf("SKU-%s", vid))
		m[vid] = &v
	}
	return m
}

// ============================================================
// ValidateCartStep tests
// ============================================================

func TestValidateCartStep_Name(t *testing.T) {
	step := checkout.NewValidateCartStep(&mockVariantRepo{})
	if step.Name() != "validate_cart" {
		t.Errorf("Name() = %q, want validate_cart", step.Name())
	}
}

func TestValidateCartStep_Success(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1", "v2")}
	step := checkout.NewValidateCartStep(repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems(t, "cust-1", "v1", "v2")

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if v, ok := cctx.GetMeta("validated"); !ok || v != true {
		t.Error("expected validated=true in meta")
	}
}

func TestValidateCartStep_MissingVariant(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")} // v2 missing
	step := checkout.NewValidateCartStep(repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems(t, "cust-1", "v1", "v2")

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error for missing variant")
	}
	if _, ok := cctx.GetMeta("validated"); ok {
		t.Error("validated meta should not be set on error")
	}
}

func TestValidateCartStep_RepoError(t *testing.T) {
	repo := &mockVariantRepo{err: errors.New("db down")}
	step := checkout.NewValidateCartStep(repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems(t, "cust-1", "v1")

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error from repo")
	}
	if _, ok := cctx.GetMeta("validated"); ok {
		t.Error("validated meta should not be set on error")
	}
}

func TestValidateCartStep_NilCart(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")}
	step := checkout.NewValidateCartStep(repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	// Cart not set

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error for nil cart")
	}
}

func TestValidateCartStep_Idempotent(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap("v1")}
	step := checkout.NewValidateCartStep(repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems(t, "cust-1", "v1")

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("first Execute: %v", err)
	}

	// Make repo fail — second call should skip via meta
	repo.err = errors.New("should not be called")
	if err := step.Execute(cctx); err != nil {
		t.Fatalf("second Execute should be idempotent: %v", err)
	}
}

func TestValidateCartStep_EmptyCart(t *testing.T) {
	repo := &mockVariantRepo{variants: variantMap()}
	step := checkout.NewValidateCartStep(repo)

	c, err := cart.NewCart(id.New(), "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = &c

	// Empty cart has no items to validate — should succeed and mark validated
	if err := step.Execute(cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if v, ok := cctx.GetMeta("validated"); !ok || v != true {
		t.Error("expected validated=true")
	}
}

// ============================================================
// RecalculatePricingStep tests
// ============================================================

func TestRecalculatePricingStep_Name(t *testing.T) {
	pipeline := pricing.NewPipeline()
	step := checkout.NewRecalculatePricingStep(pipeline)
	if step.Name() != "recalculate_pricing" {
		t.Errorf("Name() = %q, want recalculate_pricing", step.Name())
	}
}

func TestRecalculatePricingStep_Success(t *testing.T) {
	// Use finalize step to compute totals
	pipeline := pricing.NewPipeline(pricing.NewFinalizeStep())
	step := checkout.NewRecalculatePricingStep(pipeline)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems(t, "cust-1", "v1", "v2")

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	raw, ok := cctx.GetMeta("pricing")
	if !ok {
		t.Fatal("expected pricing in meta")
	}
	pctx, ok := raw.(*pricing.PricingContext)
	if !ok {
		t.Fatalf("pricing meta is %T, want *pricing.PricingContext", raw)
	}
	if len(pctx.Items) != 2 {
		t.Errorf("Items len = %d, want 2", len(pctx.Items))
	}
	// Each item: qty=1, unitPrice=1000 EUR → total=1000; subtotal=2000
	if pctx.Subtotal.Amount() != 2000 {
		t.Errorf("Subtotal = %d, want 2000", pctx.Subtotal.Amount())
	}
	if pctx.GrandTotal.Amount() != 2000 {
		t.Errorf("GrandTotal = %d, want 2000", pctx.GrandTotal.Amount())
	}
	if v, ok := cctx.GetMeta("priced"); !ok || v != true {
		t.Error("expected priced=true in meta")
	}
}

func TestRecalculatePricingStep_NilCart(t *testing.T) {
	pipeline := pricing.NewPipeline()
	step := checkout.NewRecalculatePricingStep(pipeline)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error for nil cart")
	}
}

func TestRecalculatePricingStep_Idempotent(t *testing.T) {
	pipeline := pricing.NewPipeline(pricing.NewFinalizeStep())
	step := checkout.NewRecalculatePricingStep(pipeline)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems(t, "cust-1", "v1")

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("first Execute: %v", err)
	}

	raw1, _ := cctx.GetMeta("pricing")
	pctx1 := raw1.(*pricing.PricingContext)
	grand1 := pctx1.GrandTotal.Amount()

	// Second call should skip due to meta
	if err := step.Execute(cctx); err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	raw2, _ := cctx.GetMeta("pricing")
	pctx2 := raw2.(*pricing.PricingContext)
	if pctx2.GrandTotal.Amount() != grand1 {
		t.Errorf("GrandTotal changed on second call: %d vs %d", pctx2.GrandTotal.Amount(), grand1)
	}
}

func TestRecalculatePricingStep_PipelineError(t *testing.T) {
	// Use a pipeline with a step that fails
	failStep := &failPricingStep{}
	pipeline := pricing.NewPipeline(failStep)
	step := checkout.NewRecalculatePricingStep(pipeline)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems(t, "cust-1", "v1")

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error from pipeline")
	}
	if _, ok := cctx.GetMeta("priced"); ok {
		t.Error("priced meta should not be set on error")
	}
	if _, ok := cctx.GetMeta("pricing"); ok {
		t.Error("pricing meta should not be set on error")
	}
}

// failPricingStep is a pricing step that always fails.
type failPricingStep struct{}

func (s *failPricingStep) Name() string { return "fail" }
func (s *failPricingStep) Apply(_ context.Context, _ *pricing.PricingContext) error {
	return errors.New("pricing boom")
}
