package http_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	appAuth "github.com/akarso/shopanda/internal/application/auth"
	cartApp "github.com/akarso/shopanda/internal/application/cart"
	checkoutApp "github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/application/composition"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/customer"
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

func (s failingCheckoutStep) Execute(_ context.Context, _ *checkoutApp.Context) error { return s.err }

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
func (r *storefrontCheckoutOrderRepoStub) FindByContactEmail(_ context.Context, _ string) ([]order.Order, error) {
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
func (r *storefrontCheckoutOrderRepoStub) LinkToCustomer(_ context.Context, _ *order.Order) error {
	return nil
}
func (r *storefrontCheckoutOrderRepoStub) LinkToCustomerByContactEmail(_ context.Context, _, _ string, _ time.Time) (int64, error) {
	return 0, nil
}
func (r *storefrontCheckoutOrderRepoStub) ListPaidTaxSnapshots(context.Context, time.Time, time.Time) ([]order.TaxSnapshotRow, error) {
	return nil, nil
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
func (r *storefrontCheckoutPaymentRepoStub) List(_ context.Context, _ payment.ListFilter) ([]payment.Payment, error) {
	return nil, nil
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

func storefrontLatestCheckoutEmailRedirect(t *testing.T, published []customer.EmailVerificationRequestedData) string {
	t.Helper()
	if len(published) == 0 {
		t.Fatal("expected email verification event to be published")
	}
	verifyURL, err := url.Parse(published[len(published)-1].VerifyURL)
	if err != nil {
		t.Fatalf("Parse verify URL: %v", err)
	}
	token := strings.TrimSpace(verifyURL.Query().Get("email_token"))
	if token == "" {
		t.Fatal("expected email_token in verification URL")
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		t.Fatalf("email token = %q, want payload.signature", token)
	}
	rawClaims, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("Decode token payload: %v", err)
	}
	var claims storefrontAccountEmailTokenClaims
	if err := json.Unmarshal(rawClaims, &claims); err != nil {
		t.Fatalf("Unmarshal token claims: %v", err)
	}
	return claims.RedirectTo
}

func testPaymentRegistry(providers ...payment.Provider) *payment.ProviderRegistry {
	reg := payment.NewProviderRegistry()
	for _, p := range providers {
		reg.Register(p)
	}
	return reg
}

func newStorefrontCheckoutService(carts *storefrontCartRepoStub, prices *storefrontPriceRepoStub, variants catalog.VariantRepository) (*checkoutApp.Service, shipping.Provider, *payment.ProviderRegistry, *storefrontCheckoutOrderRepoStub) {
	log := logger.NewWithWriter(io.Discard, "error")
	bus := event.NewBus(log)
	pipeline := pricing.NewPipeline(appPricing.NewBasePriceStep(prices), pricing.NewFinalizeStep())
	orders := &storefrontCheckoutOrderRepoStub{}
	shipments := &storefrontCheckoutShipmentRepoStub{}
	payments := &storefrontCheckoutPaymentRepoStub{}
	shippingProvider := flatrate.NewProvider(shared.MustNewMoney(500, "EUR"))
	shippingReg := shipping.NewProviderRegistry()
	shippingReg.Register(shippingProvider)
	payRegistry := testPaymentRegistry(manualpay.NewProvider())
	workflow := checkoutApp.NewWorkflow([]checkoutApp.Step{
		checkoutApp.NewValidateCartStep(variants),
		checkoutApp.NewRecalculatePricingStep(pipeline),
		checkoutApp.NewReserveInventoryStep(&storefrontCheckoutReservationRepoStub{}),
		checkoutApp.NewCreateOrderStep(orders, variants, nil, nil),
		checkoutApp.NewSelectShippingStep(shippingReg, shipments),
		checkoutApp.NewInitiatePaymentStep(payRegistry, payments),
	}, bus, log)
	return checkoutApp.NewService(carts, workflow, log), shippingProvider, payRegistry, orders
}

func TestStorefrontHandler_CheckoutAddress_GuestCanAccessAddressForm(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, carts, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	currentCart, err := cartSvc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	variants := &mockStorefrontVariantRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
		return &catalog.Variant{ID: id, ProductID: "prod-1", SKU: "SKU-1", Name: "Widget Default"}, nil
	}}
	checkoutSvc, shippingProvider, payRegistry, _ := newStorefrontCheckoutService(carts, prices, variants)
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/checkout/address", nil)
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Continue to Shipping") {
		t.Fatalf("body missing continue action for guest checkout: %s", rec.Body.String())
	}
}

