package storefront_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/testutil"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/application/hooks"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/pkg/extapi"
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
	clone := *c
	clone.Items = make([]domainCart.Item, len(c.Items))
	copy(clone.Items, c.Items)
	return &clone, nil
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

func (r *stubCartRepo) FindRecoveryCandidates(_ context.Context, _ time.Time, _ int) ([]*domainCart.Cart, error) {
	return nil, nil
}

func (r *stubCartRepo) MarkRecoveryEmailSent(_ context.Context, _ string, _ time.Time) (bool, error) {
	return false, nil
}

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

func (r *stubPriceRepo) FindByVariantsCurrencyAndStore(ctx context.Context, variantIDs []string, currency, storeID string) (map[string]*pricing.Price, error) {
	return testutil.FindByVariantsCurrencyAndStoreFromFind(ctx, r.FindByVariantCurrencyAndStore, variantIDs, currency, storeID)
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

func cartTestLogger() logger.Logger {
	return logger.NewWithWriter(io.Discard, "error")
}

func cartTestPipeline(prices pricing.PriceRepository) pricing.Pipeline {
	return pricing.NewPipeline(
		appPricing.NewBasePriceStep(prices),
		pricing.NewFinalizeStep(),
	)
}

func newCartRouter(h *storefront.CartHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/carts", h.Create())
	mux.Handle("GET /api/v1/carts/{cartId}", h.Get())
	mux.Handle("POST /api/v1/carts/{cartId}/items", h.AddItem())
	mux.Handle("PUT /api/v1/carts/{cartId}/items/{variantId}", h.UpdateItem())
	mux.Handle("DELETE /api/v1/carts/{cartId}/items/{variantId}", h.RemoveItem())
	return mux
}

func cartSetup() (*stubCartRepo, *stubPriceRepo, *storefront.CartHandler, *http.ServeMux) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	bus := event.NewBus(cartTestLogger())
	svc := cartApp.NewService(carts, prices, nil, nil, cartTestPipeline(prices), cartTestLogger(), bus, nil, nil)
	h := storefront.NewCartHandler(svc, nil)
	return carts, prices, h, newCartRouter(h)
}

type cartTestExtensionValueRepo struct {
	values map[string]domainext.Value
}

func cartTestExtKey(target domainext.Target, fieldCode string) string {
	return string(target.Type) + ":" + target.ID + ":" + fieldCode
}

func newCartTestExtensionValueRepo() *cartTestExtensionValueRepo {
	return &cartTestExtensionValueRepo{values: make(map[string]domainext.Value)}
}

func (m *cartTestExtensionValueRepo) ListByTarget(_ context.Context, target domainext.Target) ([]domainext.Value, error) {
	out := make([]domainext.Value, 0)
	for _, value := range m.values {
		if value.TargetType == target.Type && value.TargetID == target.ID {
			out = append(out, value)
		}
	}
	return out, nil
}

func (m *cartTestExtensionValueRepo) ListByTargets(_ context.Context, targetType domainext.TargetType, targetIDs []string) ([]domainext.Value, error) {
	out := make([]domainext.Value, 0)
	for _, targetID := range targetIDs {
		stored, err := m.ListByTarget(context.Background(), domainext.Target{Type: targetType, ID: targetID})
		if err != nil {
			return nil, err
		}
		out = append(out, stored...)
	}
	return out, nil
}

func (m *cartTestExtensionValueRepo) Upsert(_ context.Context, value domainext.Value) error {
	m.values[cartTestExtKey(domainext.Target{Type: value.TargetType, ID: value.TargetID}, value.FieldCode)] = value
	return nil
}

func (m *cartTestExtensionValueRepo) UpsertBatch(_ context.Context, values []domainext.Value) error {
	for _, value := range values {
		if err := m.Upsert(context.Background(), value); err != nil {
			return err
		}
	}
	return nil
}

func (m *cartTestExtensionValueRepo) Delete(_ context.Context, target domainext.Target, fieldCode string) error {
	key := cartTestExtKey(target, fieldCode)
	if _, ok := m.values[key]; !ok {
		return apperror.NotFound("extension value not found")
	}
	delete(m.values, key)
	return nil
}

