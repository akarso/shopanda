package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	appAuth "github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

// --- in-memory saved address repository stub ---

type storefrontAddressRepoStub struct {
	byID map[string]*customer.Address
}

func newStorefrontAddressRepoStub() *storefrontAddressRepoStub {
	return &storefrontAddressRepoStub{byID: map[string]*customer.Address{}}
}

func (r *storefrontAddressRepoStub) ListByCustomer(_ context.Context, customerID string) ([]customer.Address, error) {
	var out []customer.Address
	for _, a := range r.byID {
		if a.CustomerID == customerID {
			out = append(out, *a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDefault != out[j].IsDefault {
			return out[i].IsDefault
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *storefrontAddressRepoStub) FindByID(_ context.Context, addressID string) (*customer.Address, error) {
	a, ok := r.byID[addressID]
	if !ok {
		return nil, nil
	}
	clone := *a
	return &clone, nil
}

func (r *storefrontAddressRepoStub) FindDefault(_ context.Context, customerID string) (*customer.Address, error) {
	for _, a := range r.byID {
		if a.CustomerID == customerID && a.IsDefault {
			clone := *a
			return &clone, nil
		}
	}
	return nil, nil
}

func (r *storefrontAddressRepoStub) clearDefault(customerID string) {
	for _, a := range r.byID {
		if a.CustomerID == customerID {
			a.IsDefault = false
		}
	}
}

func (r *storefrontAddressRepoStub) hasAny(customerID string) bool {
	for _, a := range r.byID {
		if a.CustomerID == customerID {
			return true
		}
	}
	return false
}

func (r *storefrontAddressRepoStub) Create(_ context.Context, a *customer.Address) error {
	makeDefault := a.IsDefault || !r.hasAny(a.CustomerID)
	if makeDefault {
		r.clearDefault(a.CustomerID)
	}
	a.IsDefault = makeDefault
	clone := *a
	r.byID[a.ID] = &clone
	return nil
}

func (r *storefrontAddressRepoStub) Update(_ context.Context, a *customer.Address) error {
	existing, ok := r.byID[a.ID]
	if !ok || existing.CustomerID != a.CustomerID {
		return apperror.NotFound("address not found")
	}
	if a.IsDefault {
		r.clearDefault(a.CustomerID)
	}
	clone := *a
	r.byID[a.ID] = &clone
	return nil
}

func (r *storefrontAddressRepoStub) SetDefault(_ context.Context, customerID, addressID string) error {
	target, ok := r.byID[addressID]
	if !ok || target.CustomerID != customerID {
		return apperror.NotFound("address not found")
	}
	r.clearDefault(customerID)
	target.IsDefault = true
	return nil
}

func (r *storefrontAddressRepoStub) Delete(_ context.Context, customerID, addressID string) error {
	a, ok := r.byID[addressID]
	if !ok || a.CustomerID != customerID {
		return apperror.NotFound("address not found")
	}
	delete(r.byID, addressID)
	return nil
}

// --- helpers ---

func newStorefrontProfileHandler(t *testing.T) (*shophttp.StorefrontHandler, http.Handler, *storefrontAddressRepoStub, *stubConsentRepo, string) {
	t.Helper()
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, _ := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	addresses := newStorefrontAddressRepoStub()
	consents := newStubConsentRepo()
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).
		WithAccountProfile(addresses, consents)
	return h, newStorefrontRouter(h), addresses, consents, out.CustomerID
}

func storefrontProfilePost(t *testing.T, router http.Handler, path, customerID string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	csrf := storefrontAccountCSRFCookie(t, router, "/account/addresses")
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf_token", csrf.Value)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrf)
	req = storefrontCustomerRequest(req, customerID)
	router.ServeHTTP(rec, req)
	return rec
}

func mustNewSavedAddress(t *testing.T, customerID, recipient, country string, isDefault bool) customer.Address {
	t.Helper()
	a, err := customer.NewAddress(id.New(), customerID, "Home", recipient, "1 Logic Lane", "Berlin", "10115", country)
	if err != nil {
		t.Fatalf("NewAddress: %v", err)
	}
	a.IsDefault = isDefault
	return a
}

// --- address tests ---

func TestStorefrontHandler_AccountAddresses_RequiresSession(t *testing.T) {
	_, router, _, _, _ := newStorefrontProfileHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/account/addresses", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/account/login") {
		t.Fatalf("location = %q, want login redirect", loc)
	}
}

func TestStorefrontHandler_AccountAddresses_ListEmpty(t *testing.T) {
	_, router, _, _, customerID := newStorefrontProfileHandler(t)

	rec := httptest.NewRecorder()
	req := storefrontCustomerRequest(httptest.NewRequest("GET", "/account/addresses", nil), customerID)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "You have not saved any addresses yet.") {
		t.Fatalf("expected empty message; body: %s", rec.Body.String())
	}
}

func TestStorefrontHandler_AccountAddressCreate_PersistsAsDefault(t *testing.T) {
	_, router, addresses, _, customerID := newStorefrontProfileHandler(t)

	form := url.Values{
		"label":     {"Home"},
		"recipient": {"Ada Lovelace"},
		"street":    {"1 Logic Lane"},
		"city":      {"Berlin"},
		"postcode":  {"10115"},
		"country":   {"DE"},
	}
	rec := storefrontProfilePost(t, router, "/account/addresses", customerID, form)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/account/addresses?saved=1" {
		t.Fatalf("location = %q, want /account/addresses?saved=1", loc)
	}
	saved, _ := addresses.ListByCustomer(context.Background(), customerID)
	if len(saved) != 1 {
		t.Fatalf("saved addresses = %d, want 1", len(saved))
	}
	if !saved[0].IsDefault {
		t.Fatal("first saved address should be the default")
	}
	if saved[0].Recipient != "Ada Lovelace" {
		t.Fatalf("recipient = %q, want Ada Lovelace", saved[0].Recipient)
	}
}