func TestStorefrontHandler_CheckoutShipping_GuestRequiresContactEmail(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, carts, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	currentCart, err := cartSvc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	variants := &mockStorefrontVariantRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
		return &catalog.Variant{ID: id, ProductID: "prod-1", SKU: "SKU-1", Name: "Widget Default"}, nil
	}}
	checkoutSvc, shippingProvider, payRegistry, _ := newStorefrontCheckoutService(carts, prices, variants)
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc)
	router := newStorefrontRouter(h)

	addressRec := httptest.NewRecorder()
	addressReq := httptest.NewRequest("GET", "/checkout/address", nil)
	addressReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(addressRec, addressReq)
	if addressRec.Code != http.StatusOK {
		t.Fatalf("address status = %d, want %d; body: %s", addressRec.Code, http.StatusOK, addressRec.Body.String())
	}
	var csrf *http.Cookie
	for _, cookie := range addressRec.Result().Cookies() {
		if cookie.Name == "shopanda_csrf" {
			csrf = cookie
			break
		}
	}
	if csrf == nil {
		t.Fatal("expected checkout CSRF cookie")
	}
	form := url.Values{
		"csrf_token": {csrf.Value},
		"first_name": {"Ada"},
		"last_name":  {"Lovelace"},
		"street":     {"1 Logic Lane"},
		"city":       {"Berlin"},
		"postcode":   {"10115"},
		"country":    {"DE"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/checkout/shipping", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrf)
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Contact email is required.") {
		t.Fatalf("expected missing contact email message: %s", rec.Body.String())
	}
}

func TestStorefrontHandler_CheckoutFlow_Manual_GuestOK(t *testing.T) {
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
	checkoutSvc, shippingProvider, payRegistry, orders := newStorefrontCheckoutService(carts, prices, variants)
	h := shophttp.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc)
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 2, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	addressRec := httptest.NewRecorder()
	addressReq := httptest.NewRequest("GET", "/checkout/address", nil)
	addressReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
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
		"csrf_token":    {csrfCookie.Value},
		"contact_email": {"guest@example.com"},
		"first_name":    {"Ada"},
		"last_name":     {"Lovelace"},
		"street":        {"1 Logic Lane"},
		"city":          {"Berlin"},
		"postcode":      {"10115"},
		"country":       {"DE"},
	}
	shippingRec := httptest.NewRecorder()
	shippingReq := httptest.NewRequest("POST", "/checkout/shipping", strings.NewReader(addressForm.Encode()))
	shippingReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	shippingReq.AddCookie(csrfCookie)
	shippingReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(shippingRec, shippingReq)
	if shippingRec.Code != http.StatusOK {
		t.Fatalf("shipping status = %d, want %d; body: %s", shippingRec.Code, http.StatusOK, shippingRec.Body.String())
	}
	if !strings.Contains(shippingRec.Body.String(), "Flat Rate Shipping") {
		t.Fatalf("shipping page missing flat rate option: %s", shippingRec.Body.String())
	}

	paymentForm := url.Values{
		"csrf_token":      {csrfCookie.Value},
		"contact_email":   {"guest@example.com"},
		"first_name":      {"Ada"},
		"last_name":       {"Lovelace"},
		"street":          {"1 Logic Lane"},
		"city":            {"Berlin"},
		"postcode":        {"10115"},
		"country":         {"DE"},
		"shipping_method": {"flat_rate"},
	}
	paymentRec := httptest.NewRecorder()
	paymentReq := httptest.NewRequest("POST", "/checkout/payment", strings.NewReader(paymentForm.Encode()))
	paymentReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	paymentReq.AddCookie(csrfCookie)
	paymentReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(paymentRec, paymentReq)
	if paymentRec.Code != http.StatusOK {
		t.Fatalf("payment status = %d, want %d; body: %s", paymentRec.Code, http.StatusOK, paymentRec.Body.String())
	}
	if !strings.Contains(paymentRec.Body.String(), "Manual payment") {
		t.Fatalf("payment page missing manual payment label: %s", paymentRec.Body.String())
	}

	confirmForm := url.Values{
		"csrf_token":      {csrfCookie.Value},
		"contact_email":   {"guest@example.com"},
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
	confirmReq := httptest.NewRequest("POST", "/checkout/confirm", strings.NewReader(confirmForm.Encode()))
	confirmReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	confirmReq.AddCookie(csrfCookie)
	confirmReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(confirmRec, confirmReq)
	if confirmRec.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, want %d; body: %s", confirmRec.Code, http.StatusOK, confirmRec.Body.String())
	}
	if orders.saved == nil {
		t.Fatal("expected checkout to save an order")
	}
	if orders.saved.CustomerID != "" {
		t.Fatalf("saved order customer id = %q, want empty guest id", orders.saved.CustomerID)
	}
	if orders.saved.ContactEmail != "guest@example.com" {
		t.Fatalf("saved guest contact email = %q, want %q", orders.saved.ContactEmail, "guest@example.com")
	}
	confirmBody := confirmRec.Body.String()
	if !strings.Contains(confirmBody, "guest-confirmation-email") || !strings.Contains(confirmBody, "guest@example.com") {
		t.Fatalf("guest confirmation missing contact email notice: %s", confirmBody)
	}
	if strings.Contains(confirmBody, `id="view-order"`) {
		t.Fatalf("guest confirmation should not link to an account order page: %s", confirmBody)
	}
}

