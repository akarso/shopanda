package http_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/akarso/shopanda/internal/testutil"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	checkoutApp "github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	credit "github.com/akarso/shopanda/internal/domain/storecredit"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"

	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	storecreditApp "github.com/akarso/shopanda/internal/application/storecredit"
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

func (r *stubCheckoutCartRepo) FindRecoveryCandidates(_ context.Context, _ time.Time, _ int) ([]*cart.Cart, error) {
	return nil, nil
}

func (r *stubCheckoutCartRepo) MarkRecoveryEmailSent(_ context.Context, _ string, _ time.Time) (bool, error) {
	return false, nil
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
func (r *stubCheckoutVariantRepo) ListByProductIDs(ctx context.Context, productIDs []string, limitPerProduct int) (map[string][]catalog.Variant, error) {
	return testutil.ListByProductIDsFromList(ctx, r.ListByProductID, productIDs, limitPerProduct)
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
	err   error
}

func (r *stubCheckoutOrderRepo) FindByID(_ context.Context, _ string) (*order.Order, error) {
	return nil, nil
}
func (r *stubCheckoutOrderRepo) FindByCustomerID(_ context.Context, _ string) ([]order.Order, error) {
	return nil, nil
}
func (r *stubCheckoutOrderRepo) FindByContactEmail(_ context.Context, _ string) ([]order.Order, error) {
	return nil, nil
}
func (r *stubCheckoutOrderRepo) List(_ context.Context, _, _ int) ([]order.Order, error) {
	return nil, nil
}
func (r *stubCheckoutOrderRepo) Save(_ context.Context, o *order.Order) error {
	if r.err != nil {
		return r.err
	}
	r.saved = o
	return nil
}
func (r *stubCheckoutOrderRepo) UpdateStatus(_ context.Context, _ *order.Order) error   { return nil }
func (r *stubCheckoutOrderRepo) LinkToCustomer(_ context.Context, _ *order.Order) error { return nil }
func (r *stubCheckoutOrderRepo) LinkToCustomerByContactEmail(_ context.Context, _, _ string, _ time.Time) (int64, error) {
	return 0, nil
}
func (r *stubCheckoutOrderRepo) ListPaidTaxSnapshots(context.Context, time.Time, time.Time) ([]order.TaxSnapshotRow, error) {
	return nil, nil
}

// stubCheckoutPriceRepo ─────────────────────────────────────────────────

type stubCheckoutPriceRepo struct {
	prices map[string]*pricing.Price // key: "variantID:currency:storeID"
}

func newStubCheckoutPriceRepo() *stubCheckoutPriceRepo {
	return &stubCheckoutPriceRepo{prices: make(map[string]*pricing.Price)}
}

func (r *stubCheckoutPriceRepo) set(variantID, currency string, amount int64) {
	r.setWithStore(variantID, currency, "", amount)
}

func (r *stubCheckoutPriceRepo) setWithStore(variantID, currency, storeID string, amount int64) {
	key := variantID + ":" + currency + ":" + storeID
	p, _ := pricing.NewPrice("price-"+key, variantID, storeID, shared.MustNewMoney(amount, currency))
	r.prices[key] = &p
}

func (r *stubCheckoutPriceRepo) FindByVariantCurrencyAndStore(_ context.Context, variantID, currency, storeID string) (*pricing.Price, error) {
	return r.prices[variantID+":"+currency+":"+storeID], nil
}

func (r *stubCheckoutPriceRepo) FindByVariantsCurrencyAndStore(ctx context.Context, variantIDs []string, currency, storeID string) (map[string]*pricing.Price, error) {
	return testutil.FindByVariantsCurrencyAndStoreFromFind(ctx, r.FindByVariantCurrencyAndStore, variantIDs, currency, storeID)
}

func (r *stubCheckoutPriceRepo) ListByVariantID(_ context.Context, _ string) ([]pricing.Price, error) {
	return nil, nil
}

func (r *stubCheckoutPriceRepo) List(_ context.Context, _, _ int) ([]pricing.Price, error) {
	return nil, nil
}

func (r *stubCheckoutPriceRepo) Upsert(_ context.Context, _ *pricing.Price) error { return nil }

type stubCheckoutStoreCreditRepo struct {
	balances        map[string]int64
	getBalanceCalls []struct {
		customerID string
		currency   string
	}
	redeemCalls []struct {
		customerID string
		orderID    string
		amount     int64
		currency   string
	}
	issueCalls []struct {
		customerID string
		amount     int64
		currency   string
		note       string
	}
}

func newStubCheckoutStoreCreditRepo() *stubCheckoutStoreCreditRepo {
	return &stubCheckoutStoreCreditRepo{balances: make(map[string]int64)}
}

func (s *stubCheckoutStoreCreditRepo) setBalance(customerID, currency string, amount int64) {
	s.balances[storeCreditBalanceKey(customerID, currency)] = amount
}

func (s *stubCheckoutStoreCreditRepo) balanceFor(customerID, currency string) int64 {
	return s.balances[storeCreditBalanceKey(customerID, currency)]
}

func storeCreditBalanceKey(customerID, currency string) string {
	return customerID + ":" + currency
}

func (s *stubCheckoutStoreCreditRepo) GetBalance(_ context.Context, customerID, currency string) (shared.Money, error) {
	s.getBalanceCalls = append(s.getBalanceCalls, struct {
		customerID string
		currency   string
	}{customerID: customerID, currency: currency})
	return shared.MustNewMoney(s.balanceFor(customerID, currency), currency), nil
}

func (s *stubCheckoutStoreCreditRepo) Issue(_ context.Context, customerID string, amount shared.Money, note string) error {
	s.issueCalls = append(s.issueCalls, struct {
		customerID string
		amount     int64
		currency   string
		note       string
	}{
		customerID: customerID,
		amount:     amount.Amount(),
		currency:   amount.Currency(),
		note:       note,
	})
	key := storeCreditBalanceKey(customerID, amount.Currency())
	s.balances[key] += amount.Amount()
	return nil
}

func (s *stubCheckoutStoreCreditRepo) Redeem(_ context.Context, customerID, orderID string, amount shared.Money) error {
	s.redeemCalls = append(s.redeemCalls, struct {
		customerID string
		orderID    string
		amount     int64
		currency   string
	}{
		customerID: customerID,
		orderID:    orderID,
		amount:     amount.Amount(),
		currency:   amount.Currency(),
	})
	key := storeCreditBalanceKey(customerID, amount.Currency())
	s.balances[key] -= amount.Amount()
	return nil
}

func (s *stubCheckoutStoreCreditRepo) ListLedger(_ context.Context, _, _ string, _, _ int) ([]credit.Entry, error) {
	return nil, nil
}

type stubCheckoutCustomerRepo struct{}

func (s *stubCheckoutCustomerRepo) FindByID(_ context.Context, id string) (*customer.Customer, error) {
	if id == "" {
		return nil, nil
	}
	c, err := customer.NewCustomer(id, id+"@example.com")
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *stubCheckoutCustomerRepo) FindByEmail(_ context.Context, _ string) (*customer.Customer, error) {
	return nil, nil
}
func (s *stubCheckoutCustomerRepo) Create(_ context.Context, _ *customer.Customer) error { return nil }
func (s *stubCheckoutCustomerRepo) Update(_ context.Context, _ *customer.Customer) error { return nil }
func (s *stubCheckoutCustomerRepo) ListCustomers(_ context.Context, _, _ int) ([]customer.Customer, error) {
	return nil, nil
}
func (s *stubCheckoutCustomerRepo) BumpTokenGeneration(_ context.Context, _ string) error { return nil }
func (s *stubCheckoutCustomerRepo) ChangePasswordAndBumpTokenGeneration(_ context.Context, _, _ string) error {
	return nil
}
func (s *stubCheckoutCustomerRepo) Delete(_ context.Context, _ string) error { return nil }
func (s *stubCheckoutCustomerRepo) DeleteAccount(_ context.Context, _ string) error {
	return nil
}

type stubCheckoutPaymentProvider struct {
	method payment.PaymentMethod
}

func (p *stubCheckoutPaymentProvider) Method() payment.PaymentMethod { return p.method }

func (p *stubCheckoutPaymentProvider) Initiate(_ context.Context, py *payment.Payment) (payment.ProviderResult, error) {
	return payment.ProviderResult{
		ProviderRef: "manual:" + py.ID,
		Success:     true,
	}, nil
}

type stubCheckoutPaymentRepo struct {
	created int
}

func (r *stubCheckoutPaymentRepo) Create(_ context.Context, _ *payment.Payment) error {
	r.created++
	return nil
}
func (r *stubCheckoutPaymentRepo) FindByID(_ context.Context, _ string) (*payment.Payment, error) {
	return nil, nil
}
func (r *stubCheckoutPaymentRepo) FindByOrderID(_ context.Context, _ string) (*payment.Payment, error) {
	return nil, nil
}
func (r *stubCheckoutPaymentRepo) UpdateStatus(_ context.Context, _ *payment.Payment, _ time.Time) error {
	return nil
}
func (r *stubCheckoutPaymentRepo) List(_ context.Context, _ payment.ListFilter) ([]payment.Payment, error) {
	return nil, nil
}

// ── helpers ─────────────────────────────────────────────────────────────

func checkoutTestLogger() logger.Logger {
	return logger.NewWithWriter(io.Discard, "error")
}

func checkoutSetup() (*stubCheckoutCartRepo, *stubCheckoutVariantRepo, *stubCheckoutPriceRepo, *http.ServeMux) {
	carts, variants, prices, _, mux := checkoutBuild(nil, false)
	return carts, variants, prices, mux
}

func checkoutBuild(creditRepo *stubCheckoutStoreCreditRepo, withPayment bool) (*stubCheckoutCartRepo, *stubCheckoutVariantRepo, *stubCheckoutPriceRepo, *stubCheckoutStoreCreditRepo, *http.ServeMux) {
	carts := newStubCheckoutCartRepo()
	variants := newStubCheckoutVariantRepo()
	prices := newStubCheckoutPriceRepo()
	reservations := &stubCheckoutReservationRepo{}
	orders := &stubCheckoutOrderRepo{}
	var creditSvc *storecreditApp.Service
	if creditRepo != nil {
		creditSvc = storecreditApp.NewService(creditRepo, &stubCheckoutCustomerRepo{})
	}
	log := checkoutTestLogger()
	bus := event.NewBus(log)

	pipeline := pricing.NewPipeline(
		appPricing.NewBasePriceStep(prices),
		pricing.NewFinalizeStep(),
	)

	validateStep := checkoutApp.NewValidateCartStep(variants)
	pricingStep := checkoutApp.NewRecalculatePricingStep(pipeline)
	reserveStep := checkoutApp.NewReserveInventoryStep(reservations)
	createOrderStep := checkoutApp.NewCreateOrderStep(orders, variants, creditSvc, nil)

	steps := []checkoutApp.Step{
		validateStep,
		pricingStep,
		reserveStep,
		createOrderStep,
	}
	if withPayment {
		reg := payment.NewProviderRegistry()
		reg.Register(&stubCheckoutPaymentProvider{method: payment.MethodManual})
		steps = append(steps, checkoutApp.NewInitiatePaymentStep(reg, &stubCheckoutPaymentRepo{}))
	}

	workflow := checkoutApp.NewWorkflow(steps, bus, log)

	svc := checkoutApp.NewService(carts, workflow, log)
	handler := shophttp.NewCheckoutHandler(svc, nil)

	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/checkout", handler.StartCheckout())
	return carts, variants, prices, creditRepo, mux
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

func seedGuestCheckoutCart(carts *stubCheckoutCartRepo, variants *stubCheckoutVariantRepo, prices *stubCheckoutPriceRepo) string {
	cartID := id.New()
	c, _ := cart.NewCart(cartID, "EUR")
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

func checkoutAddressJSON() string {
	return `{"first_name":"Ada","last_name":"Lovelace","street":"1 Logic Lane","city":"Berlin","postcode":"10115","country":"DE"}`
}

// ── tests ───────────────────────────────────────────────────────────────

func TestCheckoutHandler_StartCheckout_OK(t *testing.T) {
	carts, variants, prices, mux := checkoutSetup()
	cartID := seedCheckoutCart(carts, variants, prices)

	body := `{"cart_id":"` + cartID + `","address":` + checkoutAddressJSON() + `}`
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
	if o["store_credit_applied"].(float64) != 0 {
		t.Errorf("store_credit_applied = %v, want 0", o["store_credit_applied"])
	}
	if o["payable_amount"].(float64) != 3000 {
		t.Errorf("payable_amount = %v, want 3000", o["payable_amount"])
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

func TestCheckoutHandler_StartCheckout_MissingAddress(t *testing.T) {
	carts, variants, prices, mux := checkoutSetup()
	cartID := seedCheckoutCart(carts, variants, prices)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/checkout", strings.NewReader(`{"cart_id":"`+cartID+`"}`))
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
	req := httptest.NewRequest("POST", "/api/v1/checkout", strings.NewReader(`{"cart_id":"no-such","address":`+checkoutAddressJSON()+`}`))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestCheckoutHandler_StartCheckout_WrongCustomer(t *testing.T) {
	carts, variants, prices, mux := checkoutSetup()
	cartID := seedCheckoutCart(carts, variants, prices)

	body := `{"cart_id":"` + cartID + `","address":` + checkoutAddressJSON() + `}`
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
	req := httptest.NewRequest("POST", "/api/v1/checkout", strings.NewReader(`{"cart_id":"empty-cart","address":`+checkoutAddressJSON()+`}`))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestCheckoutHandler_StartCheckout_Guest_OK(t *testing.T) {
	carts, variants, prices, mux := checkoutSetup()
	cartID := seedGuestCheckoutCart(carts, variants, prices)

	body := `{"cart_id":"` + cartID + `","contact_email":"guest@example.com","address":` + checkoutAddressJSON() + `}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/checkout", strings.NewReader(body))
	req = testhelper.GuestRequest(req)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	resp := parseCheckoutBody(t, rec)
	data := resp["data"].(map[string]interface{})
	o := data["order"].(map[string]interface{})

	if o["customer_id"] != "" {
		t.Errorf("customer_id = %v, want empty for guest", o["customer_id"])
	}
}

func TestCheckoutHandler_StartCheckout_Guest_MissingContactEmail(t *testing.T) {
	carts, variants, prices, mux := checkoutSetup()
	cartID := seedGuestCheckoutCart(carts, variants, prices)

	body := `{"cart_id":"` + cartID + `","address":` + checkoutAddressJSON() + `}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/checkout", strings.NewReader(body))
	req = testhelper.GuestRequest(req)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestCheckoutHandler_StartCheckout_Guest_CannotUseCustomerCart(t *testing.T) {
	carts, variants, prices, mux := checkoutSetup()
	cartID := seedCheckoutCart(carts, variants, prices)

	body := `{"cart_id":"` + cartID + `","contact_email":"guest@example.com","address":` + checkoutAddressJSON() + `}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/checkout", strings.NewReader(body))
	req = testhelper.GuestRequest(req)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestCheckoutHandler_StartCheckout_WithStoreCredit(t *testing.T) {
	creditRepo := newStubCheckoutStoreCreditRepo()
	creditRepo.setBalance("cust-1", "EUR", 2000)
	carts, variants, prices, credits, mux := checkoutBuild(creditRepo, false)
	cartID := seedCheckoutCart(carts, variants, prices)

	body := `{"cart_id":"` + cartID + `","store_credit_amount":1000,"address":` + checkoutAddressJSON() + `}`
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

	if o["store_credit_applied"].(float64) != 1000 {
		t.Errorf("store_credit_applied = %v, want 1000", o["store_credit_applied"])
	}
	if o["payable_amount"].(float64) != 2000 {
		t.Errorf("payable_amount = %v, want 2000", o["payable_amount"])
	}
	if len(credits.getBalanceCalls) != 1 {
		t.Fatalf("getBalanceCalls = %d, want 1", len(credits.getBalanceCalls))
	}
	if credits.getBalanceCalls[0].customerID != "cust-1" || credits.getBalanceCalls[0].currency != "EUR" {
		t.Errorf("getBalanceCalls = %+v, want cust-1/EUR", credits.getBalanceCalls[0])
	}
	if len(credits.redeemCalls) != 1 {
		t.Fatalf("redeemCalls = %d, want 1", len(credits.redeemCalls))
	}
	redeem := credits.redeemCalls[0]
	if redeem.customerID != "cust-1" || redeem.currency != "EUR" || redeem.amount != 1000 {
		t.Errorf("redeemCalls[0] = %+v, want cust-1/EUR/1000", redeem)
	}
	if redeem.orderID == "" {
		t.Error("redeemCalls[0].orderID should not be empty")
	}
	if credits.balanceFor("cust-1", "EUR") != 1000 {
		t.Errorf("balance cust-1/EUR = %d, want 1000", credits.balanceFor("cust-1", "EUR"))
	}
}

func TestCheckoutHandler_StartCheckout_FullStoreCreditZeroPayable(t *testing.T) {
	creditRepo := newStubCheckoutStoreCreditRepo()
	creditRepo.setBalance("cust-1", "EUR", 5000)
	carts, variants, prices, credits, mux := checkoutBuild(creditRepo, true)
	cartID := seedCheckoutCart(carts, variants, prices)

	body := `{"cart_id":"` + cartID + `","store_credit_amount":3000,"address":` + checkoutAddressJSON() + `}`
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

	if o["store_credit_applied"].(float64) != 3000 {
		t.Errorf("store_credit_applied = %v, want 3000", o["store_credit_applied"])
	}
	if o["payable_amount"].(float64) != 0 {
		t.Errorf("payable_amount = %v, want 0", o["payable_amount"])
	}
	if _, ok := data["payment"]; ok {
		t.Error("payment should be omitted when payable is zero")
	}
	if len(credits.redeemCalls) != 1 {
		t.Fatalf("redeemCalls = %d, want 1", len(credits.redeemCalls))
	}
	redeem := credits.redeemCalls[0]
	if redeem.customerID != "cust-1" || redeem.currency != "EUR" || redeem.amount != 3000 {
		t.Errorf("redeemCalls[0] = %+v, want cust-1/EUR/3000", redeem)
	}
	if credits.balanceFor("cust-1", "EUR") != 2000 {
		t.Errorf("balance cust-1/EUR = %d, want 2000", credits.balanceFor("cust-1", "EUR"))
	}
}
