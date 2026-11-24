package storefront_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
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
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/application/hooks"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/customer"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/infrastructure/flatrate"
	"github.com/akarso/shopanda/internal/infrastructure/manualpay"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/apperror"
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
func (r *storefrontCheckoutReservationRepoStub) ReleaseExpiredBefore(_ context.Context, _ time.Time) ([]inventory.ReleasedReservation, error) {
	return nil, nil
}

type storefrontCheckoutOrderRepoStub struct {
	saved *order.Order
}

func (r *storefrontCheckoutOrderRepoStub) FindByID(_ context.Context, _ string) (*order.Order, error) {
	return nil, nil
}

func (r *storefrontCheckoutOrderRepoStub) FindByIDs(ctx context.Context, ids []string) (map[string]*order.Order, error) {
	out := make(map[string]*order.Order, len(ids))
	for _, id := range ids {
		o, err := r.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if o != nil {
			out[o.ID] = o
		}
	}
	return out, nil
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
func (r *storefrontCheckoutOrderRepoStub) ListCreatedSince(context.Context, time.Time, string, int) ([]order.Order, error) {
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

const storefrontCheckoutFlashTestSecret = "checkout-flash-test-secret"

func withCheckoutFlash(h *storefront.StorefrontHandler) *storefront.StorefrontHandler {
	return h.WithCheckoutFlashSecret(storefrontCheckoutFlashTestSecret).WithPublicBaseURL("https://shop.test")
}

func signCheckoutFlashCookie(t *testing.T, payload map[string]interface{}) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte("storefront-checkout-flash:"+storefrontCheckoutFlashTestSecret))
	_, _ = mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// followCheckoutConfirmPRG follows POST /checkout/confirm → 303 → GET with the
// placed-order flash cookie so tests assert the rendered confirm page.
func followCheckoutConfirmPRG(t *testing.T, router http.Handler, postRec *httptest.ResponseRecorder, cartID string, extraCookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	if postRec.Code != http.StatusSeeOther {
		t.Fatalf("confirm POST status = %d, want 303; body: %s", postRec.Code, postRec.Body.String())
	}
	loc := postRec.Header().Get("Location")
	if loc != "/checkout/confirm" {
		t.Fatalf("confirm Location = %q, want /checkout/confirm", loc)
	}
	getRec := httptest.NewRecorder()
	getReq := httptest.NewRequest("GET", loc, nil)
	getReq.Host = "shop.test"
	if cartID != "" {
		getReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: cartID})
	}
	for _, c := range extraCookies {
		if c != nil {
			getReq.AddCookie(c)
		}
	}
	for _, c := range postRec.Result().Cookies() {
		getReq.AddCookie(c)
	}
	router.ServeHTTP(getRec, getReq)
	return getRec
}

type pendingStripeProviderStub struct{}

func (pendingStripeProviderStub) Method() payment.PaymentMethod { return payment.MethodStripe }

func (pendingStripeProviderStub) Initiate(_ context.Context, _ *payment.Payment) (payment.ProviderResult, error) {
	return payment.ProviderResult{
		ProviderRef:  "pi_test_ssr_abc",
		Pending:      true,
		ClientSecret: "pi_test_ssr_abc_secret_xyz",
	}, nil
}

func newStorefrontCheckoutService(carts *storefrontCartRepoStub, prices *storefrontPriceRepoStub, products catalog.ProductRepository, variants catalog.VariantRepository) (*checkoutApp.Service, shipping.Provider, *payment.ProviderRegistry, *storefrontCheckoutOrderRepoStub, *storefrontCheckoutShipmentRepoStub) {
	return newStorefrontCheckoutServiceWithProviders(carts, prices, products, variants, manualpay.NewProvider())
}

