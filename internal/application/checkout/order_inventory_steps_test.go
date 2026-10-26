package checkout_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/testutil"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func attachCreateOrderInput(cctx *checkout.Context) {
	cctx.Input = checkout.Input{
		Address: checkout.Address{Country: "DE"},
	}
}

// ============================================================
// Mock reservation repository
// ============================================================

type mockReservationRepo struct {
	reserved       []inventory.Reservation
	released       []string
	releaseErr     error
	releaseFailIDs map[string]bool
	err            error
	failAfterN     int // if > 0, succeed for N calls then fail
	callCount      int
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
	if r.releaseFailIDs[rid] {
		return errors.New("release down")
	}
	return r.releaseErr
}
func (r *mockReservationRepo) Confirm(_ context.Context, _ string) error { return nil }
func (r *mockReservationRepo) FindByID(_ context.Context, _ string) (*inventory.Reservation, error) {
	return nil, nil
}
func (r *mockReservationRepo) ListActiveByVariantID(_ context.Context, _ string) ([]inventory.Reservation, error) {
	return nil, nil
}
func (r *mockReservationRepo) ReleaseExpiredBefore(_ context.Context, _ time.Time) ([]inventory.ReleasedReservation, error) {
	return nil, nil
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
func (r *mockOrderRepo) FindByContactEmail(_ context.Context, _ string) ([]order.Order, error) {
	return nil, nil
}
func (r *mockOrderRepo) List(_ context.Context, _, _ int) ([]order.Order, error) {
	return nil, nil
}
func (r *mockOrderRepo) Save(_ context.Context, o *order.Order) error {
	if r.err != nil {
		return r.err
	}
	r.saved = o
	return nil
}
func (r *mockOrderRepo) UpdateStatus(_ context.Context, _ *order.Order) error   { return nil }
func (r *mockOrderRepo) LinkToCustomer(_ context.Context, _ *order.Order) error { return nil }
func (r *mockOrderRepo) LinkToCustomerByContactEmail(_ context.Context, _, _ string, _ time.Time) (int64, error) {
	return 0, nil
}
func (r *mockOrderRepo) ListPaidTaxSnapshots(_ context.Context, _, _ time.Time) ([]order.TaxSnapshotRow, error) {
	return nil, nil
}

// ============================================================
// Helpers
// ============================================================

func newPermissiveReserveStep(reservations inventory.ReservationRepository, opts ...checkout.ReserveOption) *checkout.ReserveInventoryStep {
	variants, products := cartApp.PermissiveCatalogRepos()
	return checkout.NewReserveInventoryStep(reservations, products, variants, cartApp.EmptyBundleComponents(), opts...)
}

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
	step := newPermissiveReserveStep(&mockReservationRepo{})
	if step.Name() != "reserve_inventory" {
		t.Errorf("Name() = %q, want reserve_inventory", step.Name())
	}
}