func TestStorefrontHandler_AccountAddressCreate_ValidationError(t *testing.T) {
	_, router, addresses, _, customerID := newStorefrontProfileHandler(t)

	form := url.Values{
		"recipient": {""},
		"street":    {"1 Logic Lane"},
		"city":      {"Berlin"},
		"postcode":  {"10115"},
		"country":   {"DE"},
	}
	rec := storefrontProfilePost(t, router, "/account/addresses", customerID, form)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "recipient is required") {
		t.Fatalf("expected validation error; body: %s", rec.Body.String())
	}
	if saved, _ := addresses.ListByCustomer(context.Background(), customerID); len(saved) != 0 {
		t.Fatalf("expected no saved address on validation error, got %d", len(saved))
	}
}

func TestStorefrontHandler_AccountAddressUpdate_ChangesFields(t *testing.T) {
	_, router, addresses, _, customerID := newStorefrontProfileHandler(t)
	addr := mustNewSavedAddress(t, customerID, "Ada Lovelace", "DE", true)
	if err := addresses.Create(context.Background(), &addr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	form := url.Values{
		"label":     {"Office"},
		"recipient": {"Ada L."},
		"street":    {"2 Babbage Blvd"},
		"city":      {"Munich"},
		"postcode":  {"80331"},
		"country":   {"DE"},
	}
	rec := storefrontProfilePost(t, router, "/account/addresses/"+addr.ID, customerID, form)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	got, _ := addresses.FindByID(context.Background(), addr.ID)
	if got == nil || got.City != "Munich" || got.Street != "2 Babbage Blvd" {
		t.Fatalf("address not updated: %+v", got)
	}
	if !got.IsDefault {
		t.Fatal("default flag should be preserved across update")
	}
}

func TestStorefrontHandler_AccountAddressUpdate_RejectsForeignAddress(t *testing.T) {
	_, router, addresses, _, customerID := newStorefrontProfileHandler(t)
	foreign := mustNewSavedAddress(t, "someone-else", "Other Person", "FR", true)
	if err := addresses.Create(context.Background(), &foreign); err != nil {
		t.Fatalf("Create: %v", err)
	}

	form := url.Values{
		"recipient": {"Hacker"},
		"street":    {"1 Evil St"},
		"city":      {"Nowhere"},
		"postcode":  {"00000"},
		"country":   {"FR"},
	}
	rec := storefrontProfilePost(t, router, "/account/addresses/"+foreign.ID, customerID, form)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	got, _ := addresses.FindByID(context.Background(), foreign.ID)
	if got == nil || got.Recipient != "Other Person" {
		t.Fatalf("foreign address must not change: %+v", got)
	}
}

func TestStorefrontHandler_AccountAddressSetDefault_SwitchesDefault(t *testing.T) {
	_, router, addresses, _, customerID := newStorefrontProfileHandler(t)
	first := mustNewSavedAddress(t, customerID, "Ada Lovelace", "DE", false)
	if err := addresses.Create(context.Background(), &first); err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second := mustNewSavedAddress(t, customerID, "Ada Office", "DE", false)
	if err := addresses.Create(context.Background(), &second); err != nil {
		t.Fatalf("Create second: %v", err)
	}

	rec := storefrontProfilePost(t, router, "/account/addresses/"+second.ID+"/default", customerID, nil)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	def, _ := addresses.FindDefault(context.Background(), customerID)
	if def == nil || def.ID != second.ID {
		t.Fatalf("default = %+v, want id %s", def, second.ID)
	}
}

func TestStorefrontHandler_AccountAddressDelete_RemovesAddress(t *testing.T) {
	_, router, addresses, _, customerID := newStorefrontProfileHandler(t)
	addr := mustNewSavedAddress(t, customerID, "Ada Lovelace", "DE", true)
	if err := addresses.Create(context.Background(), &addr); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rec := storefrontProfilePost(t, router, "/account/addresses/"+addr.ID+"/delete", customerID, nil)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if got, _ := addresses.FindByID(context.Background(), addr.ID); got != nil {
		t.Fatalf("address should be deleted, got %+v", got)
	}
}

// --- preferences tests ---

func TestStorefrontHandler_AccountPreferences_GetDefaultsOff(t *testing.T) {
	_, router, _, _, customerID := newStorefrontProfileHandler(t)

	rec := httptest.NewRecorder()
	req := storefrontCustomerRequest(httptest.NewRequest("GET", "/account/preferences", nil), customerID)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `name="marketing" value="1" checked`) {
		t.Fatalf("marketing should default to unchecked; body: %s", rec.Body.String())
	}
}