func TestStorefrontHandler_CheckoutAddress_AllowsAuthenticatedCustomerWithoutAccountSecurityWiring(t *testing.T) {
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
	checkoutSvc, shippingProvider, payRegistry, _ := newStorefrontCheckoutService(carts, prices, variants)
	h := shophttp.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc)
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	rec := httptest.NewRecorder()
	req := storefrontCustomerRequest(httptest.NewRequest("GET", "/checkout/address", nil), "cust-1")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Continue to Shipping") {
		t.Fatalf("body missing continue action: %s", rec.Body.String())
	}
}

func TestStorefrontHandler_CheckoutAddress_RedirectsToEmailVerification_WhenEmailUnverified(t *testing.T) {
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
	checkoutSvc, shippingProvider, payRegistry, _ := newStorefrontCheckoutService(carts, prices, variants)
	bus := event.NewBus(logger.New("error"))
	var published []customer.EmailVerificationRequestedData
	bus.On(customer.EventEmailVerificationRequested, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(customer.EmailVerificationRequestedData)
		if !ok {
			t.Fatalf("event data type = %T", evt.Data)
		}
		published = append(published, data)
		return nil
	})
	authSvc, _ := newStorefrontAuthServiceWithBus(t, bus)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute).WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute)
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), out.CustomerID, "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, out.CustomerID, "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	rec := httptest.NewRecorder()
	req := storefrontCustomerRequest(httptest.NewRequest("GET", "/checkout/address", nil), out.CustomerID)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if rec.Header().Get("Location") != "/account/verify-email?redirect_to=%2Fcheckout%2Faddress&sent=1" {
		t.Fatalf("location = %q, want %q", rec.Header().Get("Location"), "/account/verify-email?redirect_to=%2Fcheckout%2Faddress&sent=1")
	}
	assertStorefrontEmailVerificationEvent(t, published, "/checkout/address")
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
	checkoutSvc, shippingProvider, payRegistry, orders := newStorefrontCheckoutService(carts, prices, variants)
	authSvc, repo := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	storefrontMarkCustomerEmailVerified(t, repo, out.CustomerID)
	h := shophttp.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute).WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute)
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), out.CustomerID, "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, out.CustomerID, "var-1", 2, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	addressRec := httptest.NewRecorder()
	addressReq := storefrontCustomerRequest(httptest.NewRequest("GET", "/checkout/address", nil), out.CustomerID)
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
	shippingReq := storefrontCustomerRequest(httptest.NewRequest("POST", "/checkout/shipping", strings.NewReader(addressForm.Encode())), out.CustomerID)
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
	paymentReq := storefrontCustomerRequest(httptest.NewRequest("POST", "/checkout/payment", strings.NewReader(paymentForm.Encode())), out.CustomerID)
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
	confirmReq := storefrontCustomerRequest(httptest.NewRequest("POST", "/checkout/confirm", strings.NewReader(confirmForm.Encode())), out.CustomerID)
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
	if !strings.Contains(body, `href="/account/orders/`+orders.saved.ID+`"`) {
		t.Fatalf("confirmation page missing account order link: %s", body)
	}
	if strings.Contains(body, "guest-confirmation-email") {
		t.Fatalf("authenticated confirmation should not show guest email notice: %s", body)
	}
}