func TestReserveInventoryStep_Success(t *testing.T) {
	repo := &mockReservationRepo{}
	step := newPermissiveReserveStep(repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1", "v2")

	if err := step.Execute(context.Background(), cctx); err != nil {
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
	rawQty, ok := cctx.GetMeta(checkout.ReservedVariantQtyMeta)
	qty, isMap := rawQty.(map[string]int)
	if !ok || !isMap || qty["v1"] != 2 || qty["v2"] != 2 {
		t.Fatalf("reserved qty = %#v, want v1=2 v2=2", rawQty)
	}
}

func TestReserveInventoryStep_InsufficientStock(t *testing.T) {
	repo := &mockReservationRepo{err: errors.New("insufficient stock")}
	step := newPermissiveReserveStep(repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")

	err := step.Execute(context.Background(), cctx)
	if err == nil {
		t.Fatal("expected error for insufficient stock")
	}
	if _, ok := cctx.GetMeta("reserved"); ok {
		t.Error("reserved meta should not be set on error")
	}
}

func TestReserveInventoryStep_NilCart(t *testing.T) {
	step := newPermissiveReserveStep(&mockReservationRepo{})

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")

	err := step.Execute(context.Background(), cctx)
	if err == nil {
		t.Fatal("expected error for nil cart")
	}
}

func TestReserveInventoryStep_Idempotent(t *testing.T) {
	repo := &mockReservationRepo{}
	step := newPermissiveReserveStep(repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")

	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	count1 := len(repo.reserved)

	// Second call should skip
	repo.err = errors.New("should not be called")
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("second Execute should be idempotent: %v", err)
	}
	if len(repo.reserved) != count1 {
		t.Errorf("reserved count changed from %d to %d on second call", count1, len(repo.reserved))
	}
}

func TestReserveInventoryStep_NilContext(t *testing.T) {
	repo := &mockReservationRepo{}
	step := newPermissiveReserveStep(repo)

	err := step.Execute(context.Background(), nil)
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
	step := newPermissiveReserveStep(repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1", "v2")

	err := step.Execute(context.Background(), cctx)
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
	step := newPermissiveReserveStep(repo)

	c, err := cart.NewCart(id.New(), "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = &c

	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(repo.reserved) != 0 {
		t.Errorf("reserved count = %d, want 0", len(repo.reserved))
	}
	if v, ok := cctx.GetMeta("reserved"); !ok || v != true {
		t.Error("expected reserved=true even for empty cart")
	}
}

// TestReserveInventoryStep_EmitsStockUpdatedEvent pins PR-1049's checkout
// fix: WithStockEventPublishing must publish inventory.EventStockUpdated
// for every successful Reserve, mirroring InventoryAdminHandler.Adjust's
// own event (PR-1036) — checkout is the highest-frequency stock-changing
// path, and was the one PR-1036 left completely uncovered.
func TestReserveInventoryStep_EmitsStockUpdatedEvent(t *testing.T) {
	repo := &mockReservationRepo{}
	variants := &mockVariantRepo037{variants: variantMap037("v1", "v2")}
	bus := event.NewBus(logger.New("error"))
	step := checkout.NewReserveInventoryStep(repo, &mockProductRepo047{}, variants, cartApp.EmptyBundleComponents(), checkout.WithStockEventPublishing(bus))

	var captured []event.Event
	bus.On(inventory.EventStockUpdated, func(_ context.Context, evt event.Event) error {
		captured = append(captured, evt)
		return nil
	})

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1", "v2")

	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(captured) != 2 {
		t.Fatalf("published %d events, want 2 (one per reserved item)", len(captured))
	}
	for i, want := range []string{"v1", "v2"} {
		data, ok := captured[i].Data.(inventory.StockUpdatedData)
		if !ok {
			t.Fatalf("event[%d] data type = %T, want StockUpdatedData", i, captured[i].Data)
		}
		if data.VariantID != want || data.ProductID != "prod-1" || data.SKU != fmt.Sprintf("SKU-%s", want) || data.Delta == nil || *data.Delta != -2 {
			t.Errorf("event[%d] data = %+v, want variant_id=%s product_id=prod-1 sku=SKU-%s delta=-2", i, data, want, want)
		}
	}
}

// TestReserveInventoryStep_NoBusNoEvent pins that Execute works exactly
// as before when WithStockEventPublishing was never used — the default
// for every existing caller/test that doesn't need eventing, matching
// InventoryAdminHandler's own SetBus-optional convention.
func TestReserveInventoryStep_NoBusNoEvent(t *testing.T) {
	repo := &mockReservationRepo{}
	step := newPermissiveReserveStep(repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")

	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(repo.reserved) != 1 {
		t.Fatalf("reserved count = %d, want 1", len(repo.reserved))
	}
}

func TestNewReserveInventoryStep_NilVariantsPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for nil variants repository")
		}
	}()
	_, products := cartApp.PermissiveCatalogRepos()
	checkout.NewReserveInventoryStep(&mockReservationRepo{}, products, nil, cartApp.EmptyBundleComponents())
}

func TestWithStockEventPublishing_NilBusPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for nil bus")
		}
	}()
	variants, products := cartApp.PermissiveCatalogRepos()
	checkout.NewReserveInventoryStep(&mockReservationRepo{}, products, variants, cartApp.EmptyBundleComponents(), checkout.WithStockEventPublishing(nil))
}

type reserveComponentRepo struct {
	rows []catalog.BundleComponent
}

func (r reserveComponentRepo) ListByBundleID(_ context.Context, bundleProductID string) ([]catalog.BundleComponent, error) {
	out := make([]catalog.BundleComponent, 0)
	for _, row := range r.rows {
		if row.BundleProductID == bundleProductID {
			out = append(out, row)
		}
	}
	return out, nil
}
func (reserveComponentRepo) CountByBundleID(context.Context, string) (int, error) { return 0, nil }
func (reserveComponentRepo) CountByComponentID(context.Context, string) (int, error) {
	return 0, nil
}
func (reserveComponentRepo) Upsert(context.Context, *catalog.BundleComponent) error { return nil }
func (reserveComponentRepo) Remove(context.Context, string, string) error           { return nil }

func TestReserveInventoryStep_DynamicBundleReservesComponents(t *testing.T) {
	repo := &mockReservationRepo{}
	variants := &mockVariantRepo037{variants: map[string]*catalog.Variant{
		"bundle-v": {ID: "bundle-v", ProductID: "bundle", SKU: "SKU-BUNDLE"},
		"a-v":      {ID: "a-v", ProductID: "a", SKU: "SKU-A"},
		"b-v":      {ID: "b-v", ProductID: "b", SKU: "SKU-B"},
		"opt-v":    {ID: "opt-v", ProductID: "opt", SKU: "SKU-OPT"},
	}}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"bundle": {ID: "bundle", Type: catalog.TypeBundle, StockMode: catalog.StockModeDynamic, Status: catalog.StatusActive},
		"a":      {ID: "a", Type: catalog.TypeSimple, Status: catalog.StatusActive},
		"b":      {ID: "b", Type: catalog.TypeSimple, Status: catalog.StatusActive},
		"opt":    {ID: "opt", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	components := reserveComponentRepo{rows: []catalog.BundleComponent{
		{BundleProductID: "bundle", ComponentProductID: "a", Quantity: 2, Required: true},
		{BundleProductID: "bundle", ComponentProductID: "b", Quantity: 1, Required: true},
		{BundleProductID: "bundle", ComponentProductID: "opt", Quantity: 5, Required: false},
	}}
	step := checkout.NewReserveInventoryStep(repo, products, variants, components)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "bundle-v")
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(repo.reserved) != 2 {
		t.Fatalf("reserved = %+v, want 2 component reservations", repo.reserved)
	}
	got := map[string]int{}
	for _, res := range repo.reserved {
		got[res.VariantID] = res.Quantity
	}
	if got["a-v"] != 4 || got["b-v"] != 2 {
		t.Fatalf("reserved quantities = %v, want a-v=4 (2*2) and b-v=2 (1*2)", got)
	}
	if _, ok := got["bundle-v"]; ok {
		t.Fatal("reserved the bundle variant")
	}
	if _, ok := got["opt-v"]; ok {
		t.Fatal("reserved an optional component")
	}
}

