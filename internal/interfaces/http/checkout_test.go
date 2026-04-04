package http_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	checkoutApp "github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"

	appPricing "github.com/akarso/shopanda/internal/application/pricing"
)

// ── stubs (checkout-specific) ───────────────────────────────────────────

type stubCheckoutCartRepo struct {
	carts map[string]*cart.Cart
}

func newStubCheckoutCartRepo() *stubCheckoutCartRepo {
	return &stubCheckoutCartRepo{carts: make(map[string]*cart.Cart)}
}

func (r *stubCheckoutCartRepo) FindByID(_ context.Context, cid string) (*cart.Cart, error) {
	c, ok := r.carts[cid]
	if !ok {
		return nil, nil
	}
	return c, nil
}

func (r *stubCheckoutCartRepo) FindActiveByCustomerID(_ context.Context, _ string) (*cart.Cart, error) {
	return nil, nil
}

func (r *stubCheckoutCartRepo) Save(_ context.Context, c *cart.Cart) error {
	r.carts[c.ID] = c
	return nil
}

func (r *stubCheckoutCartRepo) Delete(_ context.Context, cid string) error {
	delete(r.carts, cid)
	return nil
}

// stubCheckoutVariantRepo ────────────────────────────────────────────────

type stubCheckoutVariantRepo struct {
	variants map[string]*catalog.Variant
}

func newStubCheckoutVariantRepo() *stubCheckoutVariantRepo {
	return &stubCheckoutVariantRepo{variants: make(map[string]*catalog.Variant)}
}

func (r *stubCheckoutVariantRepo) set(vid, pid, sku, name string) {
	v, _ := catalog.NewVariant(vid, pid, sku)
	v.Name = name
	r.variants[vid] = &v
}