func TestStorefrontHandler_CheckoutConfirm_RedirectsToEmailVerification_WhenEmailUnverified(t *testing.T) {
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
	checkoutSvc, shippingProvider, payRegistry, orders := newStorefrontCheckoutService(carts, prices, variants)
	bus := event.NewBus(logger.New("error"))
	var published []customer.EmailVerificationRequestedData
	bus.On(customer.EventEmailVerificationRequested, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(customer.EmailVerificationRequestedData)
		if !ok {
			t.Fatalf("event data type = %T", evt.Data)
		}
		published = append(published, data)
		return nil
	})
	authSvc, _ := newStorefrontAuthServiceWithBus(t, bus)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute).WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute)
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), out.CustomerID, "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, out.CustomerID, "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	confirmForm := url.Values{
		"csrf_token":      {storefrontCheckoutCSRFCookie(t, router, out.CustomerID).Value},
		"first_name":      {"Ada"},
		"last_name":       {"Lovelace"},
		"street":          {"1 Logic Lane"},
		"city":            {"Berlin"},
		"postcode":        {"10115"},
		"country":         {"DE"},
		"shipping_method": {"flat_rate"},
		"payment_method":  {"manual"},
	}
	published = nil
	rec := httptest.NewRecorder()
	req := storefrontCustomerRequest(httptest.NewRequest("POST", "/checkout/confirm", strings.NewReader(confirmForm.Encode())), out.CustomerID)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "shopanda_csrf", Value: confirmForm.Get("csrf_token")})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("Parse location: %v", err)
	}
	if location.Path != "/account/verify-email" {
		t.Fatalf("location path = %q, want %q", location.Path, "/account/verify-email")
	}
	if location.Query().Get("sent") != "1" {
		t.Fatalf("location sent = %q, want %q", location.Query().Get("sent"), "1")
	}
	redirectTo := location.Query().Get("redirect_to")
	if !strings.HasPrefix(redirectTo, "/checkout/address?checkout_resume=") {
		t.Fatalf("location redirect_to = %q, want checkout resume redirect", redirectTo)
	}
	if orders.saved != nil {
		t.Fatalf("expected checkout to stop before order save, got order %q", orders.saved.ID)
	}
	if redirect := storefrontLatestCheckoutEmailRedirect(t, published); !strings.HasPrefix(redirect, "/checkout/address?checkout_resume=") {
		t.Fatalf("email token redirect_to = %q, want checkout resume redirect", redirect)
	}
}