func TestReserveInventoryStep_DynamicBundleSkipsDigitalLeaves(t *testing.T) {
	t.Run("mixed simple and virtual reserves only the simple leaf", func(t *testing.T) {
		repo := &mockReservationRepo{}
		variants := &mockVariantRepo037{variants: map[string]*catalog.Variant{
			"bundle-v":  {ID: "bundle-v", ProductID: "bundle", SKU: "SKU-BUNDLE"},
			"simple-v":  {ID: "simple-v", ProductID: "simple", SKU: "SKU-SIMPLE"},
			"virtual-v": {ID: "virtual-v", ProductID: "virtual", SKU: "SKU-VIRTUAL"},
		}}
		products := &mockProductRepo047{products: map[string]*catalog.Product{
			"bundle":  {ID: "bundle", Type: catalog.TypeBundle, StockMode: catalog.StockModeDynamic, Status: catalog.StatusActive},
			"simple":  {ID: "simple", Type: catalog.TypeSimple, Status: catalog.StatusActive},
			"virtual": {ID: "virtual", Type: catalog.TypeVirtual, Status: catalog.StatusActive},
		}}
		components := reserveComponentRepo{rows: []catalog.BundleComponent{
			{BundleProductID: "bundle", ComponentProductID: "simple", Quantity: 2, Required: true},
			{BundleProductID: "bundle", ComponentProductID: "virtual", Quantity: 9, Required: true},
		}}
		step := checkout.NewReserveInventoryStep(repo, products, variants, components)
		cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
		cctx.Cart = cartWithItems037(t, "cust-1", "bundle-v")
		cctx.Cart.Items[0].Quantity = 3
		if err := step.Execute(context.Background(), cctx); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if len(repo.reserved) != 1 {
			t.Fatalf("reserved = %+v, want only the simple leaf", repo.reserved)
		}
		if repo.reserved[0].VariantID != "simple-v" || repo.reserved[0].Quantity != 6 {
			t.Fatalf("reserved = %+v, want simple-v quantity 6 (2*3); virtual has no target", repo.reserved[0])
		}
		ids, _ := cctx.GetMeta("reservations")
		if got, ok := ids.([]string); !ok || len(got) != 1 {
			t.Fatalf("reservations = %#v, want one id", ids)
		}
	})

	t.Run("virtual-only required set reserves nothing", func(t *testing.T) {
		repo := &mockReservationRepo{}
		variants := &mockVariantRepo037{variants: map[string]*catalog.Variant{
			"bundle-v":  {ID: "bundle-v", ProductID: "bundle", SKU: "SKU-BUNDLE"},
			"virtual-v": {ID: "virtual-v", ProductID: "virtual", SKU: "SKU-VIRTUAL"},
		}}
		products := &mockProductRepo047{products: map[string]*catalog.Product{
			"bundle":  {ID: "bundle", Type: catalog.TypeBundle, StockMode: catalog.StockModeDynamic, Status: catalog.StatusActive},
			"virtual": {ID: "virtual", Type: catalog.TypeVirtual, Status: catalog.StatusActive},
		}}
		components := reserveComponentRepo{rows: []catalog.BundleComponent{
			{BundleProductID: "bundle", ComponentProductID: "virtual", Quantity: 4, Required: true},
		}}
		step := checkout.NewReserveInventoryStep(repo, products, variants, components)
		cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
		cctx.Cart = cartWithItems037(t, "cust-1", "bundle-v")
		if err := step.Execute(context.Background(), cctx); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if len(repo.reserved) != 0 {
			t.Fatalf("reserved = %+v, want no reservations for a digital-only required set", repo.reserved)
		}
		ids, ok := cctx.GetMeta("reservations")
		got, isSlice := ids.([]string)
		if !ok || !isSlice || len(got) != 0 {
			t.Fatalf("reservations = %#v, want an empty list", ids)
		}
		reserved, ok := cctx.GetMeta("reserved")
		if !ok || reserved != true {
			t.Fatalf("reserved meta = %#v, want true", reserved)
		}
	})

	t.Run("downloadable-only required set reserves nothing", func(t *testing.T) {
		repo := &mockReservationRepo{}
		variants := &mockVariantRepo037{variants: map[string]*catalog.Variant{
			"bundle-v":       {ID: "bundle-v", ProductID: "bundle", SKU: "SKU-BUNDLE"},
			"downloadable-v": {ID: "downloadable-v", ProductID: "downloadable", SKU: "SKU-DL"},
		}}
		products := &mockProductRepo047{products: map[string]*catalog.Product{
			"bundle":       {ID: "bundle", Type: catalog.TypeBundle, StockMode: catalog.StockModeDynamic, Status: catalog.StatusActive},
			"downloadable": {ID: "downloadable", Type: catalog.TypeDownloadable, Status: catalog.StatusActive},
		}}
		components := reserveComponentRepo{rows: []catalog.BundleComponent{
			{BundleProductID: "bundle", ComponentProductID: "downloadable", Quantity: 4, Required: true},
		}}
		step := checkout.NewReserveInventoryStep(repo, products, variants, components)
		cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
		cctx.Cart = cartWithItems037(t, "cust-1", "bundle-v")
		if err := step.Execute(context.Background(), cctx); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if len(repo.reserved) != 0 {
			t.Fatalf("reserved = %+v, want no reservations for a downloadable-only required set", repo.reserved)
		}
		ids, ok := cctx.GetMeta("reservations")
		got, isSlice := ids.([]string)
		if !ok || !isSlice || len(got) != 0 {
			t.Fatalf("reservations = %#v, want an empty list", ids)
		}
		reserved, ok := cctx.GetMeta("reserved")
		if !ok || reserved != true {
			t.Fatalf("reserved meta = %#v, want true", reserved)
		}
	})
}

