package cart_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// mutableCatalog lets tests activate then archive products after AddItem.
type mutableCatalog struct {
	mu       sync.Mutex
	variants map[string]*catalog.Variant
	products map[string]*catalog.Product
}

func newMutableCatalog() *mutableCatalog {
	return &mutableCatalog{
		variants: map[string]*catalog.Variant{},
		products: map[string]*catalog.Product{},
	}
}

func (m *mutableCatalog) setVariant(v *catalog.Variant) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.variants[v.ID] = v
}

func (m *mutableCatalog) setProduct(p *catalog.Product) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.products[p.ID] = p
}

func (m *mutableCatalog) archive(productID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.products[productID]; ok {
		cp := *p
		cp.Status = catalog.StatusArchived
		m.products[productID] = &cp
	}
}

func (m *mutableCatalog) FindVariant(_ context.Context, id string) (*catalog.Variant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.variants[id], nil
}

func (m *mutableCatalog) FindProduct(_ context.Context, id string) (*catalog.Product, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.products[id], nil
}

type mutableVariantRepo struct{ cat *mutableCatalog }

func (r mutableVariantRepo) FindByID(ctx context.Context, id string) (*catalog.Variant, error) {
	return r.cat.FindVariant(ctx, id)
}
func (mutableVariantRepo) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (mutableVariantRepo) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return nil, nil
}
func (mutableVariantRepo) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (mutableVariantRepo) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (mutableVariantRepo) Create(context.Context, *catalog.Variant) error { return nil }
func (mutableVariantRepo) Update(context.Context, *catalog.Variant) error { return nil }

type mutableProductRepo struct{ cat *mutableCatalog }