func TestStorefrontHandler_AccountPreferences_SavesMarketingConsent(t *testing.T) {
	_, router, _, consents, customerID := newStorefrontProfileHandler(t)

	rec := storefrontProfilePost(t, router, "/account/preferences", customerID, url.Values{"marketing": {"1"}})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/account/preferences?updated=1" {
		t.Fatalf("location = %q, want /account/preferences?updated=1", loc)
	}
	got, _ := consents.FindByCustomerID(context.Background(), customerID)
	if got == nil || !got.Marketing {
		t.Fatalf("marketing consent not persisted: %+v", got)
	}
	if !got.Necessary {
		t.Fatal("necessary consent should remain true")
	}
}

// --- checkout prefill ---

func TestStorefrontHandler_CheckoutAddress_PrefillsDefaultAddress(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	cartSvc, carts, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	variants := &mockStorefrontVariantRepo{findByIDFn: func(_ context.Context, vid string) (*catalog.Variant, error) {
		return &catalog.Variant{ID: vid, ProductID: "prod-1", SKU: "SKU-1", Name: "Widget"}, nil
	}}
	checkoutSvc, shippingProvider, paymentProvider, _ := newStorefrontCheckoutService(carts, prices, variants)
	authSvc, _ := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	addresses := newStorefrontAddressRepoStub()
	addr := mustNewSavedAddress(t, out.CustomerID, "Ada Lovelace", "DE", true)
	if err := addresses.Create(context.Background(), &addr); err != nil {
		t.Fatalf("Create: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithCart(variants, cartSvc).
		WithCheckout([]shipping.Provider{shippingProvider}, paymentProvider, checkoutSvc).
		WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).
		WithAccountProfile(addresses, newStubConsentRepo())
	router := newStorefrontRouter(h)

	currentCart, err := cartSvc.CreateCart(context.Background(), out.CustomerID, "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), currentCart.ID, out.CustomerID, "var-1", 1); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	rec := httptest.NewRecorder()
	req := storefrontCustomerRequest(httptest.NewRequest("GET", "/checkout/address", nil), out.CustomerID)
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: currentCart.ID})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"1 Logic Lane", "Berlin", "10115", "ada@example.com"} {
		if !strings.Contains(body, want) {
			t.Fatalf("prefilled checkout missing %q; body: %s", want, body)
		}
	}
	if !strings.Contains(body, `value="DE" selected`) {
		t.Fatalf("expected default country selected; body: %s", body)
	}
}