func TestReserveInventoryStep_FixedBundleReservesOwnVariant(t *testing.T) {
	repo := &mockReservationRepo{}
	variants := &mockVariantRepo037{variants: map[string]*catalog.Variant{
		"bundle-v": {ID: "bundle-v", ProductID: "bundle", SKU: "SKU-BUNDLE"},
		"a-v":      {ID: "a-v", ProductID: "a", SKU: "SKU-A"},
	}}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"bundle": {ID: "bundle", Type: catalog.TypeBundle, StockMode: catalog.StockModeFixed, Status: catalog.StatusActive},
		"a":      {ID: "a", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	components := reserveComponentRepo{rows: []catalog.BundleComponent{
		{BundleProductID: "bundle", ComponentProductID: "a", Quantity: 3, Required: true},
	}}
	step := checkout.NewReserveInventoryStep(repo, products, variants, components)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "bundle-v")
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(repo.reserved) != 1 {
		t.Fatalf("reserved = %+v, want one reservation of the bundle variant", repo.reserved)
	}
	if repo.reserved[0].VariantID != "bundle-v" || repo.reserved[0].Quantity != 2 {
		t.Fatalf("reserved = %+v, want bundle-v quantity 2", repo.reserved[0])
	}
}

func assertBundleCannotBeReserved(t *testing.T, err error, inner string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected reserve error")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Message != "bundle cannot be reserved" {
		t.Fatalf("err = %v, want customer message %q", err, "bundle cannot be reserved")
	}
	if inner != "" && !strings.Contains(err.Error(), inner) {
		t.Fatalf("err = %v, want wrapped detail %q", err, inner)
	}
}

