package cart_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
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
func (mutableVariantRepo) FindBySKU(context.Context, string) (*catalog.Variant, error) { return nil, nil }
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
func (mutableProductRepo) FindBySlug(context.Context, string) (*catalog.Product, error) { return nil, nil }
func (mutableProductRepo) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (mutableProductRepo) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (mutableProductRepo) Create(context.Context, *catalog.Product) error { return nil }
func (mutableProductRepo) Update(context.Context, *catalog.Product) error { return nil }

type trackingExtWriter struct {
	deleted []string
	copied  int
}

func (w *trackingExtWriter) ValidateBatch(context.Context, domainext.Target, []domainext.ValueInput, bool) error {
	return nil
}
func (w *trackingExtWriter) UpsertBatch(context.Context, domainext.Target, []domainext.ValueInput, string, bool) ([]domainext.Value, error) {
	return nil, nil
}
func (w *trackingExtWriter) DeleteAllForTarget(_ context.Context, target domainext.Target) error {
	w.deleted = append(w.deleted, target.ID)
	return nil
}
func (w *trackingExtWriter) CopyTarget(context.Context, domainext.Target, domainext.Target, string) error {
	w.copied++
	return nil
}

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

func TestService_ClaimGuestCart_AssignRejectsArchived(t *testing.T) {
	cat := newMutableCatalog()
	seedActiveLine(t, cat, "var-1", "prod-1")
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	svc := cartApp.NewService(carts, prices, nil, nil, mutableVariantRepo{cat}, mutableProductRepo{cat}, testPipeline(prices), testLogger(), testBus(), nil, nil)
	ctx := context.Background()

	guest, err := svc.CreateCart(ctx, "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, guest.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	cat.archive("prod-1")

	_, err = svc.ClaimGuestCart(ctx, guest.ID, "cust-1")
	if err == nil {
		t.Fatal("ClaimGuestCart assign: expected error for archived product")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeValidation {
		t.Fatalf("err = %v, want validation", err)
	}
}

func TestService_ClaimGuestCart_MergeValidatesBeforeExtensionMutations(t *testing.T) {
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

	_, err = svc.ClaimGuestCart(ctx, guest.ID, "cust-1")
	if err == nil {
		t.Fatal("ClaimGuestCart merge: expected error for archived guest line")
	}
	if ext.copied != 0 || len(ext.deleted) != 0 {
		t.Fatalf("extension mutations before validation fail: copied=%d deleted=%v", ext.copied, ext.deleted)
	}
	stillGuest, err := svc.GetCart(ctx, guest.ID, "")
	if err != nil {
		t.Fatalf("GetCart guest: %v", err)
	}
	if stillGuest.ItemCount() != 2 {
		t.Fatalf("guest item count = %d, want 2 (unchanged)", stillGuest.ItemCount())
	}
	cust, err := svc.GetCart(ctx, customer.ID, "cust-1")
	if err != nil {
		t.Fatalf("GetCart customer: %v", err)
	}
	if cust.ItemCount() != 0 {
		t.Fatalf("customer item count = %d, want 0", cust.ItemCount())
	}
}