func cartExtensionSetup(t *testing.T) (*stubCartRepo, *stubPriceRepo, *storefront.CartHandler, *http.ServeMux) {
	t.Helper()
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code:        "acme.gift.message",
		Label:       "Gift message",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetCartItem,
		StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatalf("register field: %v", err)
	}
	values := extensionapp.NewValueService(reg, newCartTestExtensionValueRepo())
	bus := event.NewBus(cartTestLogger())
	svc := cartApp.NewService(carts, prices, nil, nil, cartTestPipeline(prices), cartTestLogger(), bus, values, nil)
	h := storefront.NewCartHandler(svc, values)
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

func TestCartHandler_Get_GuestCannotReadCustomerCart(t *testing.T) {
	carts, _, _, mux := cartSetup()

	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	carts.Save(context.Background(), &c)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/carts/cart-1", nil)
	req = testhelper.GuestRequest(req)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
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

func TestCartHandler_Create_Guest_OK(t *testing.T) {
	_, _, _, mux := cartSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/carts", strings.NewReader(`{"currency":"EUR"}`))
	req = testhelper.GuestRequest(req)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	body := parseCartBody(t, rec)
	data := body["data"].(map[string]interface{})
	c := data["cart"].(map[string]interface{})
	if c["customer_id"] != nil && c["customer_id"] != "" {
		t.Errorf("customer_id = %v, want empty for guest cart", c["customer_id"])
	}
}

func TestCartHandler_Create_NoAuth_GuestSucceeds(t *testing.T) {
	_, _, _, mux := cartSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/carts", strings.NewReader(`{"currency":"EUR"}`))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
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

func TestCartHandler_AddItem_WithExtensions(t *testing.T) {
	carts, prices, _, mux := cartExtensionSetup(t)
	prices.set("var-1", "EUR", 1000)

	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	carts.Save(context.Background(), &c)

	rec := httptest.NewRecorder()
	body := `{"variant_id":"var-1","quantity":1,"extensions":[{"field_code":"acme.gift.message","value":"Hello"}]}`
	req := httptest.NewRequest("POST", "/api/v1/carts/cart-1/items", strings.NewReader(body))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	parsed := parseCartBody(t, rec)
	data := parsed["data"].(map[string]interface{})
	cart := data["cart"].(map[string]interface{})
	items := cart["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	item := items[0].(map[string]interface{})
	exts := item["extensions"].([]interface{})
	if len(exts) != 1 {
		t.Fatalf("extensions = %d, want 1", len(exts))
	}
	ext := exts[0].(map[string]interface{})
	if ext["field_code"] != "acme.gift.message" || ext["value"] != "Hello" {
		t.Fatalf("extension = %+v", ext)
	}
}

func TestCartHandler_AddItem_InvalidExtensionRejected(t *testing.T) {
	carts, prices, _, mux := cartExtensionSetup(t)
	prices.set("var-1", "EUR", 1000)

	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	carts.Save(context.Background(), &c)

	rec := httptest.NewRecorder()
	body := `{"variant_id":"var-1","quantity":1,"extensions":[{"field_code":"missing.field","value":"x"}]}`
	req := httptest.NewRequest("POST", "/api/v1/carts/cart-1/items", strings.NewReader(body))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestCartHandler_UpdateItem_PreservesExtensions(t *testing.T) {
	carts, prices, _, mux := cartExtensionSetup(t)
	prices.set("var-1", "EUR", 1000)

	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	carts.Save(context.Background(), &c)

	addRec := httptest.NewRecorder()
	addBody := `{"variant_id":"var-1","quantity":1,"extensions":[{"field_code":"acme.gift.message","value":"Keep me"}]}`
	addReq := httptest.NewRequest("POST", "/api/v1/carts/cart-1/items", strings.NewReader(addBody))
	addReq = testhelper.CustomerRequest(addReq, "cust-1")
	mux.ServeHTTP(addRec, addReq)
	if addRec.Code != http.StatusOK {
		t.Fatalf("add status = %d, want %d; body: %s", addRec.Code, http.StatusOK, addRec.Body.String())
	}

	rec := httptest.NewRecorder()
	updateBody := `{"quantity":3}`
	req := httptest.NewRequest("PUT", "/api/v1/carts/cart-1/items/var-1", strings.NewReader(updateBody))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	parsed := parseCartBody(t, rec)
	data := parsed["data"].(map[string]interface{})
	cart := data["cart"].(map[string]interface{})
	items := cart["items"].([]interface{})
	item := items[0].(map[string]interface{})
	if item["quantity"].(float64) != 3 {
		t.Fatalf("quantity = %v, want 3", item["quantity"])
	}
	exts := item["extensions"].([]interface{})
	if len(exts) != 1 {
		t.Fatalf("extensions = %d, want 1", len(exts))
	}
	ext := exts[0].(map[string]interface{})
	if ext["value"] != "Keep me" {
		t.Fatalf("extension value = %v, want Keep me", ext["value"])
	}
}

func cartSetupWithHooks(reg *hooks.Registry) (*stubCartRepo, *stubPriceRepo, *storefront.CartHandler, *http.ServeMux) {
	carts := newStubCartRepo()
	prices := newStubPriceRepo()
	bus := event.NewBus(cartTestLogger())
	svc := cartApp.NewService(carts, prices, nil, nil, cartTestPipeline(prices), cartTestLogger(), bus, nil, reg)
	h := storefront.NewCartHandler(svc, nil)
	return carts, prices, h, newCartRouter(h)
}

func TestCartHandler_Get_IncludesValidationWarnings(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	if err := reg.Register(hooks.HookCartValidate, 100, "plugin.test", func(ctx *hooks.Context) error {
		hooks.AppendValidationIssue(ctx, extapi.CartValidationIssue{
			Code:    "acme.soft",
			Message: "consider bundle",
			Level:   "warning",
		})
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	carts, prices, _, mux := cartSetupWithHooks(reg)
	prices.set("var-1", "EUR", 1000)
	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	_ = c.AddItem("var-1", 1, shared.MustNewMoney(1000, "EUR"))
	carts.Save(context.Background(), &c)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/carts/cart-1", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	parsed := parseCartBody(t, rec)
	data := parsed["data"].(map[string]interface{})
	issues := data["validation_errors"].([]interface{})
	if len(issues) != 1 {
		t.Fatalf("validation_errors = %v", data["validation_errors"])
	}
	issue := issues[0].(map[string]interface{})
	if issue["code"] != "acme.soft" {
		t.Fatalf("issue code = %v", issue["code"])
	}
}

func TestCartHandler_AddItem_BlockedByValidateHook(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	if err := reg.Register(hooks.HookCartValidate, 100, "plugin.test", func(ctx *hooks.Context) error {
		hooks.AppendValidationIssue(ctx, extapi.CartValidationIssue{
			Code:      "acme.blocked",
			Message:   "not allowed",
			VariantID: "var-1",
		})
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	carts, prices, _, mux := cartSetupWithHooks(reg)
	prices.set("var-1", "EUR", 1000)
	c, _ := domainCart.NewCart("cart-1", "EUR")
	c.SetCustomerID("cust-1")
	carts.Save(context.Background(), &c)

	rec := httptest.NewRecorder()
	body := `{"variant_id":"var-1","quantity":1}`
	req := httptest.NewRequest("POST", "/api/v1/carts/cart-1/items", strings.NewReader(body))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	parsed := parseCartBody(t, rec)
	errBody := parsed["error"].(map[string]interface{})
	if errBody["code"] != "cart_validation_failed" {
		t.Fatalf("error code = %v", errBody["code"])
	}
	data := parsed["data"].(map[string]interface{})
	issues := data["validation_errors"].([]interface{})
	if len(issues) != 1 {
		t.Fatalf("validation_errors = %v", data["validation_errors"])
	}
	cart := data["cart"].(map[string]interface{})
	items := cart["items"].([]interface{})
	if len(items) != 0 {
		t.Fatalf("persisted items = %d, want 0", len(items))
	}
}