func TestReserveInventoryStep_DynamicBundleTargetFailures(t *testing.T) {
	bundleVariant := &catalog.Variant{ID: "bundle-v", ProductID: "bundle", SKU: "SKU-BUNDLE"}
	oneLeaf := &mockVariantRepo037{variants: map[string]*catalog.Variant{
		"bundle-v": bundleVariant,
		"a-v":      {ID: "a-v", ProductID: "a", SKU: "SKU-A"},
	}}
	twoLeaves := &mockVariantRepo037{variants: map[string]*catalog.Variant{
		"bundle-v": bundleVariant,
		"a-v":      {ID: "a-v", ProductID: "a", SKU: "SKU-A"},
		"a-v2":     {ID: "a-v2", ProductID: "a", SKU: "SKU-A2"},
	}}

	cases := []struct {
		name       string
		variants   *mockVariantRepo037
		products   map[string]*catalog.Product
		components []catalog.BundleComponent
		lineQty    int
		inner      string
	}{
		{
			name:     "empty required",
			variants: oneLeaf,
			products: map[string]*catalog.Product{
				"bundle": {ID: "bundle", Type: catalog.TypeBundle, StockMode: catalog.StockModeDynamic, Status: catalog.StatusActive},
				"opt":    {ID: "opt", Type: catalog.TypeSimple, Status: catalog.StatusActive},
			},
			components: []catalog.BundleComponent{
				{BundleProductID: "bundle", ComponentProductID: "opt", Quantity: 1, Required: false},
			},
			inner: "no required components",
		},
		{
			name:     "inactive",
			variants: oneLeaf,
			products: map[string]*catalog.Product{
				"bundle": {ID: "bundle", Type: catalog.TypeBundle, StockMode: catalog.StockModeDynamic, Status: catalog.StatusActive},
				"a":      {ID: "a", Type: catalog.TypeSimple, Status: catalog.StatusArchived},
			},
			components: []catalog.BundleComponent{
				{BundleProductID: "bundle", ComponentProductID: "a", Quantity: 1, Required: true},
			},
			inner: "not active",
		},
		{
			name:     "non-leaf",
			variants: oneLeaf,
			products: map[string]*catalog.Product{
				"bundle": {ID: "bundle", Type: catalog.TypeBundle, StockMode: catalog.StockModeDynamic, Status: catalog.StatusActive},
				"a":      {ID: "a", Type: catalog.TypeConfigurable, Status: catalog.StatusActive},
			},
			components: []catalog.BundleComponent{
				{BundleProductID: "bundle", ComponentProductID: "a", Quantity: 1, Required: true},
			},
			inner: "simple, virtual, or downloadable",
		},
		{
			name:     "multi-variant",
			variants: twoLeaves,
			products: map[string]*catalog.Product{
				"bundle": {ID: "bundle", Type: catalog.TypeBundle, StockMode: catalog.StockModeDynamic, Status: catalog.StatusActive},
				"a":      {ID: "a", Type: catalog.TypeSimple, Status: catalog.StatusActive},
			},
			components: []catalog.BundleComponent{
				{BundleProductID: "bundle", ComponentProductID: "a", Quantity: 1, Required: true},
			},
			inner: "exactly one variant",
		},
		{
			name:     "qty overflow",
			variants: oneLeaf,
			products: map[string]*catalog.Product{
				"bundle": {ID: "bundle", Type: catalog.TypeBundle, StockMode: catalog.StockModeDynamic, Status: catalog.StatusActive},
				"a":      {ID: "a", Type: catalog.TypeSimple, Status: catalog.StatusActive},
			},
			components: []catalog.BundleComponent{
				{BundleProductID: "bundle", ComponentProductID: "a", Quantity: 2, Required: true},
			},
			lineQty: math.MaxInt,
			inner:   "reserve quantity is invalid",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockReservationRepo{}
			step := checkout.NewReserveInventoryStep(repo, &mockProductRepo047{products: tc.products}, tc.variants, reserveComponentRepo{rows: tc.components})
			cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
			cctx.Cart = cartWithItems037(t, "cust-1", "bundle-v")
			if tc.lineQty != 0 {
				cctx.Cart.Items[0].Quantity = tc.lineQty
			}
			err := step.Execute(context.Background(), cctx)
			assertBundleCannotBeReserved(t, err, tc.inner)
			if len(repo.reserved) != 0 || len(repo.released) != 0 {
				t.Fatalf("reserved=%d released=%d, want no reservations", len(repo.reserved), len(repo.released))
			}
		})
	}
}

func TestReserveInventoryStep_DynamicBundleMidReserveReleasesPrior(t *testing.T) {
	repo := &mockReservationRepo{err: apperror.New(apperror.CodeConflict, "insufficient stock"), failAfterN: 1}
	variants := &mockVariantRepo037{variants: map[string]*catalog.Variant{
		"bundle-v": {ID: "bundle-v", ProductID: "bundle", SKU: "SKU-BUNDLE"},
		"a-v":      {ID: "a-v", ProductID: "a", SKU: "SKU-A"},
		"b-v":      {ID: "b-v", ProductID: "b", SKU: "SKU-B"},
	}}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"bundle": {ID: "bundle", Type: catalog.TypeBundle, StockMode: catalog.StockModeDynamic, Status: catalog.StatusActive},
		"a":      {ID: "a", Type: catalog.TypeSimple, Status: catalog.StatusActive},
		"b":      {ID: "b", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	components := reserveComponentRepo{rows: []catalog.BundleComponent{
		{BundleProductID: "bundle", ComponentProductID: "a", Quantity: 1, Required: true},
		{BundleProductID: "bundle", ComponentProductID: "b", Quantity: 1, Required: true},
	}}
	step := checkout.NewReserveInventoryStep(repo, products, variants, components)
	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "bundle-v")
	err := step.Execute(context.Background(), cctx)
	assertBundleCannotBeReserved(t, err, "insufficient stock")
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || strings.Contains(appErr.Message, "a-v") || strings.Contains(appErr.Message, "b-v") {
		t.Fatalf("message = %q, want %q without a variant id", appErr.Message, "bundle cannot be reserved")
	}
	if len(repo.reserved) != 1 {
		t.Fatalf("reserved = %+v, want the first component only", repo.reserved)
	}
	if len(repo.released) != 1 || repo.released[0] != repo.reserved[0].ID {
		t.Fatalf("released = %v, want the first reservation %s", repo.released, repo.reserved[0].ID)
	}
}

func TestReserveInventoryStep_DynamicBundlePublishesParentProduct(t *testing.T) {
	repo := &mockReservationRepo{}
	variants := &mockVariantRepo037{variants: map[string]*catalog.Variant{
		"bundle-v": {ID: "bundle-v", ProductID: "bundle", SKU: "SKU-BUNDLE"},
		"a-v":      {ID: "a-v", ProductID: "a", SKU: "SKU-A"},
	}}
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"bundle": {ID: "bundle", Type: catalog.TypeBundle, StockMode: catalog.StockModeDynamic, Status: catalog.StatusActive},
		"a":      {ID: "a", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	components := reserveComponentRepo{rows: []catalog.BundleComponent{
		{BundleProductID: "bundle", ComponentProductID: "a", Quantity: 1, Required: true},
	}}
	bus := event.NewBus(logger.New("error"))
	step := checkout.NewReserveInventoryStep(repo, products, variants, components, checkout.WithStockEventPublishing(bus))
	var productIDs []string
	bus.On(inventory.EventStockUpdated, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(inventory.StockUpdatedData)
		if !ok {
			t.Fatalf("data type %T", evt.Data)
		}
		productIDs = append(productIDs, data.ProductID)
		return nil
	})
	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "bundle-v")
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	seenParent := false
	seenComponent := false
	for _, id := range productIDs {
		if id == "bundle" {
			seenParent = true
		}
		if id == "a" {
			seenComponent = true
		}
	}
	if !seenParent || !seenComponent {
		t.Fatalf("product ids = %v, want component a and parent bundle", productIDs)
	}
}

