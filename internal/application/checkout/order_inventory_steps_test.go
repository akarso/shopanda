package checkout_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"
)

// ============================================================
// Mock reservation repository
// ============================================================

type mockReservationRepo struct {
	reserved   []inventory.Reservation
	released   []string
	err        error
	failAfterN int // if > 0, succeed for N calls then fail
	callCount  int
}

func (r *mockReservationRepo) Reserve(_ context.Context, res *inventory.Reservation) error {
	if r.failAfterN > 0 {
		r.callCount++
		if r.callCount > r.failAfterN {
			return r.err
		}
	} else if r.err != nil {
		return r.err
	}
	r.reserved = append(r.reserved, *res)
	return nil
}

func (r *mockReservationRepo) Release(_ context.Context, rid string) error {
	r.released = append(r.released, rid)
	return nil
}
func (r *mockReservationRepo) Confirm(_ context.Context, _ string) error { return nil }
func (r *mockReservationRepo) FindByID(_ context.Context, _ string) (*inventory.Reservation, error) {
	return nil, nil
}
func (r *mockReservationRepo) ListActiveByVariantID(_ context.Context, _ string) ([]inventory.Reservation, error) {
	return nil, nil
}
func (r *mockReservationRepo) ReleaseExpiredBefore(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

// ============================================================
// Mock order repository
// ============================================================

type mockOrderRepo struct {
	saved *order.Order
	err   error
}

func (r *mockOrderRepo) FindByID(_ context.Context, _ string) (*order.Order, error) {
	return nil, nil
}
func (r *mockOrderRepo) FindByCustomerID(_ context.Context, _ string) ([]order.Order, error) {
	return nil, nil
}
func (r *mockOrderRepo) Save(_ context.Context, o *order.Order) error {
	if r.err != nil {
		return r.err
	}
	r.saved = o
	return nil
}
func (r *mockOrderRepo) UpdateStatus(_ context.Context, _ *order.Order) error { return nil }

// ============================================================
// Helpers
// ============================================================

func cartWithItems037(t *testing.T, customerID string, variantIDs ...string) *cart.Cart {
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
		if err := c.AddItem(vid, 2, price); err != nil {
			t.Fatalf("AddItem(%s): %v", vid, err)
		}
	}
	return &c
}

func variantMap037(ids ...string) map[string]*catalog.Variant {
	m := make(map[string]*catalog.Variant, len(ids))
	for _, vid := range ids {
		v, _ := catalog.NewVariant(vid, "prod-1", fmt.Sprintf("SKU-%s", vid))
		v.Name = fmt.Sprintf("Variant %s", vid)
		m[vid] = &v
	}
	return m
}

func pricingContext037(t *testing.T, variantIDs ...string) *pricing.PricingContext {
	t.Helper()
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	for _, vid := range variantIDs {
		pi, err := pricing.NewPricingItem(vid, 2, shared.MustNewMoney(1000, "EUR"))
		if err != nil {
			t.Fatalf("NewPricingItem: %v", err)
		}
		pctx.Items = append(pctx.Items, pi)
	}
	return &pctx
}

// ============================================================
// ReserveInventoryStep tests
// ============================================================

func TestReserveInventoryStep_Name(t *testing.T) {
	step := checkout.NewReserveInventoryStep(&mockReservationRepo{})
	if step.Name() != "reserve_inventory" {
		t.Errorf("Name() = %q, want reserve_inventory", step.Name())
	}
}