func (r mutableProductRepo) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	return r.cat.FindProduct(ctx, id)
}
func (mutableProductRepo) FindBySlug(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (mutableProductRepo) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (mutableProductRepo) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (mutableProductRepo) Create(context.Context, *catalog.Product) error { return nil }
func (mutableProductRepo) Update(context.Context, *catalog.Product) error { return nil }

func seedActiveLine(t *testing.T, cat *mutableCatalog, variantID, productID string) {
	t.Helper()
	cat.setProduct(&catalog.Product{ID: productID, Status: catalog.StatusActive})
	cat.setVariant(&catalog.Variant{ID: variantID, ProductID: productID})
}

func TestService_UpdateItemQuantity_RejectsIncreaseAfterArchive(t *testing.T) {
	cat := newMutableCatalog()
	seedActiveLine(t, cat, "var-1", "prod-1")
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	svc := cartApp.NewService(carts, prices, nil, nil, mutableVariantRepo{cat}, mutableProductRepo{cat}, testPipeline(prices), testLogger(), testBus(), nil, nil)
	ctx := context.Background()

	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 2, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	cat.archive("prod-1")

	_, err = svc.UpdateItemQuantity(ctx, c.ID, "cust-1", "var-1", 3)
	if err == nil {
		t.Fatal("UpdateItemQuantity increase: expected error after archive")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeValidation {
		t.Fatalf("err = %v, want validation", err)
	}

	got, err := svc.UpdateItemQuantity(ctx, c.ID, "cust-1", "var-1", 1)
	if err != nil {
		t.Fatalf("UpdateItemQuantity decrease after archive: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Quantity != 1 {
		t.Fatalf("items = %+v, want qty 1", got.Items)
	}
}

func TestService_RemoveItem_AllowsUnavailableLine(t *testing.T) {
	cat := newMutableCatalog()
	seedActiveLine(t, cat, "var-1", "prod-1")
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	svc := cartApp.NewService(carts, prices, nil, nil, mutableVariantRepo{cat}, mutableProductRepo{cat}, testPipeline(prices), testLogger(), testBus(), nil, nil)
	ctx := context.Background()

	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	cat.archive("prod-1")

	got, err := svc.RemoveItem(ctx, c.ID, "cust-1", "var-1")
	if err != nil {
		t.Fatalf("RemoveItem archived line: %v", err)
	}
	if len(got.Items) != 0 {
		t.Fatalf("len(items) = %d, want 0", len(got.Items))
	}
}

func TestService_ClaimGuestCart_AssignDropsArchivedLine(t *testing.T) {
	cat := newMutableCatalog()
	seedActiveLine(t, cat, "var-1", "prod-1")
	seedActiveLine(t, cat, "var-2", "prod-later-archive")
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	prices.set("var-2", "EUR", 2000)
	ext := &trackingExtWriter{}
	svc := cartApp.NewService(carts, prices, nil, nil, mutableVariantRepo{cat}, mutableProductRepo{cat}, testPipeline(prices), testLogger(), testBus(), ext, nil)
	ctx := context.Background()

	guest, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	for _, vid := range []string{"var-1", "var-2"} {
		if _, err := svc.AddItem(ctx, guest.ID, "", vid, 1, cartApp.AddItemOptions{}); err != nil {
			t.Fatalf("AddItem %s: %v", vid, err)
		}
	}
	cat.archive("prod-later-archive")

	claimed, err := svc.ClaimGuestCart(ctx, guest.ID, "cust-1")
	if err != nil {
		t.Fatalf("ClaimGuestCart assign: %v", err)
	}
	if claimed.CustomerID != "cust-1" {
		t.Fatalf("customer id = %q, want cust-1", claimed.CustomerID)
	}
	if len(claimed.Items) != 1 || claimed.Items[0].VariantID != "var-1" {
		t.Fatalf("items = %+v, want only var-1", claimed.Items)
	}
	if len(ext.orphanPurges) != 1 {
		t.Fatalf("orphan extension purges = %+v, want one after assign cleanup", ext.orphanPurges)
	}
	if ext.orphanPurges[0].cartID != guest.ID {
		t.Fatalf("orphan purge cart = %q, want %q", ext.orphanPurges[0].cartID, guest.ID)
	}
	if len(ext.orphanPurges[0].keepVariantIDs) != 1 || ext.orphanPurges[0].keepVariantIDs[0] != "var-1" {
		t.Fatalf("orphan purge keep = %v, want only var-1", ext.orphanPurges[0].keepVariantIDs)
	}
}

// failNthSaveCartRepo lets CreateCart/AddItem succeed, then fails the Nth
// subsequent Save (1 = next Save). Used to simulate claim Save conflict.
type failNthSaveCartRepo struct {
	*stubCartRepo
	failOn  int
	saveN   int
	saveErr error
}

func (r *failNthSaveCartRepo) Save(ctx context.Context, c *domainCart.Cart) error {
	r.saveN++
	if r.failOn > 0 && r.saveN == r.failOn {
		return r.saveErr
	}
	return r.stubCartRepo.Save(ctx, c)
}

func TestService_ClaimGuestCart_AssignSaveFails_PreservesDroppedLineExtensions(t *testing.T) {
	cat := newMutableCatalog()
	seedActiveLine(t, cat, "var-1", "prod-1")
	seedActiveLine(t, cat, "var-2", "prod-later-archive")
	inner := newStubCartRepo()
	carts := &failNthSaveCartRepo{stubCartRepo: inner, saveErr: errors.New("save conflict")}
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	prices.set("var-2", "EUR", 2000)
	ext := &trackingExtWriter{}
	svc := cartApp.NewService(carts, prices, nil, nil, mutableVariantRepo{cat}, mutableProductRepo{cat}, testPipeline(prices), testLogger(), testBus(), ext, nil)
	ctx := context.Background()

	guest, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	for _, vid := range []string{"var-1", "var-2"} {
		if _, err := svc.AddItem(ctx, guest.ID, "", vid, 1, cartApp.AddItemOptions{}); err != nil {
			t.Fatalf("AddItem %s: %v", vid, err)
		}
	}
	// CreateCart + 2×AddItem = 3 saves so far; fail the next (claim) Save.
	carts.failOn = carts.saveN + 1
	cat.archive("prod-later-archive")

	if _, err := svc.ClaimGuestCart(ctx, guest.ID, "cust-1"); err == nil {
		t.Fatal("ClaimGuestCart: expected save error")
	}
	if len(ext.deleted) != 0 {
		t.Fatalf("deleted extension targets = %v, want none when claim Save fails", ext.deleted)
	}
	stillGuest, err := svc.GetCart(ctx, guest.ID, "")
	if err != nil {
		t.Fatalf("GetCart: %v", err)
	}
	if stillGuest.CustomerID != "" {
		t.Fatalf("guest claimed despite Save failure: customer=%q", stillGuest.CustomerID)
	}
	if stillGuest.ItemCount() != 2 {
		t.Fatalf("guest items = %d, want 2 (dropped line must remain for retry)", stillGuest.ItemCount())
	}
}

func TestService_ClaimGuestCart_MergeSaveFails_PreservesGuestLineExtensions(t *testing.T) {
	cat := newMutableCatalog()
	seedActiveLine(t, cat, "var-1", "prod-active")
	seedActiveLine(t, cat, "var-2", "prod-later-archive")
	inner := newStubCartRepo()
	carts := &failNthSaveCartRepo{stubCartRepo: inner, saveErr: errors.New("save conflict")}
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	prices.set("var-2", "EUR", 2000)
	ext := &trackingExtWriter{}
	svc := cartApp.NewService(carts, prices, nil, nil, mutableVariantRepo{cat}, mutableProductRepo{cat}, testPipeline(prices), testLogger(), testBus(), ext, nil)
	ctx := context.Background()

	customer, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart customer: %v", err)
	}
	guest, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	for _, vid := range []string{"var-1", "var-2"} {
		if _, err := svc.AddItem(ctx, guest.ID, "", vid, 1, cartApp.AddItemOptions{}); err != nil {
			t.Fatalf("AddItem %s: %v", vid, err)
		}
	}
	carts.failOn = carts.saveN + 1
	cat.archive("prod-later-archive")

	if _, err := svc.ClaimGuestCart(ctx, guest.ID, "cust-1"); err == nil {
		t.Fatal("ClaimGuestCart merge: expected save error")
	}
	if len(ext.deleted) != 0 {
		t.Fatalf("deleted guest extension targets = %v, want none when merge Save fails", ext.deleted)
	}
	stillGuest, err := svc.GetCart(ctx, guest.ID, "")
	if err != nil {
		t.Fatalf("GetCart guest: %v", err)
	}
	if stillGuest.ItemCount() != 2 {
		t.Fatalf("guest items = %d, want 2 unchanged for retry", stillGuest.ItemCount())
	}
	stillCustomer, err := svc.GetCart(ctx, customer.ID, "cust-1")
	if err != nil {
		t.Fatalf("GetCart customer: %v", err)
	}
	if stillCustomer.ItemCount() != 0 {
		t.Fatalf("customer items = %d, want 0 when merge Save fails", stillCustomer.ItemCount())
	}
}

func TestService_ClaimGuestCart_InfraErrorStillFails(t *testing.T) {
	cat := newMutableCatalog()
	seedActiveLine(t, cat, "var-1", "prod-1")
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	facts := &erroringFacts{}
	svc := cartApp.NewService(carts, prices, nil, nil, mutableVariantRepo{cat}, mutableProductRepo{cat}, testPipeline(prices), testLogger(), testBus(), nil, nil)
	ctx := context.Background()

	guest, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, guest.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	svc.WithVisibilityFacts(facts, catalog.VisibilityOptions{})

	if _, err := svc.ClaimGuestCart(ctx, guest.ID, "cust-1"); err == nil {
		t.Fatal("ClaimGuestCart: expected facts load error to abort the claim")
	}
	stillGuest, err := svc.GetCart(ctx, guest.ID, "")
	if err != nil {
		t.Fatalf("GetCart guest: %v", err)
	}
	if stillGuest.CustomerID != "" || stillGuest.ItemCount() != 1 {
		t.Fatalf("guest cart mutated on infra error: customer=%q items=%d", stillGuest.CustomerID, stillGuest.ItemCount())
	}
}

func TestService_ClaimGuestCart_MergeDropsArchivedLine(t *testing.T) {
	cat := newMutableCatalog()
	seedActiveLine(t, cat, "var-1", "prod-active")
	seedActiveLine(t, cat, "var-2", "prod-later-archive")
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	prices.set("var-2", "EUR", 2000)
	ext := &trackingExtWriter{}
	svc := cartApp.NewService(carts, prices, nil, nil, mutableVariantRepo{cat}, mutableProductRepo{cat}, testPipeline(prices), testLogger(), testBus(), ext, nil)
	ctx := context.Background()

	customer, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart customer: %v", err)
	}
	guest, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	if _, err := svc.AddItem(ctx, guest.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem var-1: %v", err)
	}
	if _, err := svc.AddItem(ctx, guest.ID, "", "var-2", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem var-2: %v", err)
	}
	cat.archive("prod-later-archive")

	merged, err := svc.ClaimGuestCart(ctx, guest.ID, "cust-1")
	if err != nil {
		t.Fatalf("ClaimGuestCart merge: %v", err)
	}
	if merged.ID != customer.ID {
		t.Fatalf("merged cart = %q, want customer cart %q", merged.ID, customer.ID)
	}
	if len(merged.Items) != 1 || merged.Items[0].VariantID != "var-1" {
		t.Fatalf("items = %+v, want only var-1", merged.Items)
	}
	if ext.copied != 1 {
		t.Fatalf("extension copies = %d, want 1 (var-1 only)", ext.copied)
	}
	if len(ext.deleted) != 2 {
		t.Fatalf("deleted guest extension targets = %v, want both guest lines", ext.deleted)
	}
	if _, err := svc.GetCart(ctx, guest.ID, ""); !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("guest cart after merge: err = %v, want not found", err)
	}
}
