package http_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	checkoutApp "github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/application/composition"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/infrastructure/flatrate"
	"github.com/akarso/shopanda/internal/infrastructure/manualpay"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	platformAuth "github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type failingCheckoutStep struct {
	err error
}

func (s failingCheckoutStep) Name() string { return "failing_step" }

func (s failingCheckoutStep) Execute(_ *checkoutApp.Context) error { return s.err }

type storefrontCheckoutReservationRepoStub struct{}

func (r *storefrontCheckoutReservationRepoStub) Reserve(_ context.Context, _ *inventory.Reservation) error {
	return nil
}

func (r *storefrontCheckoutReservationRepoStub) Release(_ context.Context, _ string) error {
	return nil
}
func (r *storefrontCheckoutReservationRepoStub) Confirm(_ context.Context, _ string) error {
	return nil
}
func (r *storefrontCheckoutReservationRepoStub) FindByID(_ context.Context, _ string) (*inventory.Reservation, error) {
	return nil, nil
}
func (r *storefrontCheckoutReservationRepoStub) ListActiveByVariantID(_ context.Context, _ string) ([]inventory.Reservation, error) {
	return nil, nil
}
func (r *storefrontCheckoutReservationRepoStub) ReleaseExpiredBefore(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

type storefrontCheckoutOrderRepoStub struct {
	saved *order.Order
}

func (r *storefrontCheckoutOrderRepoStub) FindByID(_ context.Context, _ string) (*order.Order, error) {
	return nil, nil
}
func (r *storefrontCheckoutOrderRepoStub) FindByCustomerID(_ context.Context, _ string) ([]order.Order, error) {
	return nil, nil
}
func (r *storefrontCheckoutOrderRepoStub) List(_ context.Context, _, _ int) ([]order.Order, error) {
	return nil, nil
}
func (r *storefrontCheckoutOrderRepoStub) Save(_ context.Context, o *order.Order) error {
	clone := *o
	r.saved = &clone
	return nil
}
func (r *storefrontCheckoutOrderRepoStub) UpdateStatus(_ context.Context, _ *order.Order) error {
	return nil
}

type storefrontCheckoutShipmentRepoStub struct {
	created *shipping.Shipment
}

func (r *storefrontCheckoutShipmentRepoStub) FindByID(_ context.Context, _ string) (*shipping.Shipment, error) {
	return nil, nil
}
func (r *storefrontCheckoutShipmentRepoStub) FindByOrderID(_ context.Context, _ string) (*shipping.Shipment, error) {
	return nil, nil
}
func (r *storefrontCheckoutShipmentRepoStub) Create(_ context.Context, s *shipping.Shipment) error {
	clone := *s
	r.created = &clone
	return nil
}
func (r *storefrontCheckoutShipmentRepoStub) UpdateStatus(_ context.Context, _ *shipping.Shipment, _ time.Time) error {
	return nil
}

type storefrontCheckoutPaymentRepoStub struct {
	created *payment.Payment
}

func (r *storefrontCheckoutPaymentRepoStub) FindByID(_ context.Context, _ string) (*payment.Payment, error) {
	return nil, nil
}
func (r *storefrontCheckoutPaymentRepoStub) FindByOrderID(_ context.Context, _ string) (*payment.Payment, error) {
	return nil, nil
}
func (r *storefrontCheckoutPaymentRepoStub) Create(_ context.Context, p *payment.Payment) error {
	clone := *p
	r.created = &clone
	return nil
}
func (r *storefrontCheckoutPaymentRepoStub) UpdateStatus(_ context.Context, p *payment.Payment, _ time.Time) error {
	clone := *p
	r.created = &clone
	return nil
}

func storefrontCustomerRequest(req *http.Request, customerID string) *http.Request {
	id, err := identity.NewIdentity(customerID, identity.RoleCustomer)
	if err != nil {
		panic(err)
	}
	return req.WithContext(platformAuth.WithIdentity(req.Context(), id))
}

func storefrontCheckoutCSRFCookie(t *testing.T, handler http.Handler, customerID string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	req := storefrontCustomerRequest(httptest.NewRequest("GET", "/checkout/address", nil), customerID)
	handler.ServeHTTP(rec, req)

	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "shopanda_csrf" {
			return cookie
		}
	}
	t.Fatalf("missing checkout CSRF cookie; body: %s", rec.Body.String())
	return nil
}

