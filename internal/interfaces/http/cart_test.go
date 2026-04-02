package http_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ── stubs (cart-specific) ───────────────────────────────────────────────

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

type stubPriceRepo struct {
	prices map[string]*pricing.Price
}

func newStubPriceRepo() *stubPriceRepo {
	return &stubPriceRepo{prices: make(map[string]*pricing.Price)}
}

func (r *stubPriceRepo) set(variantID, currency string, amount int64) {
	key := variantID + ":" + currency
	p, _ := pricing.NewPrice("price-"+key, variantID, shared.MustNewMoney(amount, currency))
	r.prices[key] = &p
}

func (r *stubPriceRepo) FindByVariantAndCurrency(_ context.Context, variantID, currency string) (*pricing.Price, error) {
	return r.prices[variantID+":"+currency], nil
}

func (r *stubPriceRepo) ListByVariantID(_ context.Context, _ string) ([]pricing.Price, error) {
	return nil, nil
}

func (r *stubPriceRepo) Upsert(_ context.Context, _ *pricing.Price) error {
	return nil
}

func cartTestLogger() logger.Logger {
	return logger.NewWithWriter(io.Discard, "error")
}

func cartTestPipeline(prices pricing.PriceRepository) pricing.Pipeline {
	return pricing.NewPipeline(
		appPricing.NewBasePriceStep(prices),
		pricing.NewFinalizeStep(),
	)
}

func newCartRouter(h *shophttp.CartHandler) *http.ServeMux {
	requireAuth := shophttp.RequireAuth()
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/carts", requireAuth(h.Create()))
	mux.Handle("GET /api/v1/carts/{cartId}", requireAuth(h.Get()))
	mux.Handle("POST /api/v1/carts/{cartId}/items", requireAuth(h.AddItem()))
	mux.Handle("PUT /api/v1/carts/{cartId}/items/{variantId}", requireAuth(h.UpdateItem()))
	mux.Handle("DELETE /api/v1/carts/{cartId}/items/{variantId}", requireAuth(h.RemoveItem()))
	return mux
}

func cartSetup() (*stubCartRepo, *stubPriceRepo, *shophttp.CartHandler, *http.ServeMux) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	bus := event.NewBus(cartTestLogger())
	svc := cartApp.NewService(carts, prices, cartTestPipeline(prices), cartTestLogger(), bus)
	h := shophttp.NewCartHandler(svc)
	return carts, prices, h, newCartRouter(h)
}

func parseCartBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return body
}

// ── tests ───────────────────────────────────────────────────────────────

func TestCartHandler_Create_OK(t *testing.T) {
	_, _, _, mux := cartSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/carts", strings.NewReader(`{"currency":"EUR"}`))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	body := parseCartBody(t, rec)
	data := body["data"].(map[string]interface{})
	c := data["cart"].(map[string]interface{})
	if c["currency"] != "EUR" {
		t.Errorf("currency = %v, want EUR", c["currency"])
	}
	if c["status"] != "active" {
		t.Errorf("status = %v, want active", c["status"])
	}
}

func TestCartHandler_Create_InvalidCurrency(t *testing.T) {
	_, _, _, mux := cartSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/carts", strings.NewReader(`{"currency":"bad"}`))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCartHandler_Create_InvalidBody(t *testing.T) {
	_, _, _, mux := cartSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/carts", strings.NewReader(`not json`))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCartHandler_Get_OK(t *testing.T) {
	carts, _, _, mux := cartSetup()

	// Seed a cart.
	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	carts.Save(context.Background(), &c)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/carts/cart-1", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := parseCartBody(t, rec)
	data := body["data"].(map[string]interface{})
	cart := data["cart"].(map[string]interface{})
	if cart["id"] != "cart-1" {
		t.Errorf("id = %v, want cart-1", cart["id"])
	}
}