func TestReserveInventoryStep_Success(t *testing.T) {
	repo := &mockReservationRepo{}
	step := checkout.NewReserveInventoryStep(repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1", "v2")

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(repo.reserved) != 2 {
		t.Fatalf("reserved count = %d, want 2", len(repo.reserved))
	}
	if repo.reserved[0].VariantID != "v1" {
		t.Errorf("reserved[0].VariantID = %q, want v1", repo.reserved[0].VariantID)
	}
	if repo.reserved[0].Quantity != 2 {
		t.Errorf("reserved[0].Quantity = %d, want 2", repo.reserved[0].Quantity)
	}
	if repo.reserved[1].VariantID != "v2" {
		t.Errorf("reserved[1].VariantID = %q, want v2", repo.reserved[1].VariantID)
	}

	raw, ok := cctx.GetMeta("reservations")
	if !ok {
		t.Fatal("expected reservations in meta")
	}
	ids, ok := raw.([]string)
	if !ok {
		t.Fatalf("reservations is %T, want []string", raw)
	}
	if len(ids) != 2 {
		t.Errorf("reservation IDs len = %d, want 2", len(ids))
	}

	if v, ok := cctx.GetMeta("reserved"); !ok || v != true {
		t.Error("expected reserved=true in meta")
	}
}

func TestReserveInventoryStep_InsufficientStock(t *testing.T) {
	repo := &mockReservationRepo{err: errors.New("insufficient stock")}
	step := checkout.NewReserveInventoryStep(repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error for insufficient stock")
	}
	if _, ok := cctx.GetMeta("reserved"); ok {
		t.Error("reserved meta should not be set on error")
	}
}

func TestReserveInventoryStep_NilCart(t *testing.T) {
	step := checkout.NewReserveInventoryStep(&mockReservationRepo{})

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error for nil cart")
	}
}

func TestReserveInventoryStep_Idempotent(t *testing.T) {
	repo := &mockReservationRepo{}
	step := checkout.NewReserveInventoryStep(repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	count1 := len(repo.reserved)

	// Second call should skip
	repo.err = errors.New("should not be called")
	if err := step.Execute(cctx); err != nil {
		t.Fatalf("second Execute should be idempotent: %v", err)
	}
	if len(repo.reserved) != count1 {
		t.Errorf("reserved count changed from %d to %d on second call", count1, len(repo.reserved))
	}
}

func TestReserveInventoryStep_NilContext(t *testing.T) {
	repo := &mockReservationRepo{}
	step := checkout.NewReserveInventoryStep(repo)

	err := step.Execute(nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
	if len(repo.reserved) != 0 {
		t.Errorf("reserved count = %d, want 0", len(repo.reserved))
	}
}

func TestReserveInventoryStep_PartialFailureRollback(t *testing.T) {
	repo := &mockReservationRepo{
		err:        errors.New("insufficient stock"),
		failAfterN: 1, // first Reserve succeeds, second fails
	}
	step := checkout.NewReserveInventoryStep(repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1", "v2")

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error from second Reserve")
	}
	if len(repo.reserved) != 1 {
		t.Fatalf("reserved count = %d, want 1 (only first succeeded)", len(repo.reserved))
	}
	if len(repo.released) != 1 {
		t.Fatalf("released count = %d, want 1 (rollback of first)", len(repo.released))
	}
	if _, ok := cctx.GetMeta("reserved"); ok {
		t.Error("reserved meta should not be set on error")
	}
	if _, ok := cctx.GetMeta("reservations"); ok {
		t.Error("reservations meta should not be set on error")
	}
}

func TestReserveInventoryStep_EmptyCart(t *testing.T) {
	repo := &mockReservationRepo{}
	step := checkout.NewReserveInventoryStep(repo)

	c, err := cart.NewCart(id.New(), "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = &c

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(repo.reserved) != 0 {
		t.Errorf("reserved count = %d, want 0", len(repo.reserved))
	}
	if v, ok := cctx.GetMeta("reserved"); !ok || v != true {
		t.Error("expected reserved=true even for empty cart")
	}
}

// ============================================================
// CreateOrderStep tests
// ============================================================

func TestCreateOrderStep_Name(t *testing.T) {
	step := checkout.NewCreateOrderStep(
		&mockOrderRepo{},
		&mockVariantRepo037{variants: variantMap037()},
	)
	if step.Name() != "create_order" {
		t.Errorf("Name() = %q, want create_order", step.Name())
	}
}

func TestCreateOrderStep_Success(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1", "v2")}
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1", "v2")
	cctx.SetMeta("pricing", pricingContext037(t, "v1", "v2"))

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if cctx.Order == nil {
		t.Fatal("expected Order to be set on context")
	}
	if cctx.Order.CustomerID != "cust-1" {
		t.Errorf("Order.CustomerID = %q, want cust-1", cctx.Order.CustomerID)
	}
	if cctx.Order.Currency != "EUR" {
		t.Errorf("Order.Currency = %q, want EUR", cctx.Order.Currency)
	}
	if len(cctx.Order.Items()) != 2 {
		t.Errorf("Order.Items len = %d, want 2", len(cctx.Order.Items()))
	}
	// Each item: qty=2, unitPrice=1000→ lineTotal=2000; total=4000
	if cctx.Order.TotalAmount.Amount() != 4000 {
		t.Errorf("TotalAmount = %d, want 4000", cctx.Order.TotalAmount.Amount())
	}
	if orderRepo.saved == nil {
		t.Fatal("expected order to be saved")
	}

	v, ok := cctx.GetMeta("created_order_id")
	if !ok {
		t.Fatal("expected created_order_id in meta")
	}
	if v.(string) == "" {
		t.Error("created_order_id should not be empty")
	}
}

func TestCreateOrderStep_ItemPricesFromPricing(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1")}
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")

	// Override pricing with a different unit price to prove snapshot is from pricing
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	pi, err := pricing.NewPricingItem("v1", 2, shared.MustNewMoney(750, "EUR"))
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	pctx.Items = []pricing.PricingItem{pi}
	cctx.SetMeta("pricing", &pctx)

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	items := cctx.Order.Items()
	if items[0].UnitPrice.Amount() != 750 {
		t.Errorf("Item UnitPrice = %d, want 750 (from pricing snapshot)", items[0].UnitPrice.Amount())
	}
}

func TestCreateOrderStep_NoPricing(t *testing.T) {
	step := checkout.NewCreateOrderStep(
		&mockOrderRepo{},
		&mockVariantRepo037{variants: variantMap037("v1")},
	)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	// No pricing in meta

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error for missing pricing")
	}
	if _, ok := cctx.GetMeta("created_order_id"); ok {
		t.Error("created_order_id should not be set on error")
	}
}

func TestCreateOrderStep_NilCart(t *testing.T) {
	step := checkout.NewCreateOrderStep(
		&mockOrderRepo{},
		&mockVariantRepo037{variants: variantMap037()},
	)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error for nil cart")
	}
}

func TestCreateOrderStep_SaveError(t *testing.T) {
	orderRepo := &mockOrderRepo{err: errors.New("db down")}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1")}
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error from save")
	}
	if _, ok := cctx.GetMeta("created_order_id"); ok {
		t.Error("created_order_id should not be set on error")
	}
}