func newStorefrontCheckoutService(carts *storefrontCartRepoStub, prices *storefrontPriceRepoStub, variants catalog.VariantRepository) (*checkoutApp.Service, shipping.Provider, payment.Provider, *storefrontCheckoutOrderRepoStub) {
	log := logger.NewWithWriter(io.Discard, "error")
	bus := event.NewBus(log)
	pipeline := pricing.NewPipeline(appPricing.NewBasePriceStep(prices), pricing.NewFinalizeStep())
	orders := &storefrontCheckoutOrderRepoStub{}
	shipments := &storefrontCheckoutShipmentRepoStub{}
	payments := &storefrontCheckoutPaymentRepoStub{}
	shippingProvider := flatrate.NewProvider(shared.MustNewMoney(500, "EUR"))
	paymentProvider := manualpay.NewProvider()
	workflow := checkoutApp.NewWorkflow([]checkoutApp.Step{
		checkoutApp.NewValidateCartStep(variants),
		checkoutApp.NewRecalculatePricingStep(pipeline),
		checkoutApp.NewReserveInventoryStep(&storefrontCheckoutReservationRepoStub{}),
		checkoutApp.NewCreateOrderStep(orders, variants),
		checkoutApp.NewSelectShippingStep(shippingProvider, shipments),
		checkoutApp.NewInitiatePaymentStep(paymentProvider, payments),
	}, bus, log)
	return checkoutApp.NewService(carts, workflow, log), shippingProvider, paymentProvider, orders
}

func TestStorefrontHandler_CheckoutAddress_GuestShowsSignInMessage(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, carts, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	currentCart, err := cartSvc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 1); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	variants := &mockStorefrontVariantRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
		return &catalog.Variant{ID: id, ProductID: "prod-1", SKU: "SKU-1", Name: "Widget Default"}, nil
	}}
	checkoutSvc, shippingProvider, paymentProvider, _ := newStorefrontCheckoutService(carts, prices, variants)
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, paymentProvider, checkoutSvc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/checkout/address", nil)
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Sign in to continue checkout") {
		t.Fatalf("body missing sign-in message: %s", rec.Body.String())
	}
}

func TestStorefrontHandler_CheckoutFlow_Manual_OK(t *testing.T) {
	products := &mockStorefrontRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
		return &catalog.Product{ID: id, Name: "Widget", Slug: "widget"}, nil
	}}
	variants := &mockStorefrontVariantRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
		return &catalog.Variant{ID: id, ProductID: "prod-1", SKU: "WID-1", Name: "Default"}, nil
	}}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, carts, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	checkoutSvc, shippingProvider, paymentProvider, orders := newStorefrontCheckoutService(carts, prices, variants)
	h := shophttp.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, paymentProvider, checkoutSvc)
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "cust-1", "var-1", 2); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	addressRec := httptest.NewRecorder()
	addressReq := storefrontCustomerRequest(httptest.NewRequest("GET", "/checkout/address", nil), "cust-1")
	router.ServeHTTP(addressRec, addressReq)
	if addressRec.Code != http.StatusOK {
		t.Fatalf("address status = %d, want %d; body: %s", addressRec.Code, http.StatusOK, addressRec.Body.String())
	}
	if !strings.Contains(addressRec.Body.String(), "Continue to Shipping") {
		t.Fatalf("address page missing continue action: %s", addressRec.Body.String())
	}
	var csrfCookie *http.Cookie
	for _, cookie := range addressRec.Result().Cookies() {
		if cookie.Name == "shopanda_csrf" {
			csrfCookie = cookie
			break
		}
	}
	if csrfCookie == nil {
		t.Fatal("expected checkout CSRF cookie")
	}

	addressForm := url.Values{
		"csrf_token": {csrfCookie.Value},
		"first_name": {"Ada"},
		"last_name":  {"Lovelace"},
		"street":     {"1 Logic Lane"},
		"city":       {"Berlin"},
		"postcode":   {"10115"},
		"country":    {"DE"},
	}
	shippingRec := httptest.NewRecorder()
	shippingReq := storefrontCustomerRequest(httptest.NewRequest("POST", "/checkout/shipping", strings.NewReader(addressForm.Encode())), "cust-1")
	shippingReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	shippingReq.AddCookie(csrfCookie)
	router.ServeHTTP(shippingRec, shippingReq)
	if shippingRec.Code != http.StatusOK {
		t.Fatalf("shipping status = %d, want %d; body: %s", shippingRec.Code, http.StatusOK, shippingRec.Body.String())
	}
	if !strings.Contains(shippingRec.Body.String(), "Flat Rate Shipping") {
		t.Fatalf("shipping page missing flat rate option: %s", shippingRec.Body.String())
	}

	paymentForm := url.Values{
		"csrf_token":      {csrfCookie.Value},
		"first_name":      {"Ada"},
		"last_name":       {"Lovelace"},
		"street":          {"1 Logic Lane"},
		"city":            {"Berlin"},
		"postcode":        {"10115"},
		"country":         {"DE"},
		"shipping_method": {"flat_rate"},
	}
	paymentRec := httptest.NewRecorder()
	paymentReq := storefrontCustomerRequest(httptest.NewRequest("POST", "/checkout/payment", strings.NewReader(paymentForm.Encode())), "cust-1")
	paymentReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	paymentReq.AddCookie(csrfCookie)
	router.ServeHTTP(paymentRec, paymentReq)
	if paymentRec.Code != http.StatusOK {
		t.Fatalf("payment status = %d, want %d; body: %s", paymentRec.Code, http.StatusOK, paymentRec.Body.String())
	}
	if !strings.Contains(paymentRec.Body.String(), "Manual payment") {
		t.Fatalf("payment page missing manual payment label: %s", paymentRec.Body.String())
	}

	confirmForm := url.Values{
		"csrf_token":      {csrfCookie.Value},
		"first_name":      {"Ada"},
		"last_name":       {"Lovelace"},
		"street":          {"1 Logic Lane"},
		"city":            {"Berlin"},
		"postcode":        {"10115"},
		"country":         {"DE"},
		"shipping_method": {"flat_rate"},
		"payment_method":  {"manual"},
	}
	confirmRec := httptest.NewRecorder()
	confirmReq := storefrontCustomerRequest(httptest.NewRequest("POST", "/checkout/confirm", strings.NewReader(confirmForm.Encode())), "cust-1")
	confirmReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	confirmReq.AddCookie(csrfCookie)
	router.ServeHTTP(confirmRec, confirmReq)
	if confirmRec.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, want %d; body: %s", confirmRec.Code, http.StatusOK, confirmRec.Body.String())
	}
	if orders.saved == nil {
		t.Fatal("expected checkout to save an order")
	}
	body := confirmRec.Body.String()
	if !strings.Contains(body, "Order #") {
		t.Fatalf("confirmation page missing order number: %s", body)
	}
	if !strings.Contains(body, orders.saved.ID) {
		t.Fatalf("confirmation page missing saved order id %q: %s", orders.saved.ID, body)
	}
	if !strings.Contains(body, "EUR 30.00") {
		t.Fatalf("confirmation page missing order total: %s", body)
	}
}