func TestStorefrontHandler_CheckoutConfirm_ResumesPaymentAfterEmailVerification(t *testing.T) {
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
	checkoutSvc, shippingProvider, payRegistry, orders := newStorefrontCheckoutService(carts, prices, variants)
	bus := event.NewBus(logger.New("error"))
	var published []customer.EmailVerificationRequestedData
	bus.On(customer.EventEmailVerificationRequested, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(customer.EmailVerificationRequestedData)
		if !ok {
			t.Fatalf("event data type = %T", evt.Data)
		}
		published = append(published, data)
		return nil
	})
	authSvc, _ := newStorefrontAuthServiceWithBus(t, bus)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute).WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute)
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), out.CustomerID, "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, out.CustomerID, "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	confirmForm := url.Values{
		"csrf_token":      {storefrontCheckoutCSRFCookie(t, router, out.CustomerID).Value},
		"first_name":      {"Ada"},
		"last_name":       {"Lovelace"},
		"street":          {"1 Logic Lane"},
		"city":            {"Berlin"},
		"postcode":        {"10115"},
		"country":         {"DE"},
		"shipping_method": {"flat_rate"},
		"payment_method":  {"manual"},
	}
	published = nil
	confirmRec := httptest.NewRecorder()
	confirmReq := storefrontCustomerRequest(httptest.NewRequest("POST", "/checkout/confirm", strings.NewReader(confirmForm.Encode())), out.CustomerID)
	confirmReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	confirmReq.AddCookie(&http.Cookie{Name: "shopanda_csrf", Value: confirmForm.Get("csrf_token")})
	router.ServeHTTP(confirmRec, confirmReq)

	if confirmRec.Code != http.StatusSeeOther {
		t.Fatalf("confirm status = %d, want %d; body: %s", confirmRec.Code, http.StatusSeeOther, confirmRec.Body.String())
	}
	resumeRedirect := storefrontLatestCheckoutEmailRedirect(t, published)
	verifyURL, err := url.Parse(published[len(published)-1].VerifyURL)
	if err != nil {
		t.Fatalf("Parse verify URL: %v", err)
	}
	verifyRec := httptest.NewRecorder()
	verifyReq := httptest.NewRequest("GET", verifyURL.RequestURI(), nil)
	router.ServeHTTP(verifyRec, verifyReq)

	if verifyRec.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want %d; body: %s", verifyRec.Code, http.StatusOK, verifyRec.Body.String())
	}
	if !strings.Contains(verifyRec.Body.String(), resumeRedirect) {
		t.Fatalf("verify body missing continue redirect %q: %s", resumeRedirect, verifyRec.Body.String())
	}

	resumeRec := httptest.NewRecorder()
	resumeReq := storefrontCustomerRequest(httptest.NewRequest("GET", resumeRedirect, nil), out.CustomerID)
	router.ServeHTTP(resumeRec, resumeReq)

	if resumeRec.Code != http.StatusOK {
		t.Fatalf("resume status = %d, want %d; body: %s", resumeRec.Code, http.StatusOK, resumeRec.Body.String())
	}
	if orders.saved != nil {
		t.Fatalf("expected checkout resume to stop before saving order, got %q", orders.saved.ID)
	}
	body := resumeRec.Body.String()
	if !strings.Contains(body, "Manual payment") {
		t.Fatalf("resume body missing payment label: %s", body)
	}
	if !strings.Contains(body, "Place Order") {
		t.Fatalf("resume body missing place order action: %s", body)
	}
	if !strings.Contains(body, `name="first_name" value="Ada"`) {
		t.Fatalf("resume body missing preserved first name: %s", body)
	}
	if !strings.Contains(body, `name="shipping_method" value="flat_rate"`) {
		t.Fatalf("resume body missing preserved shipping method: %s", body)
	}
}

func TestStorefrontHandler_CheckoutAddress_FallsBackToAddressFormOnInvalidResumeToken(t *testing.T) {
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
	checkoutSvc, shippingProvider, payRegistry, orders := newStorefrontCheckoutService(carts, prices, variants)
	authSvc, repo := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	storefrontMarkCustomerEmailVerified(t, repo, out.CustomerID)
	h := shophttp.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute).WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute)
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), out.CustomerID, "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, out.CustomerID, "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	for _, tc := range []struct {
		name  string
		token string
	}{
		{name: "garbage", token: "notavalidtoken"},
		{name: "tampered base64", token: "AAAA" + strings.Repeat("X", 60)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := storefrontCustomerRequest(
				httptest.NewRequest("GET", "/checkout/address?checkout_resume="+url.QueryEscape(tc.token), nil),
				out.CustomerID,
			)
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "Continue to Shipping") {
				t.Fatalf("expected checkout address form fallback, got body: %s", rec.Body.String())
			}
			if orders.saved != nil {
				t.Fatal("expected no order to be saved on bad resume token")
			}
		})
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
	payRegistry := testPaymentRegistry(manualpay.NewProvider())
	authSvc, repo := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	storefrontMarkCustomerEmailVerified(t, repo, out.CustomerID)
	h := shophttp.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute).WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute)
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), out.CustomerID, "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, out.CustomerID, "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	confirmForm := url.Values{
		"csrf_token":      {storefrontCheckoutCSRFCookie(t, router, out.CustomerID).Value},
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
	req := storefrontCustomerRequest(httptest.NewRequest("POST", "/checkout/confirm", strings.NewReader(confirmForm.Encode())), out.CustomerID)
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
	checkoutSvc, shippingProvider, payRegistry, _ := newStorefrontCheckoutService(carts, prices, variants)
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc)

	currentCart, err := cartSvc.CreateCart(context.Background(), "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
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