func newStorefrontCheckoutServiceWithProviders(carts *storefrontCartRepoStub, prices *storefrontPriceRepoStub, products catalog.ProductRepository, variants catalog.VariantRepository, providers ...payment.Provider) (*checkoutApp.Service, shipping.Provider, *payment.ProviderRegistry, *storefrontCheckoutOrderRepoStub, *storefrontCheckoutShipmentRepoStub) {
	log := logger.NewWithWriter(io.Discard, "error")
	bus := event.NewBus(log)
	pipeline := pricing.NewPipeline(appPricing.NewBasePriceStepLegacy(prices), pricing.NewFinalizeStep())
	orders := &storefrontCheckoutOrderRepoStub{}
	shipments := &storefrontCheckoutShipmentRepoStub{}
	payments := &storefrontCheckoutPaymentRepoStub{}
	shippingProvider := flatrate.NewProvider(shared.MustNewMoney(500, "EUR"))
	shippingReg := shipping.NewProviderRegistry()
	shippingReg.Register(shippingProvider)
	payRegistry := testPaymentRegistry(providers...)
	if products == nil {
		products = &mockStorefrontRepo{}
	}
	workflow := checkoutApp.NewWorkflow([]checkoutApp.Step{
		checkoutApp.NewValidateCartStep(variants, products, cartApp.EmptyBundleComponents(), cartApp.PermissiveStock()),
		checkoutApp.NewRecalculatePricingStep(pipeline),
		checkoutApp.NewReserveInventoryStep(&storefrontCheckoutReservationRepoStub{}, products, variants, cartApp.EmptyBundleComponents()),
		checkoutApp.NewCreateOrderStep(orders, variants, nil, nil, products, cartApp.EmptyBundleComponents(), prices),
		checkoutApp.NewSelectShippingStep(shippingReg, shipments),
		checkoutApp.NewInitiatePaymentStep(payRegistry, payments),
	}, bus, log)
	return checkoutApp.NewService(carts, workflow, log), shippingProvider, payRegistry, orders, shipments
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
	products := &mockStorefrontRepo{}
	checkoutSvc, shippingProvider, payRegistry, _, _ := newStorefrontCheckoutService(carts, prices, products, variants)
	h := storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc)

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
	products := &mockStorefrontRepo{}
	checkoutSvc, shippingProvider, payRegistry, _, _ := newStorefrontCheckoutService(carts, prices, products, variants)
	h := storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc)
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
	checkoutSvc, shippingProvider, payRegistry, orders, _ := newStorefrontCheckoutService(carts, prices, products, variants)
	h := withCheckoutFlash(storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc))
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
	confirmPost := httptest.NewRecorder()
	confirmReq := httptest.NewRequest("POST", "/checkout/confirm", strings.NewReader(confirmForm.Encode()))
	confirmReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	confirmReq.AddCookie(csrfCookie)
	confirmReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(confirmPost, confirmReq)
	confirmRec := followCheckoutConfirmPRG(t, router, confirmPost, currentCart.ID)
	if confirmRec.Code != http.StatusOK {
		t.Fatalf("confirm GET status = %d, want %d; body: %s", confirmRec.Code, http.StatusOK, confirmRec.Body.String())
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

func TestStorefrontHandler_CheckoutConfirm_GuestStripeRendersStripeJS(t *testing.T) {
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
	checkoutSvc, shippingProvider, payRegistry, orders, _ := newStorefrontCheckoutServiceWithProviders(carts, prices, products, variants, pendingStripeProviderStub{})
	h := withCheckoutFlash(storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithCart(variants, cartSvc).
		WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc).
		WithStripePublishableKey("pk_test_ssr_confirm").
		WithCSPEnabled(true))
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	addressRec := httptest.NewRecorder()
	addressReq := httptest.NewRequest("GET", "/checkout/address", nil)
	addressReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(addressRec, addressReq)
	if addressRec.Code != http.StatusOK {
		t.Fatalf("address status = %d; body: %s", addressRec.Code, addressRec.Body.String())
	}
	if addrCSP := addressRec.Header().Get("Content-Security-Policy"); strings.Contains(addrCSP, "js.stripe.com") {
		t.Fatalf("address CSP must not include Stripe: %q", addrCSP)
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
		"payment_method":  {"stripe"},
	}
	confirmPost := httptest.NewRecorder()
	confirmReq := httptest.NewRequest("POST", "/checkout/confirm", strings.NewReader(confirmForm.Encode()))
	confirmReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	confirmReq.AddCookie(csrfCookie)
	confirmReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(confirmPost, confirmReq)
	confirmRec := followCheckoutConfirmPRG(t, router, confirmPost, currentCart.ID)

	if confirmRec.Code != http.StatusOK {
		t.Fatalf("confirm GET status = %d, want 200; body: %s", confirmRec.Code, confirmRec.Body.String())
	}
	if orders.saved == nil {
		t.Fatal("expected checkout to save an order")
	}
	body := confirmRec.Body.String()
	if !strings.Contains(body, "Complete Payment") {
		t.Fatalf("missing Complete Payment heading: %s", body)
	}
	if !strings.Contains(body, `data-client-secret="pi_test_ssr_abc_secret_xyz"`) {
		t.Fatalf("missing client_secret in confirm HTML: %s", body)
	}
	if !strings.Contains(body, `data-publishable-key="pk_test_ssr_confirm"`) {
		t.Fatalf("missing publishable key in confirm HTML: %s", body)
	}
	if !strings.Contains(body, `data-return-url="https://shop.test/checkout/payment/return?order_id=`) {
		t.Fatalf("missing return_url with order_id: %s", body)
	}
	var flashCookie *http.Cookie
	for _, c := range confirmRec.Result().Cookies() {
		if c.Name == "shopanda_checkout_placed" {
			flashCookie = c
			break
		}
	}
	if flashCookie == nil {
		t.Fatal("expected reminted flash cookie for Stripe pending confirm")
	}
	if !strings.Contains(flashCookie.Value, ".") {
		t.Fatalf("flash cookie must be HMAC-signed payload.sig, got %q", flashCookie.Value)
	}
	if !strings.Contains(body, "https://js.stripe.com/v3/") {
		t.Fatalf("missing Stripe.js script: %s", body)
	}
	if cc := confirmRec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want private, no-store", cc)
	}
	csp := confirmRec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "https://js.stripe.com") {
		t.Fatalf("CSP missing js.stripe.com: %q", csp)
	}
	if !strings.Contains(csp, "frame-src") {
		t.Fatalf("CSP missing frame-src for Stripe Elements: %q", csp)
	}
}