// TestReserveInventoryStep_RollbackEmitsStockUpdatedEvent pins that a
// rollback Release (triggered by a later item's Reserve failing) also
// publishes inventory.EventStockUpdated for the released reservation —
// its restore is a real stock change too, and the search index would
// otherwise keep showing the item as reserved/unavailable after checkout
// actually released it back.
func TestReserveInventoryStep_RollbackEmitsStockUpdatedEvent(t *testing.T) {
	repo := &mockReservationRepo{
		err:        errors.New("insufficient stock"),
		failAfterN: 1, // first Reserve succeeds, second fails
	}
	variants := &mockVariantRepo037{variants: variantMap037("v1", "v2")}
	bus := event.NewBus(logger.New("error"))
	step := checkout.NewReserveInventoryStep(repo, &mockProductRepo047{}, variants, cartApp.EmptyBundleComponents(), checkout.WithStockEventPublishing(bus))

	var captured []event.Event
	bus.On(inventory.EventStockUpdated, func(_ context.Context, evt event.Event) error {
		captured = append(captured, evt)
		return nil
	})

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1", "v2")

	if err := step.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected error from second Reserve")
	}

	// One publish for v1's own successful Reserve, one more for its
	// rollback Release once v2's Reserve failed.
	if len(captured) != 2 {
		t.Fatalf("published %d events, want 2 (v1 reserve + v1 rollback release)", len(captured))
	}
	for i, evt := range captured {
		data, ok := evt.Data.(inventory.StockUpdatedData)
		if !ok || data.VariantID != "v1" {
			t.Errorf("event[%d] = %+v, want VariantID=v1", i, evt.Data)
		}
	}
}

// TestReserveInventoryStep_ReusesValidateCartStepResolvedVariant pins the
// code review fix: ReserveInventoryStep's stock-event publishing must
// reuse the variant ValidateCartStep already resolved (via cctx's
// cart_variants meta) rather than re-querying VariantRepository per
// item — checkout is the highest-frequency stock-changing path in the
// system, and both steps run in the same pipeline a few steps apart. The
// variant repo is sabotaged (forced to error) after ValidateCartStep runs
// but before ReserveInventoryStep runs — if reserve fell back to its own
// FindByID instead of the cached value, it would hit that error and
// silently skip publishing (its own documented best-effort behavior),
// so a captured event here is only possible via the cache.
func TestReserveInventoryStep_ReusesValidateCartStepResolvedVariant(t *testing.T) {
	repo := &mockReservationRepo{}
	variants := &mockVariantRepo037{variants: variantMap037("v1")}
	validateStep := newValidateCartStep(variants, &mockProductRepo047{})
	bus := event.NewBus(logger.New("error"))
	reserveStep := checkout.NewReserveInventoryStep(repo, &mockProductRepo047{}, variants, cartApp.EmptyBundleComponents(), checkout.WithStockEventPublishing(bus))

	var captured []event.Event
	bus.On(inventory.EventStockUpdated, func(_ context.Context, evt event.Event) error {
		captured = append(captured, evt)
		return nil
	})

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")

	if err := validateStep.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("ValidateCartStep.Execute: %v", err)
	}

	// Sabotage: any further lookup on this shared repo now fails.
	variants.err = errors.New("must not be called again")

	if err := reserveStep.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("ReserveInventoryStep.Execute: %v", err)
	}

	if len(captured) != 1 {
		t.Fatalf("published %d events, want 1 (must have used the cached variant, not re-queried)", len(captured))
	}
	data, ok := captured[0].Data.(inventory.StockUpdatedData)
	if !ok || data.VariantID != "v1" || data.ProductID != "prod-1" {
		t.Fatalf("event data = %+v, want variant_id=v1 product_id=prod-1", captured[0].Data)
	}
}

// ============================================================
// CreateOrderStep tests
// ============================================================

func TestCreateOrderStep_Name(t *testing.T) {
	step := checkout.NewCreateOrderStep(
		&mockOrderRepo{},
		&mockVariantRepo037{variants: variantMap037()},
		nil,
		nil, idleProducts(), idleComponents(), idlePrices())
	if step.Name() != "create_order" {
		t.Errorf("Name() = %q, want create_order", step.Name())
	}
}