func TestCreateOrderStep_Idempotent(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1")}
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	savedFirst := orderRepo.saved

	// Second call should skip
	orderRepo.err = errors.New("should not be called")
	if err := step.Execute(cctx); err != nil {
		t.Fatalf("second Execute should be idempotent: %v", err)
	}
	if orderRepo.saved != savedFirst {
		t.Error("order repo Save called on second execution")
	}
}

func TestCreateOrderStep_VariantNotFound(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037()} // empty
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error for missing variant")
	}
	if _, ok := cctx.GetMeta("created_order_id"); ok {
		t.Error("created_order_id should not be set on error")
	}
}

func TestCreateOrderStep_NilContext(t *testing.T) {
	step := checkout.NewCreateOrderStep(
		&mockOrderRepo{},
		&mockVariantRepo037{variants: variantMap037()},
	)

	err := step.Execute(nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

// ============================================================
// mockVariantRepo037 — separate from steps_test.go to avoid redeclaration
// ============================================================

type mockVariantRepo037 struct {
	variants map[string]*catalog.Variant
	err      error
}

func (r *mockVariantRepo037) FindByID(_ context.Context, vid string) (*catalog.Variant, error) {
	if r.err != nil {
		return nil, r.err
	}
	v, ok := r.variants[vid]
	if !ok {
		return nil, nil
	}
	return v, nil
}
func (r *mockVariantRepo037) FindBySKU(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (r *mockVariantRepo037) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return nil, nil
}
func (r *mockVariantRepo037) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (r *mockVariantRepo037) Update(_ context.Context, _ *catalog.Variant) error { return nil }
func (r *mockVariantRepo037) WithTx(_ *sql.Tx) catalog.VariantRepository         { return r }
