package cart_test

import (
	"context"
	"errors"
	"io"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ── stub implementations ────────────────────────────────────────────────

// stubCartRepo is an in-memory cart repository for tests.
type stubCartRepo struct {
	carts map[string]*domainCart.Cart
}

func newStubCartRepo() *stubCartRepo {
	return &stubCartRepo{carts: make(map[string]*domainCart.Cart)}
}

func (r *stubCartRepo) FindByID(_ context.Context, id string) (*domainCart.Cart, error) {
	c, ok := r.carts[id]
	if !ok {
		return nil, nil
	}
	return c, nil
}

func (r *stubCartRepo) FindActiveByCustomerID(_ context.Context, customerID string) (*domainCart.Cart, error) {
	for _, c := range r.carts {
		if c.CustomerID == customerID && c.IsActive() {
			return c, nil
		}
	}
	return nil, nil
}

func (r *stubCartRepo) Save(_ context.Context, c *domainCart.Cart) error {
	// Store a shallow copy so tests can verify independently.
	clone := *c
	clone.Items = make([]domainCart.Item, len(c.Items))
	copy(clone.Items, c.Items)
	r.carts[c.ID] = &clone
	return nil
}

func (r *stubCartRepo) Delete(_ context.Context, id string) error {
	delete(r.carts, id)
	return nil
}

// stubPriceRepo returns pre-set prices by variant+currency+store.
type stubPriceRepo struct {
	prices map[string]*pricing.Price // key: "variantID:currency:storeID"
}

func newStubPriceRepo() *stubPriceRepo {
	return &stubPriceRepo{prices: make(map[string]*pricing.Price)}
}

func (r *stubPriceRepo) set(variantID, currency string, amount int64) {
	r.setWithStore(variantID, currency, "", amount)
}

func (r *stubPriceRepo) setWithStore(variantID, currency, storeID string, amount int64) {
	key := variantID + ":" + currency + ":" + storeID
	p, _ := pricing.NewPrice("price-"+key, variantID, storeID, shared.MustNewMoney(amount, currency))
	r.prices[key] = &p
}

func (r *stubPriceRepo) FindByVariantCurrencyAndStore(_ context.Context, variantID, currency, storeID string) (*pricing.Price, error) {
	return r.prices[variantID+":"+currency+":"+storeID], nil
}

func (r *stubPriceRepo) ListByVariantID(_ context.Context, _ string) ([]pricing.Price, error) {
	return nil, nil
}

func (r *stubPriceRepo) List(_ context.Context, _, _ int) ([]pricing.Price, error) {
	return nil, nil
}

func (r *stubPriceRepo) Upsert(_ context.Context, _ *pricing.Price) error {
	return nil
}

// testLogger returns a silent logger for tests.
func testLogger() logger.Logger {
	return logger.NewWithWriter(io.Discard, "error")
}

// testBus returns an event bus for tests.
func testBus() *event.Bus {
	return event.NewBus(testLogger())
}

// testPipeline returns a pricing pipeline with BasePriceStep + FinalizeStep.
func testPipeline(prices pricing.PriceRepository) pricing.Pipeline {
	return pricing.NewPipeline(
		appPricing.NewBasePriceStep(prices),
		pricing.NewFinalizeStep(),
	)
}

// ── tests ───────────────────────────────────────────────────────────────

func TestService_CreateCart(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())

	c, err := svc.CreateCart(context.Background(), "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if c.ID == "" {
		t.Error("expected non-empty ID")
	}
	if c.Currency != "EUR" {
		t.Errorf("Currency = %q, want EUR", c.Currency)
	}
	if c.Status() != domainCart.CartStatusActive {
		t.Errorf("Status = %q, want active", c.Status())
	}
	if c.CustomerID != "cust-1" {
		t.Errorf("CustomerID = %q, want cust-1", c.CustomerID)
	}
	// Verify persisted.
	got, _ := carts.FindByID(context.Background(), c.ID)
	if got == nil {
		t.Error("cart not persisted")
	}
}