func (r *stubCheckoutVariantRepo) FindByID(_ context.Context, vid string) (*catalog.Variant, error) {
	v, ok := r.variants[vid]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (r *stubCheckoutVariantRepo) FindBySKU(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (r *stubCheckoutVariantRepo) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return nil, nil
}
func (r *stubCheckoutVariantRepo) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (r *stubCheckoutVariantRepo) Update(_ context.Context, _ *catalog.Variant) error { return nil }
func (r *stubCheckoutVariantRepo) WithTx(_ *sql.Tx) catalog.VariantRepository {
	return r
}

// stubCheckoutReservationRepo ────────────────────────────────────────────

type stubCheckoutReservationRepo struct{}

func (r *stubCheckoutReservationRepo) Reserve(_ context.Context, _ *inventory.Reservation) error {
	return nil
}
func (r *stubCheckoutReservationRepo) Release(_ context.Context, _ string) error { return nil }
func (r *stubCheckoutReservationRepo) Confirm(_ context.Context, _ string) error { return nil }
func (r *stubCheckoutReservationRepo) FindByID(_ context.Context, _ string) (*inventory.Reservation, error) {
	return nil, nil
}
func (r *stubCheckoutReservationRepo) ListActiveByVariantID(_ context.Context, _ string) ([]inventory.Reservation, error) {
	return nil, nil
}
func (r *stubCheckoutReservationRepo) ReleaseExpiredBefore(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

// stubCheckoutOrderRepo ─────────────────────────────────────────────────

type stubCheckoutOrderRepo struct {
	saved *order.Order
}

func (r *stubCheckoutOrderRepo) FindByID(_ context.Context, _ string) (*order.Order, error) {
	return nil, nil
}
func (r *stubCheckoutOrderRepo) FindByCustomerID(_ context.Context, _ string) ([]order.Order, error) {
	return nil, nil
}
func (r *stubCheckoutOrderRepo) List(_ context.Context, _, _ int) ([]order.Order, error) {
	return nil, nil
}
func (r *stubCheckoutOrderRepo) Save(_ context.Context, o *order.Order) error {
	r.saved = o
	return nil
}
func (r *stubCheckoutOrderRepo) UpdateStatus(_ context.Context, _ *order.Order) error { return nil }

// stubCheckoutPriceRepo ─────────────────────────────────────────────────

type stubCheckoutPriceRepo struct {
	prices map[string]*pricing.Price
}

func newStubCheckoutPriceRepo() *stubCheckoutPriceRepo {
	return &stubCheckoutPriceRepo{prices: make(map[string]*pricing.Price)}
}

func (r *stubCheckoutPriceRepo) set(variantID, currency string, amount int64) {
	key := variantID + ":" + currency
	p, _ := pricing.NewPrice("price-"+key, variantID, shared.MustNewMoney(amount, currency))
	r.prices[key] = &p
}

func (r *stubCheckoutPriceRepo) FindByVariantAndCurrency(_ context.Context, variantID, currency string) (*pricing.Price, error) {
	return r.prices[variantID+":"+currency], nil
}

func (r *stubCheckoutPriceRepo) ListByVariantID(_ context.Context, _ string) ([]pricing.Price, error) {
	return nil, nil
}

func (r *stubCheckoutPriceRepo) Upsert(_ context.Context, _ *pricing.Price) error { return nil }

// ── helpers ─────────────────────────────────────────────────────────────

func checkoutTestLogger() logger.Logger {
	return logger.NewWithWriter(io.Discard, "error")
}

func checkoutSetup() (*stubCheckoutCartRepo, *stubCheckoutVariantRepo, *stubCheckoutPriceRepo, *http.ServeMux) {
	carts := newStubCheckoutCartRepo()
	variants := newStubCheckoutVariantRepo()
	prices := newStubCheckoutPriceRepo()
	reservations := &stubCheckoutReservationRepo{}
	orders := &stubCheckoutOrderRepo{}
	log := checkoutTestLogger()
	bus := event.NewBus(log)

	pipeline := pricing.NewPipeline(
		appPricing.NewBasePriceStep(prices),
		pricing.NewFinalizeStep(),
	)

	validateStep := checkoutApp.NewValidateCartStep(variants)
	pricingStep := checkoutApp.NewRecalculatePricingStep(pipeline)
	reserveStep := checkoutApp.NewReserveInventoryStep(reservations)
	createOrderStep := checkoutApp.NewCreateOrderStep(orders, variants)

	workflow := checkoutApp.NewWorkflow([]checkoutApp.Step{
		validateStep,
		pricingStep,
		reserveStep,
		createOrderStep,
	}, bus, log)

	svc := checkoutApp.NewService(carts, workflow, log)
	handler := shophttp.NewCheckoutHandler(svc)

	requireAuth := shophttp.RequireAuth()
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/checkout", requireAuth(handler.StartCheckout()))
	return carts, variants, prices, mux
}

func seedCheckoutCart(carts *stubCheckoutCartRepo, variants *stubCheckoutVariantRepo, prices *stubCheckoutPriceRepo) string {
	cartID := id.New()
	c, _ := cart.NewCart(cartID, "EUR")
	_ = c.SetCustomerID("cust-1")
	_ = c.AddItem("var-1", 2, shared.MustNewMoney(1500, "EUR"))
	carts.Save(context.Background(), &c)

	variants.set("var-1", "prod-1", "SKU-VAR1", "Widget A")
	prices.set("var-1", "EUR", 1500)

	return cartID
}

func parseCheckoutBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return body
}

// ── tests ───────────────────────────────────────────────────────────────

func TestCheckoutHandler_StartCheckout_OK(t *testing.T) {
	carts, variants, prices, mux := checkoutSetup()
	cartID := seedCheckoutCart(carts, variants, prices)

	body := `{"cart_id":"` + cartID + `"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/checkout", strings.NewReader(body))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	resp := parseCheckoutBody(t, rec)
	data := resp["data"].(map[string]interface{})
	o := data["order"].(map[string]interface{})

	if o["customer_id"] != "cust-1" {
		t.Errorf("customer_id = %v, want cust-1", o["customer_id"])
	}
	if o["status"] != "pending" {
		t.Errorf("status = %v, want pending", o["status"])
	}
	if o["currency"] != "EUR" {
		t.Errorf("currency = %v, want EUR", o["currency"])
	}

	items := o["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	item := items[0].(map[string]interface{})
	if item["variant_id"] != "var-1" {
		t.Errorf("item variant_id = %v, want var-1", item["variant_id"])
	}
	if item["sku"] != "SKU-VAR1" {
		t.Errorf("item sku = %v, want SKU-VAR1", item["sku"])
	}
	if item["name"] != "Widget A" {
		t.Errorf("item name = %v, want Widget A", item["name"])
	}
	// qty=2, unitPrice=1500
	if item["unit_price"].(float64) != 1500 {
		t.Errorf("unit_price = %v, want 1500", item["unit_price"])
	}
	if item["quantity"].(float64) != 2 {
		t.Errorf("quantity = %v, want 2", item["quantity"])
	}

	// total_amount = 2 * 1500 = 3000
	if o["total_amount"].(float64) != 3000 {
		t.Errorf("total_amount = %v, want 3000", o["total_amount"])
	}

	if o["id"] == nil || o["id"] == "" {
		t.Error("order id should not be empty")
	}
	if o["created_at"] == nil || o["created_at"] == "" {
		t.Error("created_at should not be empty")
	}
}

func TestCheckoutHandler_StartCheckout_MissingCartID(t *testing.T) {
	_, _, _, mux := checkoutSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/checkout", strings.NewReader(`{}`))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestCheckoutHandler_StartCheckout_InvalidBody(t *testing.T) {
	_, _, _, mux := checkoutSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/checkout", strings.NewReader(`not json`))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCheckoutHandler_StartCheckout_CartNotFound(t *testing.T) {
	_, _, _, mux := checkoutSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/checkout", strings.NewReader(`{"cart_id":"no-such"}`))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestCheckoutHandler_StartCheckout_WrongCustomer(t *testing.T) {
	carts, variants, prices, mux := checkoutSetup()
	cartID := seedCheckoutCart(carts, variants, prices)

	body := `{"cart_id":"` + cartID + `"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/checkout", strings.NewReader(body))
	req = testhelper.CustomerRequest(req, "other-customer")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestCheckoutHandler_StartCheckout_EmptyCart(t *testing.T) {
	carts, _, _, mux := checkoutSetup()

	// Cart with no items.
	c, _ := cart.NewCart("empty-cart", "EUR")
	_ = c.SetCustomerID("cust-1")
	carts.Save(context.Background(), &c)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/checkout", strings.NewReader(`{"cart_id":"empty-cart"}`))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestCheckoutHandler_StartCheckout_Unauthenticated(t *testing.T) {
	_, _, _, mux := checkoutSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/checkout", strings.NewReader(`{"cart_id":"cart-1"}`))
	// No auth set — guest identity.
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