func TestStorefrontHandler_CheckoutConfirm_MissingPublishableKey_ShopperSafe(t *testing.T) {
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
	checkoutSvc, shippingProvider, payRegistry, _, _ := newStorefrontCheckoutServiceWithProviders(carts, prices, products, variants, pendingStripeProviderStub{})
	h := withCheckoutFlash(storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithCart(variants, cartSvc).
		WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc))
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	addressRec := httptest.NewRecorder()
	addressReq := httptest.NewRequest("GET", "/checkout/address", nil)
	addressReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(addressRec, addressReq)
	var csrfCookie *http.Cookie
	for _, cookie := range addressRec.Result().Cookies() {
		if cookie.Name == "shopanda_csrf" {
			csrfCookie = cookie
			break
		}
	}
	if csrfCookie == nil {
		t.Fatal("expected CSRF cookie")
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
		"payment_method":  {"stripe"},
	}
	confirmPost := httptest.NewRecorder()
	confirmReq := httptest.NewRequest("POST", "/checkout/confirm", strings.NewReader(confirmForm.Encode()))
	confirmReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	confirmReq.AddCookie(csrfCookie)
	confirmReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(confirmPost, confirmReq)
	confirmRec := followCheckoutConfirmPRG(t, router, confirmPost, currentCart.ID)
	body := confirmRec.Body.String()
	if !strings.Contains(body, "stripe-misconfigured") {
		t.Fatalf("expected shopper-safe misconfig copy: %s", body)
	}
	if strings.Contains(body, "SHOPANDA_PAYMENT_STRIPE_PUBLISHABLE_KEY") || strings.Contains(body, "client_secret") {
		t.Fatalf("leaked ops instructions to shopper: %s", body)
	}
	if strings.Contains(body, "data-client-secret=") {
		t.Fatalf("must not embed client_secret without publishable key: %s", body)
	}
	cleared := false
	for _, c := range confirmRec.Result().Cookies() {
		if c.Name == "shopanda_checkout_placed" && (c.MaxAge < 0 || c.Value == "") {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("misconfig must clear confirm flash cookie (no client_secret left in jar)")
	}
}