func TestService_CreateCart_Guest(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())

	c, err := svc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	if c.CustomerID != "" {
		t.Errorf("CustomerID = %q, want empty", c.CustomerID)
	}
	if c.Status() != domainCart.CartStatusActive {
		t.Errorf("Status = %q, want active", c.Status())
	}
	persisted, _ := carts.FindByID(context.Background(), c.ID)
	if persisted == nil {
		t.Fatal("guest cart not persisted")
	}
	if persisted.CustomerID != "" {
		t.Errorf("persisted CustomerID = %q, want empty", persisted.CustomerID)
	}
}

func TestService_CreateCart_InvalidCurrency(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())

	_, err := svc.CreateCart(context.Background(), "cust-1", "bad")
	if err == nil {
		t.Fatal("expected error for invalid currency")
	}
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Errorf("expected validation error, got: %v", err)
	}
}

func TestService_GetCart(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())

	c, _ := svc.CreateCart(context.Background(), "cust-1", "EUR")
	got, err := svc.GetCart(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("GetCart: %v", err)
	}
	if got.ID != c.ID {
		t.Errorf("ID = %q, want %q", got.ID, c.ID)
	}
}

func TestService_GetCart_NotFound(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())

	_, err := svc.GetCart(context.Background(), "no-such-id")
	if err == nil {
		t.Fatal("expected error")
	}
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected not_found, got: %v", err)
	}
}

func TestService_AddItem(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1500) // 15.00 EUR
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())
	ctx := context.Background()

	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	got, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 2)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(got.Items))
	}
	if got.Items[0].VariantID != "var-1" {
		t.Errorf("VariantID = %q, want var-1", got.Items[0].VariantID)
	}
	if got.Items[0].Quantity != 2 {
		t.Errorf("Quantity = %d, want 2", got.Items[0].Quantity)
	}
	if got.Items[0].UnitPrice.Amount() != 1500 {
		t.Errorf("UnitPrice = %d, want 1500", got.Items[0].UnitPrice.Amount())
	}
}

func TestService_AddItem_CartNotFound(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())

	_, err := svc.AddItem(context.Background(), "no-cart", "cust-1", "var-1", 1)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected not_found, got: %v", err)
	}
}

func TestService_AddItem_NoPriceForVariant(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())
	ctx := context.Background()

	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	_, err := svc.AddItem(ctx, c.ID, "cust-1", "var-no-price", 1)
	if err == nil {
		t.Fatal("expected error for missing price")
	}
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Errorf("expected validation error, got: %v", err)
	}
}

func TestService_AddItem_MergesQuantity(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())
	ctx := context.Background()

	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	svc.AddItem(ctx, c.ID, "cust-1", "var-1", 2)
	got, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 3)
	if err != nil {
		t.Fatalf("AddItem merge: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(got.Items))
	}
	if got.Items[0].Quantity != 5 {
		t.Errorf("Quantity = %d, want 5 (2+3)", got.Items[0].Quantity)
	}
}

func TestService_UpdateItemQuantity(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1500)
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())
	ctx := context.Background()

	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	svc.AddItem(ctx, c.ID, "cust-1", "var-1", 2)

	got, err := svc.UpdateItemQuantity(ctx, c.ID, "cust-1", "var-1", 5)
	if err != nil {
		t.Fatalf("UpdateItemQuantity: %v", err)
	}
	if got.Items[0].Quantity != 5 {
		t.Errorf("Quantity = %d, want 5", got.Items[0].Quantity)
	}
}

func TestService_UpdateItemQuantity_CartNotFound(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())

	_, err := svc.UpdateItemQuantity(context.Background(), "no-cart", "cust-1", "var-1", 1)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected not_found, got: %v", err)
	}
}

func TestService_UpdateItemQuantity_ItemNotFound(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())
	ctx := context.Background()

	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	_, err := svc.UpdateItemQuantity(ctx, c.ID, "cust-1", "var-1", 1)
	if err == nil {
		t.Fatal("expected error for non-existent item")
	}
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Errorf("expected validation error, got: %v", err)
	}
}