func TestCreateOrderStep_Success(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1", "v2")}
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, nil, idleProducts(), idleComponents(), idlePrices())

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1", "v2")
	cctx.SetMeta("pricing", pricingContext037(t, "v1", "v2"))

	if err := step.Execute(context.Background(), cctx); err != nil {
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
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, nil, idleProducts(), idleComponents(), idlePrices())

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
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

	if err := step.Execute(context.Background(), cctx); err != nil {
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
		nil,
		nil, idleProducts(), idleComponents(), idlePrices())

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	// No pricing in meta

	err := step.Execute(context.Background(), cctx)
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
		nil,
		nil, idleProducts(), idleComponents(), idlePrices())

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")

	err := step.Execute(context.Background(), cctx)
	if err == nil {
		t.Fatal("expected error for nil cart")
	}
}

func TestCreateOrderStep_SaveError(t *testing.T) {
	orderRepo := &mockOrderRepo{err: errors.New("db down")}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1")}
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, nil, idleProducts(), idleComponents(), idlePrices())

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	err := step.Execute(context.Background(), cctx)
	if err == nil {
		t.Fatal("expected error from save")
	}
	if _, ok := cctx.GetMeta("created_order_id"); ok {
		t.Error("created_order_id should not be set on error")
	}
}

func TestCreateOrderStep_ReleaseClearsReservedAndAllowsRetry(t *testing.T) {
	repo := &mockReservationRepo{}
	variants := &mockVariantRepo037{variants: variantMap037("v1")}
	products := &mockProductRepo047{}
	reserve := checkout.NewReserveInventoryStep(repo, products, variants, cartApp.EmptyBundleComponents())
	create := checkout.NewCreateOrderStep(&mockOrderRepo{err: errors.New("db down")}, variants, nil, nil, products, idleComponents(), idlePrices()).
		WithReservations(repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	if err := reserve.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := create.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected save error")
	}
	if reserved, _ := cctx.GetMeta("reserved"); reserved != false {
		t.Fatalf("reserved = %#v, want false after a successful release", reserved)
	}
	if holds, ok := cctx.GetMeta(checkout.ReservationHoldsMeta); ok {
		listed, isHolds := holds.([]checkout.ReservationHold)
		if !isHolds || len(listed) != 0 {
			t.Fatalf("holds = %#v, want none after release", holds)
		}
	}
	if len(repo.released) != 1 {
		t.Fatalf("released = %v, want the one hold", repo.released)
	}

	before := len(repo.reserved)
	if err := reserve.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("reserve after release: %v", err)
	}
	if len(repo.reserved) != before+1 {
		t.Fatalf("reserved count = %d, want %d (a new hold, not the released one)", len(repo.reserved), before+1)
	}
	if reserved, _ := cctx.GetMeta("reserved"); reserved != true {
		t.Fatalf("reserved = %#v, want true after the new hold", reserved)
	}
}

func TestCreateOrderStep_FailedReleaseDoesNotStayValid(t *testing.T) {
	repo := &mockReservationRepo{releaseFailIDs: map[string]bool{"res-b": true}}
	variants := &mockVariantRepo037{variants: variantMap037("v1")}
	products := &mockProductRepo047{}
	create := checkout.NewCreateOrderStep(&mockOrderRepo{err: errors.New("db down")}, variants, nil, nil, products, idleComponents(), idlePrices()).
		WithReservations(repo)
	reserve := checkout.NewReserveInventoryStep(repo, products, variants, cartApp.EmptyBundleComponents())

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))
	cctx.SetMeta("reserved", true)
	cctx.SetMeta("reservations", []string{"res-a", "res-b"})
	cctx.SetMeta(checkout.ReservationHoldsMeta, []checkout.ReservationHold{
		{ID: "res-a", VariantID: "v1", Quantity: 1},
		{ID: "res-b", VariantID: "v1", Quantity: 1},
	})
	cctx.SetMeta(checkout.ReservedVariantQtyMeta, map[string]int{"v1": 2})

	if err := create.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected save error")
	}
	if reserved, _ := cctx.GetMeta("reserved"); reserved != false {
		t.Fatalf("reserved = %#v, want false when a release failed", reserved)
	}
	raw, _ := cctx.GetMeta(checkout.ReservationHoldsMeta)
	holds, _ := raw.([]checkout.ReservationHold)
	if len(holds) != 1 || holds[0].ID != "res-b" {
		t.Fatalf("holds = %+v, want only res-b", holds)
	}
	if len(repo.released) != 2 {
		t.Fatalf("released = %v, want both ids attempted", repo.released)
	}

	repo.releaseFailIDs = nil
	if err := reserve.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("reserve after partial release: %v", err)
	}
	if len(repo.reserved) != 1 {
		t.Fatalf("reserved = %+v, want one new hold after the leftover was released", repo.reserved)
	}
}

func TestCreateOrderStep_Idempotent(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1")}
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, nil, idleProducts(), idleComponents(), idlePrices())

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	savedFirst := orderRepo.saved

	// Second call should skip
	orderRepo.err = errors.New("should not be called")
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("second Execute should be idempotent: %v", err)
	}
	if orderRepo.saved != savedFirst {
		t.Error("order repo Save called on second execution")
	}
}

func TestCreateOrderStep_VariantNotFound(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037()} // empty
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, nil, nil, idleProducts(), idleComponents(), idlePrices())

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	err := step.Execute(context.Background(), cctx)
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
		nil,
		nil, idleProducts(), idleComponents(), idlePrices())

	err := step.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