func TestStorefrontHandler_CheckoutConfirm_MissingPublicBaseURL_Misconfig(t *testing.T) {
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
	checkoutSvc, shippingProvider, payRegistry, _, _ := newStorefrontCheckoutServiceWithProviders(carts, prices, products, variants, pendingStripeProviderStub{})
	h := storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithCart(variants, cartSvc).
		WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc).
		WithCheckoutFlashSecret(storefrontCheckoutFlashTestSecret).
		WithStripePublishableKey("pk_test_ssr_confirm")
		// deliberately no WithPublicBaseURL
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	addressRec := httptest.NewRecorder()
	addressReq := httptest.NewRequest("GET", "/checkout/address", nil)
	addressReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(addressRec, addressReq)
	var csrfCookie *http.Cookie
	for _, cookie := range addressRec.Result().Cookies() {
		if cookie.Name == "shopanda_csrf" {
			csrfCookie = cookie
			break
		}
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
		"payment_method":  {"stripe"},
	}
	confirmPost := httptest.NewRecorder()
	confirmReq := httptest.NewRequest("POST", "/checkout/confirm", strings.NewReader(confirmForm.Encode()))
	confirmReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	confirmReq.AddCookie(csrfCookie)
	confirmReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(confirmPost, confirmReq)
	confirmRec := followCheckoutConfirmPRG(t, router, confirmPost, currentCart.ID)
	if !strings.Contains(confirmRec.Body.String(), "stripe-misconfigured") {
		t.Fatalf("expected misconfig without public_base_url: %s", confirmRec.Body.String())
	}
	for _, c := range confirmRec.Result().Cookies() {
		if c.Name == "shopanda_checkout_placed" && c.Value != "" && c.MaxAge >= 0 {
			t.Fatalf("misconfig must clear flash, got %+v", c)
		}
	}
}