func TestService_RemoveItem(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())
	ctx := context.Background()

	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1)

	got, err := svc.RemoveItem(ctx, c.ID, "cust-1", "var-1")
	if err != nil {
		t.Fatalf("RemoveItem: %v", err)
	}
	if len(got.Items) != 0 {
		t.Errorf("len(Items) = %d, want 0", len(got.Items))
	}
}

func TestService_RemoveItem_CartNotFound(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())

	_, err := svc.RemoveItem(context.Background(), "no-cart", "cust-1", "var-1")
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected not_found, got: %v", err)
	}
}

func TestService_RemoveItem_ItemNotFound(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())
	ctx := context.Background()

	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	_, err := svc.RemoveItem(ctx, c.ID, "cust-1", "var-x")
	if err == nil {
		t.Fatal("expected error for non-existent item")
	}
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Errorf("expected validation error, got: %v", err)
	}
}

func TestService_GetActiveCartByCustomer(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())
	ctx := context.Background()

	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	c.SetCustomerID("cust-1")
	carts.Save(ctx, c)

	got, err := svc.GetActiveCartByCustomer(ctx, "cust-1")
	if err != nil {
		t.Fatalf("GetActiveCartByCustomer: %v", err)
	}
	if got.ID != c.ID {
		t.Errorf("ID = %q, want %q", got.ID, c.ID)
	}
}

func TestService_GetActiveCartByCustomer_NotFound(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())

	_, err := svc.GetActiveCartByCustomer(context.Background(), "no-customer")
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Errorf("expected not_found, got: %v", err)
	}
}

func TestService_RecalculateUpdatesPrices(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	prices.set("var-2", "EUR", 2500)
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())
	ctx := context.Background()

	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1)
	got, _ := svc.AddItem(ctx, c.ID, "cust-1", "var-2", 3)

	// Verify prices were set by the pipeline.
	if got.Items[0].UnitPrice.Amount() != 1000 {
		t.Errorf("Items[0].UnitPrice = %d, want 1000", got.Items[0].UnitPrice.Amount())
	}
	if got.Items[1].UnitPrice.Amount() != 2500 {
		t.Errorf("Items[1].UnitPrice = %d, want 2500", got.Items[1].UnitPrice.Amount())
	}
}

// errorCartRepo wraps a stubCartRepo and injects a Save error.
type errorCartRepo struct {
	*stubCartRepo
	saveErr error
}

func (r *errorCartRepo) Save(_ context.Context, _ *domainCart.Cart) error {
	return r.saveErr
}

func TestService_AddItem_SaveError(t *testing.T) {
	inner := newStubCartRepo()
	carts := &errorCartRepo{stubCartRepo: inner, saveErr: errors.New("db down")}
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), testBus())
	ctx := context.Background()

	// Create the cart directly in the inner repo.
	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	inner.carts["cart-1"] = &c

	_, err := svc.AddItem(ctx, "cart-1", "cust-1", "var-1", 1)
	if err == nil {
		t.Fatal("expected save error")
	}
}

// ── event emission tests ────────────────────────────────────────────────

func TestService_AddItem_EmitsEvent(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1500)
	bus := testBus()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), bus)
	ctx := context.Background()

	var captured event.Event
	bus.On(domainCart.EventItemAdded, func(_ context.Context, evt event.Event) error {
		captured = evt
		return nil
	})

	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	svc.AddItem(ctx, c.ID, "cust-1", "var-1", 2)

	if captured.Name != domainCart.EventItemAdded {
		t.Fatalf("event name = %q, want %q", captured.Name, domainCart.EventItemAdded)
	}
	data, ok := captured.Data.(domainCart.ItemAddedData)
	if !ok {
		t.Fatalf("event data type = %T, want ItemAddedData", captured.Data)
	}
	if data.CartID != c.ID {
		t.Errorf("CartID = %q, want %q", data.CartID, c.ID)
	}
	if data.VariantID != "var-1" {
		t.Errorf("VariantID = %q, want var-1", data.VariantID)
	}
	if data.Quantity != 2 {
		t.Errorf("Quantity = %d, want 2", data.Quantity)
	}
}