type mockStoreCreditService struct {
	balance    int64
	redeemed   []int64
	issued     []int64
	issuedKeys []string
}

func (m *mockStoreCreditService) GetBalance(_ context.Context, _, currency string) (shared.Money, error) {
	return shared.MustNewMoney(m.balance, currency), nil
}

func (m *mockStoreCreditService) Redeem(_ context.Context, _, _ string, amount shared.Money) error {
	m.redeemed = append(m.redeemed, amount.Amount())
	m.balance -= amount.Amount()
	return nil
}

func (m *mockStoreCreditService) Issue(_ context.Context, _ string, amount shared.Money, _, key string) error {
	return m.recordIssue(amount, key)
}

func (m *mockStoreCreditService) Restore(_ context.Context, _ string, amount shared.Money, _, key string) error {
	return m.recordIssue(amount, key)
}

func (m *mockStoreCreditService) recordIssue(amount shared.Money, key string) error {
	if key != "" {
		for _, k := range m.issuedKeys {
			if k == key {
				return nil
			}
		}
		m.issuedKeys = append(m.issuedKeys, key)
	}
	m.issued = append(m.issued, amount.Amount())
	m.balance += amount.Amount()
	return nil
}

func TestCreateOrderStep_ApplyStoreCredit(t *testing.T) {
	orderRepo := &mockOrderRepo{}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1")}
	credits := &mockStoreCreditService{balance: 1500}
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, credits, nil, idleProducts(), idleComponents(), idlePrices())

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Input.StoreCreditAmount = 1000
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if cctx.Order.StoreCreditApplied.Amount() != 1000 {
		t.Errorf("StoreCreditApplied = %d, want 1000", cctx.Order.StoreCreditApplied.Amount())
	}
	payable, err := cctx.Order.PayableAmount()
	if err != nil {
		t.Fatalf("PayableAmount: %v", err)
	}
	if payable.Amount() != 1000 {
		t.Errorf("PayableAmount = %d, want 1000", payable.Amount())
	}
	if len(credits.redeemed) != 1 || credits.redeemed[0] != 1000 {
		t.Errorf("redeemed = %v, want [1000]", credits.redeemed)
	}
}

func TestCreateOrderStep_StoreCreditRequiresCustomer(t *testing.T) {
	step := checkout.NewCreateOrderStep(
		&mockOrderRepo{},
		&mockVariantRepo037{variants: variantMap037("v1")},
		&mockStoreCreditService{balance: 1000},
		nil, idleProducts(), idleComponents(), idlePrices())

	cctx := checkout.NewContext("cart-1", "", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Input.StoreCreditAmount = 500
	c, err := cart.NewCart("cart-1", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.AddItem("v1", 2, shared.MustNewMoney(1000, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	cctx.Cart = &c
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	err = step.Execute(context.Background(), cctx)
	if err == nil {
		t.Fatal("expected validation error for guest store credit")
	}
}

func TestCreateOrderStep_SaveErrorRollsBackStoreCredit(t *testing.T) {
	orderRepo := &mockOrderRepo{err: errors.New("db down")}
	variantRepo := &mockVariantRepo037{variants: variantMap037("v1")}
	credits := &mockStoreCreditService{balance: 1500}
	step := checkout.NewCreateOrderStep(orderRepo, variantRepo, credits, nil, idleProducts(), idleComponents(), idlePrices())

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Input.StoreCreditAmount = 1000
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	err := step.Execute(context.Background(), cctx)
	if err == nil {
		t.Fatal("expected error from save")
	}
	if len(credits.redeemed) != 1 || credits.redeemed[0] != 1000 {
		t.Fatalf("redeemed = %v, want [1000]", credits.redeemed)
	}
	if len(credits.issued) != 1 || credits.issued[0] != 1000 {
		t.Fatalf("issued rollback = %v, want [1000]", credits.issued)
	}
	if credits.balance != 1500 {
		t.Errorf("balance = %d, want 1500 after rollback", credits.balance)
	}
	if _, ok := cctx.GetMeta("created_order_id"); ok {
		t.Error("created_order_id should not be set on save failure")
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
func (r *mockVariantRepo037) FindBySKUs(_ context.Context, skus []string) (map[string]*catalog.Variant, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make(map[string]*catalog.Variant, len(skus))
	for _, sku := range skus {
		for _, v := range r.variants {
			if v != nil && v.SKU == sku {
				out[sku] = v
				break
			}
		}
	}
	return out, nil
}
func (r *mockVariantRepo037) ListByProductID(_ context.Context, productID string, _, limit int) ([]catalog.Variant, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make([]catalog.Variant, 0)
	for _, v := range r.variants {
		if v == nil || v.ProductID != productID {
			continue
		}
		out = append(out, *v)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}
func (r *mockVariantRepo037) ListByProductIDs(ctx context.Context, productIDs []string, limitPerProduct int) (map[string][]catalog.Variant, error) {
	return testutil.ListByProductIDsFromList(ctx, r.ListByProductID, productIDs, limitPerProduct)
}

func (r *mockVariantRepo037) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (r *mockVariantRepo037) Update(_ context.Context, _ *catalog.Variant) error { return nil }
func (r *mockVariantRepo037) WithTx(_ *sql.Tx) catalog.VariantRepository         { return r }