func TestStorefrontHandler_CheckoutConfirm_ForgedFlashRedirects(t *testing.T) {
	engine := createTestTheme(t)
	h := withCheckoutFlash(storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](), composition.NewPipeline[composition.ListingContext](), newStorefrontSearchMock()))
	router := newStorefrontRouter(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/checkout/confirm", nil)
	req.AddCookie(&http.Cookie{Name: "shopanda_checkout_placed", Value: " forgery.notvalid"})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/checkout/address" {
		t.Fatalf("forged flash: status=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestStorefrontHandler_CheckoutConfirm_ExpiredFlashRedirects(t *testing.T) {
	engine := createTestTheme(t)
	h := withCheckoutFlash(storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(),
		composition.NewPipeline[composition.ProductContext](), composition.NewPipeline[composition.ListingContext](), newStorefrontSearchMock()))
	router := newStorefrontRouter(h)

	value := signCheckoutFlashCookie(t, map[string]interface{}{
		"order_id":     "ord-expired",
		"continue_url": "/products",
		"exp":          time.Now().UTC().Add(-time.Minute).Unix(),
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/checkout/confirm", nil)
	req.AddCookie(&http.Cookie{Name: "shopanda_checkout_placed", Value: value})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/checkout/address" {
		t.Fatalf("expired flash: status=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestStorefrontHandler_CheckoutConfirm_FlashSecureUnderTLS(t *testing.T) {
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
	checkoutSvc, shippingProvider, payRegistry, _, _ := newStorefrontCheckoutService(carts, prices, products, variants)
	h := withCheckoutFlash(storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithCart(variants, cartSvc).
		WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc))
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	addressRec := httptest.NewRecorder()
	addressReq := httptest.NewRequest("GET", "/checkout/address", nil)
	addressReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(addressRec, addressReq)
	var csrfCookie *http.Cookie
	for _, cookie := range addressRec.Result().Cookies() {
		if cookie.Name == "shopanda_csrf" {
			csrfCookie = cookie
			break
		}
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
	confirmPost := httptest.NewRecorder()
	confirmReq := httptest.NewRequest("POST", "/checkout/confirm", strings.NewReader(confirmForm.Encode()))
	confirmReq.TLS = &tls.ConnectionState{}
	confirmReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	confirmReq.AddCookie(csrfCookie)
	confirmReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(confirmPost, confirmReq)
	if confirmPost.Code != http.StatusSeeOther {
		t.Fatalf("status = %d; body: %s", confirmPost.Code, confirmPost.Body.String())
	}
	var flash *http.Cookie
	for _, c := range confirmPost.Result().Cookies() {
		if c.Name == "shopanda_checkout_placed" {
			flash = c
			break
		}
	}
	if flash == nil {
		t.Fatal("expected flash cookie on place-order redirect")
	}
	if !flash.Secure {
		t.Fatalf("flash Secure = false, want true under TLS")
	}
}

func TestStorefrontHandler_CheckoutConfirm_UnavailableReturns503(t *testing.T) {
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
	workflow := checkoutApp.NewWorkflow([]checkoutApp.Step{failingCheckoutStep{err: apperror.Unavailable("cart port down")}}, event.NewBus(log), log)
	checkoutSvc := checkoutApp.NewService(carts, workflow, log)
	shippingProvider := flatrate.NewProvider(shared.MustNewMoney(500, "EUR"))
	payRegistry := testPaymentRegistry(manualpay.NewProvider())
	h := storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithCart(variants, cartSvc).
		WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc)
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	addressRec := httptest.NewRecorder()
	addressReq := httptest.NewRequest("GET", "/checkout/address", nil)
	addressReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(addressRec, addressReq)
	var csrfCookie *http.Cookie
	for _, cookie := range addressRec.Result().Cookies() {
		if cookie.Name == "shopanda_csrf" {
			csrfCookie = cookie
			break
		}
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
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/checkout/confirm", strings.NewReader(confirmForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "cart port down") {
		t.Fatalf("expected unavailable message pass-through: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Sorry, something went wrong") {
		t.Fatalf("503 must not use generic 500 mask: %s", rec.Body.String())
	}
}

func TestStorefrontHandler_CheckoutPaymentReturn_NeutralCopy(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := withCheckoutFlash(storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()))
	router := newStorefrontRouter(h)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/checkout/payment/return?order_id=ord-1&redirect_status=succeeded", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Order #ord-1") {
		t.Fatalf("missing order id: %s", body)
	}
	if !strings.Contains(body, "Checking payment status") {
		t.Fatalf("expected neutral notice, got: %s", body)
	}
	if strings.Contains(body, "Payment confirmed") {
		t.Fatalf("must not trust redirect_status=succeeded for paid copy: %s", body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("Cache-Control = %q", cc)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "js.stripe.com") {
		t.Fatalf("return page must not widen Stripe CSP: %q", csp)
	}
}

func paymentReturnOrderFixture(t *testing.T, ownerID string) (*storefrontAccountOrderRepoStub, extensionapp.ValueAPI) {
	t.Helper()
	orders := newStorefrontAccountOrderRepoStub()
	item, err := order.NewItem("var-1", "SKU-SECRET", "Secret Widget", 1, shared.MustNewMoney(9999, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder("ord-secret", ownerID, "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	orders.byID[o.ID] = &o
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift", Type: domainext.FieldTypeString,
		Scope: domainext.TargetOrderItem, StorageMode: domainext.StorageSnapshot,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	extRepo := newOrderTestExtensionValueRepo()
	msg := "owner-gift"
	extRepo.values[orderTestExtKey(domainext.OrderItemTarget("ord-secret", "var-1"), "acme.gift.message")] = domainext.Value{
		FieldCode: "acme.gift.message", TargetType: domainext.TargetOrderItem, TargetID: "ord-secret:var-1",
		Payload: domainext.ValuePayload{StringValue: &msg},
	}
	return orders, extensionapp.NewValueService(reg, extRepo)
}

func TestStorefrontHandler_CheckoutPaymentReturn_AnonymousOrderIDHasNoLines(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	orders, values := paymentReturnOrderFixture(t, "owner-1")
	h := withCheckoutFlash(storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithAccount(nil, orders, &storefrontAccountDeleterStub{}).
		WithExtensions(values))
	router := newStorefrontRouter(h)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/checkout/payment/return?order_id=ord-secret", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leak := range []string{"Secret Widget", "SKU-SECRET", "owner-gift", "confirm-line", "view-order"} {
		if strings.Contains(body, leak) {
			t.Fatalf("unsigned return leaked %q: %s", leak, body)
		}
	}
}

func TestStorefrontHandler_CheckoutPaymentReturn_OwnerSeesLines(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	orders, values := paymentReturnOrderFixture(t, "owner-1")
	h := withCheckoutFlash(storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithAccount(nil, orders, &storefrontAccountDeleterStub{}).
		WithExtensions(values))
	router := newStorefrontRouter(h)

	rec := httptest.NewRecorder()
	req := storefrontCustomerRequest(httptest.NewRequest("GET", "/checkout/payment/return?order_id=ord-secret", nil), "owner-1")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Secret Widget") || !strings.Contains(body, "SKU-SECRET") || !strings.Contains(body, "Gift:owner-gift") {
		t.Fatalf("owner missing line data: %s", body)
	}
	if !strings.Contains(body, `id="view-order"`) || !strings.Contains(body, "/account/orders/ord-secret") {
		t.Fatalf("owner missing View Order: %s", body)
	}
}

func TestStorefrontHandler_CheckoutPaymentReturn_OtherCustomerHasNoLines(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	orders, values := paymentReturnOrderFixture(t, "owner-1")
	h := withCheckoutFlash(storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithAccount(nil, orders, &storefrontAccountDeleterStub{}).
		WithExtensions(values))
	router := newStorefrontRouter(h)

	rec := httptest.NewRecorder()
	req := storefrontCustomerRequest(httptest.NewRequest("GET", "/checkout/payment/return?order_id=ord-secret", nil), "other-9")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leak := range []string{"Secret Widget", "SKU-SECRET", "owner-gift", "confirm-line", "view-order", "/account/orders/ord-secret"} {
		if strings.Contains(body, leak) {
			t.Fatalf("other customer leaked %q: %s", leak, body)
		}
	}
}

func TestStorefrontHandler_CheckoutPaymentReturn_MatchingFlashShowsGuestLines(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	orders := newStorefrontAccountOrderRepoStub()
	item, err := order.NewItem("var-1", "SKU-1", "Guest Widget", 1, shared.MustNewMoney(1500, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	// Guest order: empty CustomerID.
	o, err := order.NewOrder("ord-guest", "", "guest@example.com", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	orders.byID[o.ID] = &o
	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift message", Type: domainext.FieldTypeString,
		Scope: domainext.TargetOrderItem, StorageMode: domainext.StorageSnapshot,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	extRepo := newOrderTestExtensionValueRepo()
	msg := "Hello guest"
	extRepo.values[orderTestExtKey(domainext.OrderItemTarget("ord-guest", "var-1"), "acme.gift.message")] = domainext.Value{
		FieldCode: "acme.gift.message", TargetType: domainext.TargetOrderItem, TargetID: "ord-guest:var-1",
		Payload: domainext.ValuePayload{StringValue: &msg},
	}
	values := extensionapp.NewValueService(reg, extRepo)

	h := withCheckoutFlash(storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithAccount(nil, orders, &storefrontAccountDeleterStub{}).
		WithExtensions(values))
	router := newStorefrontRouter(h)

	flash := signCheckoutFlashCookie(t, map[string]interface{}{
		"order_id":     "ord-guest",
		"status":       "pending",
		"total_text":   "EUR 15.00",
		"notice":       "Thanks",
		"continue_url": "/products",
		"guest_email":  "guest@example.com",
		"exp":          time.Now().UTC().Add(10 * time.Minute).Unix(),
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/checkout/payment/return?order_id=ord-guest", nil)
	req.AddCookie(&http.Cookie{Name: "shopanda_checkout_placed", Value: flash})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Guest Widget") || !strings.Contains(body, "Gift message:Hello guest") {
		t.Fatalf("matching flash should show guest lines: %s", body)
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "shopanda_checkout_placed" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("expected confirm flash cleared after payment return")
	}
}

func TestStorefrontHandler_CheckoutPaymentReturn_MismatchedFlashNoLines(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	orders, values := paymentReturnOrderFixture(t, "owner-1")
	h := withCheckoutFlash(storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithAccount(nil, orders, &storefrontAccountDeleterStub{}).
		WithExtensions(values))
	router := newStorefrontRouter(h)

	// Flash for ord-secret; query asks for a different id. Session is neither owner.
	flash := signCheckoutFlashCookie(t, map[string]interface{}{
		"order_id":       "ord-secret",
		"status":         "pending",
		"total_text":     "EUR 99.99",
		"notice":         "Thanks",
		"continue_url":   "/products",
		"view_order_url": "/account/orders/ord-secret",
		"exp":            time.Now().UTC().Add(10 * time.Minute).Unix(),
	})
	rec := httptest.NewRecorder()
	req := storefrontCustomerRequest(httptest.NewRequest("GET", "/checkout/payment/return?order_id=ord-other", nil), "other-9")
	req.AddCookie(&http.Cookie{Name: "shopanda_checkout_placed", Value: flash})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leak := range []string{"Secret Widget", "SKU-SECRET", "owner-gift", "confirm-line", "view-order", "/account/orders/ord-secret"} {
		if strings.Contains(body, leak) {
			t.Fatalf("mismatched flash leaked %q: %s", leak, body)
		}
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "shopanda_checkout_placed" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("expected confirm flash cleared even when order_id mismatches")
	}
}

func TestStorefrontHandler_CheckoutConfirm_FlashRendersLineExtensions(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	orders := newStorefrontAccountOrderRepoStub()
	item, err := order.NewItem("var-1", "SKU-1", "Widget", 1, shared.MustNewMoney(1500, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder("ord-1", "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	orders.byID[o.ID] = &o

	reg := extensionapp.NewRegistry()
	if err := reg.Register(domainext.FieldDef{
		Code: "acme.gift.message", Label: "Gift message", Type: domainext.FieldTypeString,
		Scope: domainext.TargetOrderItem, StorageMode: domainext.StorageSnapshot,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	extRepo := newOrderTestExtensionValueRepo()
	msg := "Hello"
	extRepo.values[orderTestExtKey(domainext.OrderItemTarget("ord-1", "var-1"), "acme.gift.message")] = domainext.Value{
		FieldCode: "acme.gift.message", TargetType: domainext.TargetOrderItem, TargetID: "ord-1:var-1",
		Payload: domainext.ValuePayload{StringValue: &msg},
	}
	values := extensionapp.NewValueService(reg, extRepo)

	extensionapp.SetImageHostLookupForTest(t, func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	hookReg := hooks.NewRegistry(nil)
	_ = hookReg.Register(hooks.HookOrderLineDisplay, 100, "metaldogs", func(ctx *hooks.Context) error {
		raw, _ := ctx.Get("fields")
		fields := raw.(*[]extapi.OrderLineDisplayField)
		*fields = append(*fields,
			extapi.OrderLineDisplayField{
				Code: "plugin.thumb", Label: "Design", Kind: extapi.DisplayKindImage,
				Value: "https://cdn.example/confirm.png", Alt: "thumb",
			},
			extapi.OrderLineDisplayField{
				Code: "plugin.http", Kind: extapi.DisplayKindImage, Value: "http://cdn.example/no.png",
			},
		)
		return nil
	})

	h := withCheckoutFlash(storefront.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithAccount(nil, orders, &storefrontAccountDeleterStub{}).
		WithExtensions(values).
		WithOrderLineHooks(hookReg))
	router := newStorefrontRouter(h)

	flash := signCheckoutFlashCookie(t, map[string]interface{}{
		"order_id":       "ord-1",
		"status":         "pending",
		"total_text":     "EUR 15.00",
		"notice":         "Thanks",
		"continue_url":   "/products",
		"needs_shipping": false,
		"exp":            time.Now().UTC().Add(10 * time.Minute).Unix(),
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/checkout/confirm", nil)
	req.AddCookie(&http.Cookie{Name: "shopanda_checkout_placed", Value: flash})
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-variant="var-1"`) || !strings.Contains(body, "Gift message:Hello") {
		t.Fatalf("confirm flash missing line extensions: %s", body)
	}
	if !strings.Contains(body, `src="https://cdn.example/confirm.png"`) {
		t.Fatalf("confirm missing https image: %s", body)
	}
	if strings.Contains(body, "http://cdn.example/no.png") {
		t.Fatalf("http image must not render: %s", body)
	}
}

func TestStorefrontHandler_CheckoutFlow_DigitalOnly_SkipsShipping(t *testing.T) {
	products := &mockStorefrontRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
		return &catalog.Product{ID: id, Name: "Virtual Card", Slug: "card", Status: catalog.StatusActive, Type: catalog.TypeVirtual}, nil
	}}
	variants := &mockStorefrontVariantRepo{findByIDFn: func(_ context.Context, id string) (*catalog.Variant, error) {
		return &catalog.Variant{ID: id, ProductID: "prod-1", SKU: "VIRT-1", Name: "Default"}, nil
	}}
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, carts, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	checkoutSvc, shippingProvider, payRegistry, orders, shipments := newStorefrontCheckoutService(carts, prices, products, variants)
	h := withCheckoutFlash(storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc))
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, "", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	addressRec := httptest.NewRecorder()
	addressReq := httptest.NewRequest("GET", "/checkout/address", nil)
	addressReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(addressRec, addressReq)
	if addressRec.Code != http.StatusOK {
		t.Fatalf("address status = %d, want %d; body: %s", addressRec.Code, http.StatusOK, addressRec.Body.String())
	}
	body := addressRec.Body.String()
	if !strings.Contains(body, "Continue to Payment") {
		t.Fatalf("digital address page should continue to payment: %s", body)
	}
	if !strings.Contains(body, `action="/checkout/payment"`) {
		t.Fatalf("digital address form should post to payment: %s", body)
	}
	if strings.Contains(body, `data-progress="Shipping"`) {
		t.Fatalf("digital progress should omit Shipping: %s", body)
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
		"contact_email": {"digital@example.com"},
		"first_name":    {"Ada"},
		"last_name":     {"Lovelace"},
		"street":        {"1 Logic Lane"},
		"city":          {"Berlin"},
		"postcode":      {"10115"},
		"country":       {"DE"},
	}
	paymentRec := httptest.NewRecorder()
	paymentReq := httptest.NewRequest("POST", "/checkout/payment", strings.NewReader(addressForm.Encode()))
	paymentReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	paymentReq.AddCookie(csrfCookie)
	paymentReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(paymentRec, paymentReq)
	if paymentRec.Code != http.StatusOK {
		t.Fatalf("payment status = %d, want %d; body: %s", paymentRec.Code, http.StatusOK, paymentRec.Body.String())
	}

	confirmForm := url.Values{
		"csrf_token":     {csrfCookie.Value},
		"contact_email":  {"digital@example.com"},
		"first_name":     {"Ada"},
		"last_name":      {"Lovelace"},
		"street":         {"1 Logic Lane"},
		"city":           {"Berlin"},
		"postcode":       {"10115"},
		"country":        {"DE"},
		"payment_method": {"manual"},
	}
	confirmPost := httptest.NewRecorder()
	confirmReq := httptest.NewRequest("POST", "/checkout/confirm", strings.NewReader(confirmForm.Encode()))
	confirmReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	confirmReq.AddCookie(csrfCookie)
	confirmReq.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(confirmPost, confirmReq)
	confirmRec := followCheckoutConfirmPRG(t, router, confirmPost, currentCart.ID)
	if confirmRec.Code != http.StatusOK {
		t.Fatalf("confirm GET status = %d, want %d; body: %s", confirmRec.Code, http.StatusOK, confirmRec.Body.String())
	}
	if orders.saved == nil {
		t.Fatal("expected digital checkout to save an order")
	}
	if shipments.created != nil {
		t.Fatal("digital checkout must not create a shipment")
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
	checkoutSvc, shippingProvider, payRegistry, _, _ := newStorefrontCheckoutService(carts, prices, products, variants)
	h := storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc)
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
	checkoutSvc, shippingProvider, payRegistry, _, _ := newStorefrontCheckoutService(carts, prices, products, variants)
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
	h := storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute).WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute)
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
	checkoutSvc, shippingProvider, payRegistry, orders, _ := newStorefrontCheckoutService(carts, prices, products, variants)
	authSvc, repo := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	storefrontMarkCustomerEmailVerified(t, repo, out.CustomerID)
	h := withCheckoutFlash(storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute).WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute))
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
	confirmPost := httptest.NewRecorder()
	confirmReq := storefrontCustomerRequest(httptest.NewRequest("POST", "/checkout/confirm", strings.NewReader(confirmForm.Encode())), out.CustomerID)
	confirmReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	confirmReq.AddCookie(csrfCookie)
	router.ServeHTTP(confirmPost, confirmReq)
	getRec := httptest.NewRecorder()
	if confirmPost.Code != http.StatusSeeOther {
		t.Fatalf("confirm POST status = %d, want 303; body: %s", confirmPost.Code, confirmPost.Body.String())
	}
	getReq := storefrontCustomerRequest(httptest.NewRequest("GET", confirmPost.Header().Get("Location"), nil), out.CustomerID)
	for _, c := range confirmPost.Result().Cookies() {
		getReq.AddCookie(c)
	}
	router.ServeHTTP(getRec, getReq)
	confirmRec := getRec
	if confirmRec.Code != http.StatusOK {
		t.Fatalf("confirm GET status = %d, want %d; body: %s", confirmRec.Code, http.StatusOK, confirmRec.Body.String())
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
	checkoutSvc, shippingProvider, payRegistry, orders, _ := newStorefrontCheckoutService(carts, prices, products, variants)
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
	h := storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute).WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute)
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
	checkoutSvc, shippingProvider, payRegistry, orders, _ := newStorefrontCheckoutService(carts, prices, products, variants)
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
	h := storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute).WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute)
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
	checkoutSvc, shippingProvider, payRegistry, orders, _ := newStorefrontCheckoutService(carts, prices, products, variants)
	authSvc, repo := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	storefrontMarkCustomerEmailVerified(t, repo, out.CustomerID)
	h := storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute).WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute)
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
	h := storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute).WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute)
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
	products := &mockStorefrontRepo{}
	checkoutSvc, shippingProvider, payRegistry, _, _ := newStorefrontCheckoutService(carts, prices, products, variants)
	h := storefront.NewStorefrontHandler(engine, products, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithCart(variants, cartSvc).WithCheckout([]shipping.Provider{shippingProvider}, payRegistry, checkoutSvc)

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