func TestCartHandler_Get_NotFound(t *testing.T) {
	_, _, _, mux := cartSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/carts/no-such", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCartHandler_AddItem_OK(t *testing.T) {
	carts, prices, _, mux := cartSetup()
	prices.set("var-1", "EUR", 1500)

	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	carts.Save(context.Background(), &c)

	rec := httptest.NewRecorder()
	body := `{"variant_id":"var-1","quantity":2}`
	req := httptest.NewRequest("POST", "/api/v1/carts/cart-1/items", strings.NewReader(body))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := parseCartBody(t, rec)
	data := resp["data"].(map[string]interface{})
	cart := data["cart"].(map[string]interface{})
	items := cart["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	item := items[0].(map[string]interface{})
	if item["variant_id"] != "var-1" {
		t.Errorf("variant_id = %v, want var-1", item["variant_id"])
	}
	if int(item["quantity"].(float64)) != 2 {
		t.Errorf("quantity = %v, want 2", item["quantity"])
	}
	if int64(item["unit_price"].(float64)) != 1500 {
		t.Errorf("unit_price = %v, want 1500", item["unit_price"])
	}
}

func TestCartHandler_AddItem_CartNotFound(t *testing.T) {
	_, _, _, mux := cartSetup()

	rec := httptest.NewRecorder()
	body := `{"variant_id":"var-1","quantity":1}`
	req := httptest.NewRequest("POST", "/api/v1/carts/no-cart/items", strings.NewReader(body))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCartHandler_AddItem_MissingVariantID(t *testing.T) {
	carts, _, _, mux := cartSetup()

	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	carts.Save(context.Background(), &c)

	rec := httptest.NewRecorder()
	body := `{"quantity":1}`
	req := httptest.NewRequest("POST", "/api/v1/carts/cart-1/items", strings.NewReader(body))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCartHandler_AddItem_InvalidQuantity(t *testing.T) {
	carts, _, _, mux := cartSetup()

	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	carts.Save(context.Background(), &c)

	rec := httptest.NewRecorder()
	body := `{"variant_id":"var-1","quantity":0}`
	req := httptest.NewRequest("POST", "/api/v1/carts/cart-1/items", strings.NewReader(body))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCartHandler_UpdateItem_OK(t *testing.T) {
	carts, prices, _, mux := cartSetup()
	prices.set("var-1", "EUR", 1000)

	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	c.AddItem("var-1", 1, shared.MustNewMoney(1000, "EUR"))
	carts.Save(context.Background(), &c)

	rec := httptest.NewRecorder()
	body := `{"quantity":5}`
	req := httptest.NewRequest("PUT", "/api/v1/carts/cart-1/items/var-1", strings.NewReader(body))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := parseCartBody(t, rec)
	data := resp["data"].(map[string]interface{})
	cart := data["cart"].(map[string]interface{})
	items := cart["items"].([]interface{})
	item := items[0].(map[string]interface{})
	if int(item["quantity"].(float64)) != 5 {
		t.Errorf("quantity = %v, want 5", item["quantity"])
	}
}

func TestCartHandler_UpdateItem_CartNotFound(t *testing.T) {
	_, _, _, mux := cartSetup()

	rec := httptest.NewRecorder()
	body := `{"quantity":3}`
	req := httptest.NewRequest("PUT", "/api/v1/carts/no-cart/items/var-1", strings.NewReader(body))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCartHandler_UpdateItem_InvalidQuantity(t *testing.T) {
	carts, _, _, mux := cartSetup()

	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	carts.Save(context.Background(), &c)

	rec := httptest.NewRecorder()
	body := `{"quantity":-1}`
	req := httptest.NewRequest("PUT", "/api/v1/carts/cart-1/items/var-1", strings.NewReader(body))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCartHandler_RemoveItem_OK(t *testing.T) {
	carts, prices, _, mux := cartSetup()
	prices.set("var-1", "EUR", 500)

	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	c.AddItem("var-1", 1, shared.MustNewMoney(500, "EUR"))
	carts.Save(context.Background(), &c)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/carts/cart-1/items/var-1", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := parseCartBody(t, rec)
	data := resp["data"].(map[string]interface{})
	cart := data["cart"].(map[string]interface{})
	items := cart["items"].([]interface{})
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestCartHandler_RemoveItem_CartNotFound(t *testing.T) {
	_, _, _, mux := cartSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/carts/no-cart/items/var-1", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCartHandler_RemoveItem_ItemNotFound(t *testing.T) {
	carts, _, _, mux := cartSetup()

	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	carts.Save(context.Background(), &c)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/carts/cart-1/items/var-x", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCartHandler_Unauthenticated(t *testing.T) {
	_, _, _, mux := cartSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/carts", strings.NewReader(`{"currency":"EUR"}`))
	// No auth context — guest identity.
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestCartHandler_AddItem_WrongCustomer(t *testing.T) {
	carts, prices, _, mux := cartSetup()
	prices.set("var-1", "EUR", 1000)

	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	carts.Save(context.Background(), &c)

	rec := httptest.NewRecorder()
	body := `{"variant_id":"var-1","quantity":1}`
	req := httptest.NewRequest("POST", "/api/v1/carts/cart-1/items", strings.NewReader(body))
	req = testhelper.CustomerRequest(req, "cust-OTHER")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}