func TestStorefrontHandler_CheckoutConfirm_SanitizesServerErrors(t *testing.T) {
	products := &mockStorefrontRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
		return &catalog.Product{ID: id, Name: "Widget", Slug: "widget"}, nil
	}}
	variants := &mockStorefrontVariantRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
		return &catalog.Variant{ID: id, ProductID: "prod-1", SKU: "WID-1", Name: "Default"}, nil
	}}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, carts, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	log := logger.NewWithWriter(io.Discard, "error")
	workflow := checkoutApp.NewWorkflow([]checkoutApp.Step{failingCheckoutStep{err: errors.New("db credentials leaked")}}, event.NewBus(log), log)
	checkoutSvc := checkoutApp.NewService(carts, workflow, log)
	shippingProvider := flatrate.NewProvider(shared.MustNewMoney(500, "EUR"))
	paymentProvider := manualpay.NewProvider()
	h := shophttp.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, paymentProvider, checkoutSvc)
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "cust-1", "var-1", 1); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	confirmForm := url.Values{
		"csrf_token":      {storefrontCheckoutCSRFCookie(t, router, "cust-1").Value},
		"first_name":      {"Ada"},
		"last_name":       {"Lovelace"},
		"street":          {"1 Logic Lane"},
		"city":            {"Berlin"},
		"postcode":        {"10115"},
		"country":         {"DE"},
		"shipping_method": {"flat_rate"},
		"payment_method":  {"manual"},
	}
	rec := httptest.NewRecorder()
	req := storefrontCustomerRequest(httptest.NewRequest("POST", "/checkout/confirm", strings.NewReader(confirmForm.Encode())), "cust-1")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "shopanda_csrf", Value: confirmForm.Get("csrf_token")})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "db credentials leaked") {
		t.Fatalf("body leaked raw internal error: %s", body)
	}
	if !strings.Contains(body, "Sorry, something went wrong. Please try again later.") {
		t.Fatalf("body missing sanitized error message: %s", body)
	}
}

func TestStorefrontHandler_CheckoutShipping_RejectsMissingCSRF(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, carts, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	variants := &mockStorefrontVariantRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
		return &catalog.Variant{ID: id, ProductID: "prod-1", SKU: "SKU-1", Name: "Widget Default"}, nil
	}}
	checkoutSvc, shippingProvider, paymentProvider, _ := newStorefrontCheckoutService(carts, prices, variants)
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, paymentProvider, checkoutSvc)

	currentCart, err := cartSvc.CreateCart(context.Background(), "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "cust-1", "var-1", 1); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	form := url.Values{
		"first_name": {"Ada"},
		"last_name":  {"Lovelace"},
		"street":     {"1 Logic Lane"},
		"city":       {"Berlin"},
		"postcode":   {"10115"},
		"country":    {"DE"},
	}
	rec := httptest.NewRecorder()
	req := storefrontCustomerRequest(httptest.NewRequest("POST", "/checkout/shipping", strings.NewReader(form.Encode())), "cust-1")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}