func TestService_UpdateItemQuantity_EmitsEvent(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	bus := testBus()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), bus)
	ctx := context.Background()

	var captured event.Event
	bus.On(domainCart.EventItemUpdated, func(_ context.Context, evt event.Event) error {
		captured = evt
		return nil
	})

	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1)
	svc.UpdateItemQuantity(ctx, c.ID, "cust-1", "var-1", 5)

	if captured.Name != domainCart.EventItemUpdated {
		t.Fatalf("event name = %q, want %q", captured.Name, domainCart.EventItemUpdated)
	}
	data := captured.Data.(domainCart.ItemUpdatedData)
	if data.Quantity != 5 {
		t.Errorf("Quantity = %d, want 5", data.Quantity)
	}
}

func TestService_RemoveItem_EmitsEvent(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	bus := testBus()
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), bus)
	ctx := context.Background()

	var captured event.Event
	bus.On(domainCart.EventItemRemoved, func(_ context.Context, evt event.Event) error {
		captured = evt
		return nil
	})

	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1)
	svc.RemoveItem(ctx, c.ID, "cust-1", "var-1")

	if captured.Name != domainCart.EventItemRemoved {
		t.Fatalf("event name = %q, want %q", captured.Name, domainCart.EventItemRemoved)
	}
	data := captured.Data.(domainCart.ItemRemovedData)
	if data.CartID != c.ID {
		t.Errorf("CartID = %q, want %q", data.CartID, c.ID)
	}
	if data.VariantID != "var-1" {
		t.Errorf("VariantID = %q, want var-1", data.VariantID)
	}
}

// ── publish-error-ignored tests ─────────────────────────────────────────

func TestService_AddItem_PublishError_Ignored(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1500)
	bus := testBus()
	bus.On(domainCart.EventItemAdded, func(_ context.Context, _ event.Event) error {
		return errors.New("publish boom")
	})
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), bus)
	ctx := context.Background()

	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	got, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 2)
	if err != nil {
		t.Fatalf("AddItem should succeed despite publish error: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Quantity != 2 {
		t.Errorf("cart state incorrect after publish error")
	}
	// Verify persisted.
	persisted, _ := carts.FindByID(ctx, c.ID)
	if persisted == nil || len(persisted.Items) != 1 {
		t.Error("cart not persisted after publish error")
	}
}

func TestService_UpdateItemQuantity_PublishError_Ignored(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	bus := testBus()
	bus.On(domainCart.EventItemUpdated, func(_ context.Context, _ event.Event) error {
		return errors.New("publish boom")
	})
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), bus)
	ctx := context.Background()

	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1)
	got, err := svc.UpdateItemQuantity(ctx, c.ID, "cust-1", "var-1", 5)
	if err != nil {
		t.Fatalf("UpdateItemQuantity should succeed despite publish error: %v", err)
	}
	if got.Items[0].Quantity != 5 {
		t.Errorf("Quantity = %d, want 5", got.Items[0].Quantity)
	}
}

func TestService_RemoveItem_PublishError_Ignored(t *testing.T) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	prices.set("var-1", "EUR", 1000)
	bus := testBus()
	bus.On(domainCart.EventItemRemoved, func(_ context.Context, _ event.Event) error {
		return errors.New("publish boom")
	})
	svc := cartApp.NewService(carts, prices, nil, nil, testPipeline(prices), testLogger(), bus)
	ctx := context.Background()

	c, _ := svc.CreateCart(ctx, "cust-1", "EUR")
	svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1)
	got, err := svc.RemoveItem(ctx, c.ID, "cust-1", "var-1")
	if err != nil {
		t.Fatalf("RemoveItem should succeed despite publish error: %v", err)
	}
	if len(got.Items) != 0 {
		t.Errorf("len(Items) = %d, want 0", len(got.Items))
	}
}
