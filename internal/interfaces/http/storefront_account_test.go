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

	appAuth "github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/logger"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

func TestStorefrontHandler_Home_ShowsAccountLoginEntry_WhenAnonymous(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock())
	router := newStorefrontRouter(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/account/login") {
		t.Fatalf("expected anonymous account login entry in body: %s", body)
	}
	if strings.Contains(body, "/account/logout") {
		t.Fatalf("did not expect logout control in anonymous body: %s", body)
	}
}

func TestStorefrontHandler_Home_ShowsAccountControls_WhenAuthenticated(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, _ := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{})
	router := newStorefrontRouter(h)

	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	id = id.WithDisplayName("Ada Lovelace")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Ada Lovelace") {
		t.Fatalf("expected signed-in account name in body: %s", body)
	}
	if !strings.Contains(body, "/account/profile") {
		t.Fatalf("expected profile link in body: %s", body)
	}
	if !strings.Contains(body, "/account/orders") {
		t.Fatalf("expected orders link in body: %s", body)
	}
	if !strings.Contains(body, "/account/security") {
		t.Fatalf("expected security link in body: %s", body)
	}
	if !strings.Contains(body, "/account/logout") {
		t.Fatalf("expected logout control in body: %s", body)
	}
}

type storefrontAccountCustomerRepoStub struct {
	customers map[string]*customer.Customer
	byEmail   map[string]*customer.Customer
	bumpErr   error
}

func newStorefrontAccountCustomerRepoStub() *storefrontAccountCustomerRepoStub {
	return &storefrontAccountCustomerRepoStub{customers: map[string]*customer.Customer{}, byEmail: map[string]*customer.Customer{}}
}

func (r *storefrontAccountCustomerRepoStub) FindByID(_ context.Context, id string) (*customer.Customer, error) {
	return r.customers[id], nil
}

func (r *storefrontAccountCustomerRepoStub) FindByEmail(_ context.Context, email string) (*customer.Customer, error) {
	return r.byEmail[email], nil
}

func (r *storefrontAccountCustomerRepoStub) Create(_ context.Context, c *customer.Customer) error {
	r.customers[c.ID] = c
	r.byEmail[c.Email] = c
	return nil
}

func (r *storefrontAccountCustomerRepoStub) Update(_ context.Context, c *customer.Customer) error {
	for email, existing := range r.byEmail {
		if existing.ID == c.ID && email != c.Email {
			delete(r.byEmail, email)
		}
	}
	r.customers[c.ID] = c
	r.byEmail[c.Email] = c
	return nil
}

func (r *storefrontAccountCustomerRepoStub) ListCustomers(_ context.Context, _, _ int) ([]customer.Customer, error) {
	return nil, nil
}

func (r *storefrontAccountCustomerRepoStub) BumpTokenGeneration(_ context.Context, customerID string) error {
	if r.bumpErr != nil {
		return r.bumpErr
	}
	if c := r.customers[customerID]; c != nil {
		c.BumpTokenGeneration()
	}
	return nil
}

func (r *storefrontAccountCustomerRepoStub) ChangePasswordAndBumpTokenGeneration(_ context.Context, customerID, passwordHash string) error {
	if c := r.customers[customerID]; c != nil {
		c.PasswordHash = passwordHash
		c.BumpTokenGeneration()
	}
	return nil
}

func (r *storefrontAccountCustomerRepoStub) Delete(_ context.Context, id string) error {
	if c := r.customers[id]; c != nil {
		delete(r.byEmail, c.Email)
	}
	delete(r.customers, id)
	return nil
}

type storefrontAccountResetRepoStub struct{}

func (r *storefrontAccountResetRepoStub) Create(_ context.Context, _ *customer.PasswordResetToken) error {
	return nil
}
func (r *storefrontAccountResetRepoStub) FindByTokenHash(_ context.Context, _ string) (*customer.PasswordResetToken, error) {
	return nil, nil
}
func (r *storefrontAccountResetRepoStub) MarkUsed(_ context.Context, _ string) error { return nil }

type storefrontAccountOrderRepoStub struct {
	byID       map[string]*order.Order
	byCustomer map[string][]order.Order
}

func newStorefrontAccountOrderRepoStub() *storefrontAccountOrderRepoStub {
	return &storefrontAccountOrderRepoStub{byID: map[string]*order.Order{}, byCustomer: map[string][]order.Order{}}
}

func (r *storefrontAccountOrderRepoStub) FindByID(_ context.Context, id string) (*order.Order, error) {
	return r.byID[id], nil
}

func (r *storefrontAccountOrderRepoStub) FindByCustomerID(_ context.Context, customerID string) ([]order.Order, error) {
	return r.byCustomer[customerID], nil
}

func (r *storefrontAccountOrderRepoStub) List(_ context.Context, _, _ int) ([]order.Order, error) {
	return nil, nil
}
func (r *storefrontAccountOrderRepoStub) Save(_ context.Context, _ *order.Order) error { return nil }
func (r *storefrontAccountOrderRepoStub) UpdateStatus(_ context.Context, _ *order.Order) error {
	return nil
}

type storefrontAccountDeleterStub struct{ deleted string }

func (d *storefrontAccountDeleterStub) DeleteAccount(_ context.Context, customerID string) error {
	d.deleted = customerID
	return nil
}

func newStorefrontAuthService(t *testing.T) (*appAuth.Service, *storefrontAccountCustomerRepoStub) {
	t.Helper()
	return newStorefrontAuthServiceWithBus(t, nil)
}

func newStorefrontAuthServiceWithBus(t *testing.T, bus *event.Bus) (*appAuth.Service, *storefrontAccountCustomerRepoStub) {
	t.Helper()
	repo := newStorefrontAccountCustomerRepoStub()
	issuer, err := jwt.NewIssuer("test-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	log := logger.NewWithWriter(io.Discard, "error")
	if bus == nil {
		bus = event.NewBus(log)
	}
	return appAuth.NewService(repo, &storefrontAccountResetRepoStub{}, issuer, bus, log, time.Hour), repo
}

func storefrontAccountCSRFCookie(t *testing.T, handler http.Handler, path string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", path, nil)
	handler.ServeHTTP(rec, req)
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "shopanda_csrf" {
			return cookie
		}
	}
	t.Fatalf("missing storefront CSRF cookie for %s", path)
	return nil
}

func storefrontAccountSecurityVerifiedCookie(t *testing.T, handler http.Handler, id identity.Identity, redirectTo string) (*http.Cookie, *http.Cookie) {
	t.Helper()
	csrfCookie := storefrontAccountCSRFCookie(t, handler, "/account/security/verify")
	form := url.Values{
		"csrf_token":  {csrfCookie.Value},
		"redirect_to": {redirectTo},
		"password":    {"password123"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/account/security/verify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("verify status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if rec.Header().Get("Location") != redirectTo {
		t.Fatalf("verify location = %q, want %q", rec.Header().Get("Location"), redirectTo)
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "shopanda_storefront_security_verify" {
			return csrfCookie, cookie
		}
	}
	t.Fatal("expected storefront security verification cookie")
	return nil, nil
}

func TestStorefrontHandler_AccountLogin_SetsSessionCookie(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, _ := newStorefrontAuthService(t)
	_, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{})
	router := newStorefrontRouter(h)
	csrfCookie := storefrontAccountCSRFCookie(t, router, "/account/login")

	form := url.Values{
		"csrf_token":  {csrfCookie.Value},
		"redirect_to": {"/account/orders"},
		"email":       {"ada@example.com"},
		"password":    {"password123"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/account/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if rec.Header().Get("Location") != "/account/orders" {
		t.Fatalf("location = %q, want %q", rec.Header().Get("Location"), "/account/orders")
	}
	var sessionCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "shopanda_storefront_session" {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("expected storefront session cookie to be set")
	}
}

func TestStorefrontHandler_AccountLogin_ClaimsGuestCart(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, _ := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	guestCart, err := cartSvc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), guestCart.ID, "", "var-1", 2); err != nil {
		t.Fatalf("AddItem guest: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithCart(nil, cartSvc).
		WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{})
	router := newStorefrontRouter(h)
	csrfCookie := storefrontAccountCSRFCookie(t, router, "/account/login")

	form := url.Values{
		"csrf_token":  {csrfCookie.Value},
		"redirect_to": {"/account/orders"},
		"email":       {"ada@example.com"},
		"password":    {"password123"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/account/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: guestCart.ID})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	activeCart, err := cartSvc.GetActiveCartByCustomer(context.Background(), out.CustomerID)
	if err != nil {
		t.Fatalf("GetActiveCartByCustomer: %v", err)
	}
	if activeCart.ID != guestCart.ID {
		t.Fatalf("active cart id = %q, want %q", activeCart.ID, guestCart.ID)
	}
	if activeCart.TotalQuantity() != 2 {
		t.Fatalf("total quantity = %d, want 2", activeCart.TotalQuantity())
	}
	cleared := false
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "shopanda_storefront_cart" && cookie.MaxAge < 0 && cookie.Expires.Unix() == 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("expected storefront cart cookie to be cleared after login cart claim")
	}
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	cartRec := httptest.NewRecorder()
	cartReq := httptest.NewRequest("GET", "/cart", nil)
	cartReq = cartReq.WithContext(auth.WithIdentity(cartReq.Context(), id))
	router.ServeHTTP(cartRec, cartReq)
	if cartRec.Code != http.StatusOK {
		t.Fatalf("cart status = %d, want %d; body: %s", cartRec.Code, http.StatusOK, cartRec.Body.String())
	}
	if strings.Contains(cartRec.Body.String(), "Your cart is empty.") {
		t.Fatalf("expected claimed cart to render items, got body: %s", cartRec.Body.String())
	}
}

func TestStorefrontHandler_AccountRegister_ClaimsGuestCart(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, repo := newStorefrontAuthService(t)
	cartSvc, _, prices := newStorefrontCartService()
	prices.set("var-1", "EUR", 1500)
	guestCart, err := cartSvc.CreateCart(context.Background(), "", "EUR")
	if err != nil {
		t.Fatalf("CreateCart guest: %v", err)
	}
	if _, err := cartSvc.AddItem(context.Background(), guestCart.ID, "", "var-1", 1); err != nil {
		t.Fatalf("AddItem guest: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).
		WithCart(nil, cartSvc).
		WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{})
	router := newStorefrontRouter(h)
	csrfCookie := storefrontAccountCSRFCookie(t, router, "/account/register")

	form := url.Values{
		"csrf_token":  {csrfCookie.Value},
		"redirect_to": {"/account/orders"},
		"first_name":  {"Ada"},
		"last_name":   {"Lovelace"},
		"email":       {"ada@example.com"},
		"password":    {"password123"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/account/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_cart", Value: guestCart.ID})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	createdCustomer := repo.byEmail["ada@example.com"]
	if createdCustomer == nil {
		t.Fatal("expected registered customer to be persisted")
	}
	activeCart, err := cartSvc.GetActiveCartByCustomer(context.Background(), createdCustomer.ID)
	if err != nil {
		t.Fatalf("GetActiveCartByCustomer: %v", err)
	}
	if activeCart.ID != guestCart.ID {
		t.Fatalf("active cart id = %q, want %q", activeCart.ID, guestCart.ID)
	}
	if activeCart.TotalQuantity() != 1 {
		t.Fatalf("total quantity = %d, want 1", activeCart.TotalQuantity())
	}
	cleared := false
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "shopanda_storefront_cart" && cookie.MaxAge < 0 && cookie.Expires.Unix() == 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("expected storefront cart cookie to be cleared after register cart claim")
	}
}

func TestStorefrontHandler_AccountOrders_RendersCustomerOrders(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, repo := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	_ = repo
	orders := newStorefrontAccountOrderRepoStub()
	item, err := order.NewItem("var-1", "SKU-1", "Widget", 2, shared.MustNewMoney(1500, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder("ord-1", out.CustomerID, "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	orders.byID[o.ID] = &o
	orders.byCustomer[out.CustomerID] = []order.Order{o}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, orders, &storefrontAccountDeleterStub{})

	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/account/orders", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	newStorefrontRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "ord-1") || !strings.Contains(body, "EUR 30.00") {
		t.Fatalf("body missing order data: %s", body)
	}
}

func TestStorefrontHandler_AccountProfile_Update(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, repo := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{})
	router := newStorefrontRouter(h)
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	csrfCookie := storefrontAccountCSRFCookie(t, router, "/account/profile")

	form := url.Values{
		"csrf_token": {csrfCookie.Value},
		"first_name": {"Grace"},
		"last_name":  {"Hopper"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/account/profile", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if rec.Header().Get("Location") != "/account/profile?updated=1" {
		t.Fatalf("location = %q, want %q", rec.Header().Get("Location"), "/account/profile?updated=1")
	}
	if repo.customers[out.CustomerID].FirstName != "Grace" || repo.customers[out.CustomerID].LastName != "Hopper" {
		t.Fatalf("profile = %q %q, want Grace Hopper", repo.customers[out.CustomerID].FirstName, repo.customers[out.CustomerID].LastName)
	}
}

func TestStorefrontHandler_AccountProfile_DoesNotRenderSecurityForms(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, _ := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{})
	router := newStorefrontRouter(h)
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/account/profile", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/account/security") {
		t.Fatalf("expected security navigation link in body: %s", body)
	}
	if strings.Contains(body, "/account/security/password") || strings.Contains(body, "/account/security/delete") {
		t.Fatalf("expected profile page to omit security forms, got body: %s", body)
	}
}

func TestStorefrontHandler_AccountProfile_RemainsAvailableWhenStepUpEnabled(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, _ := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute)
	router := newStorefrontRouter(h)
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/account/profile", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != "" {
		t.Fatalf("location = %q, want empty", location)
	}
}

func TestStorefrontHandler_AccountSecurity_RedirectsToVerification_WhenStepUpRequired(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, _ := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute)
	router := newStorefrontRouter(h)
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/account/security", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if rec.Header().Get("Location") != "/account/security/verify?redirect_to=%2Faccount%2Fsecurity" {
		t.Fatalf("location = %q, want %q", rec.Header().Get("Location"), "/account/security/verify?redirect_to=%2Faccount%2Fsecurity")
	}
}

func TestStorefrontHandler_AccountSecurity_AllowsFreshSessionWithoutVerificationCookie(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, _ := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute)
	router := newStorefrontRouter(h)
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	id = id.WithAuthenticatedAt(time.Now().UTC())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/account/security", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestStorefrontHandler_AccountSecurity_RedirectsWhenSessionIsStale(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, _ := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute)
	router := newStorefrontRouter(h)
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	id = id.WithAuthenticatedAt(time.Now().UTC().Add(-10 * time.Minute))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/account/security", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if rec.Header().Get("Location") != "/account/security/verify?redirect_to=%2Faccount%2Fsecurity" {
		t.Fatalf("location = %q, want %q", rec.Header().Get("Location"), "/account/security/verify?redirect_to=%2Faccount%2Fsecurity")
	}
}

func TestStorefrontHandler_AccountSecurityVerify_SetsVerificationCookie(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, _ := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute)
	router := newStorefrontRouter(h)
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}

	_, verifiedCookie := storefrontAccountSecurityVerifiedCookie(t, router, id, "/account/security")
	if verifiedCookie.Value == "" || verifiedCookie.MaxAge <= 0 {
		t.Fatalf("expected verification cookie to be set, got %+v", verifiedCookie)
	}
}

func TestStorefrontHandler_AccountSecurityVerify_EmailLinkRequest_RedirectsAndPublishesURL(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	log := logger.NewWithWriter(io.Discard, "error")
	bus := event.NewBus(log)
	var verifyURL string
	bus.On(customer.EventSecurityVerificationRequested, func(_ context.Context, evt event.Event) error {
		data := evt.Data.(customer.SecurityVerificationRequestedData)
		verifyURL = data.VerifyURL
		return nil
	})
	authSvc, _ := newStorefrontAuthServiceWithBus(t, bus)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute).WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute)
	router := newStorefrontRouter(h)
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	csrfCookie := storefrontAccountCSRFCookie(t, router, "/account/security/verify")

	form := url.Values{
		"csrf_token":  {csrfCookie.Value},
		"redirect_to": {"/account/security"},
		"action":      {"email_link"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/account/security/verify", strings.NewReader(form.Encode()))
	req.Host = "evil.example"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if rec.Header().Get("Location") != "/account/security/verify?email_sent=1&redirect_to=%2Faccount%2Fsecurity" {
		t.Fatalf("location = %q", rec.Header().Get("Location"))
	}
	if verifyURL == "" {
		t.Fatal("expected verification URL to be published")
	}
	parsed, err := url.Parse(verifyURL)
	if err != nil {
		t.Fatalf("Parse verifyURL: %v", err)
	}
	if parsed.Path != "/account/security/verify" {
		t.Fatalf("verify path = %q, want %q", parsed.Path, "/account/security/verify")
	}
	if parsed.Scheme != "https" || parsed.Host != "shop.test" {
		t.Fatalf("verify URL host = %s://%s, want https://shop.test", parsed.Scheme, parsed.Host)
	}
	if strings.TrimSpace(parsed.Query().Get("email_token")) == "" {
		t.Fatal("expected email_token in verification URL")
	}
}

func TestStorefrontHandler_AccountSecurityVerify_EmailLink_SetsVerificationCookie(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	log := logger.NewWithWriter(io.Discard, "error")
	bus := event.NewBus(log)
	var verifyURL string
	bus.On(customer.EventSecurityVerificationRequested, func(_ context.Context, evt event.Event) error {
		data := evt.Data.(customer.SecurityVerificationRequestedData)
		verifyURL = data.VerifyURL
		return nil
	})
	authSvc, _ := newStorefrontAuthServiceWithBus(t, bus)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute).WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute)
	router := newStorefrontRouter(h)
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	csrfCookie := storefrontAccountCSRFCookie(t, router, "/account/security/verify")
	requestForm := url.Values{
		"csrf_token":  {csrfCookie.Value},
		"redirect_to": {"/account/security"},
		"action":      {"email_link"},
	}
	requestRec := httptest.NewRecorder()
	requestReq := httptest.NewRequest("POST", "/account/security/verify", strings.NewReader(requestForm.Encode()))
	requestReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	requestReq.AddCookie(csrfCookie)
	requestReq = requestReq.WithContext(auth.WithIdentity(requestReq.Context(), id))
	router.ServeHTTP(requestRec, requestReq)

	if verifyURL == "" {
		t.Fatal("expected verification URL to be published")
	}
	parsed, err := url.Parse(verifyURL)
	if err != nil {
		t.Fatalf("Parse verifyURL: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", parsed.RequestURI(), nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if rec.Header().Get("Location") != "/account/security" {
		t.Fatalf("location = %q, want %q", rec.Header().Get("Location"), "/account/security")
	}
	var verifiedCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "shopanda_storefront_security_verify" {
			verifiedCookie = cookie
			break
		}
	}
	if verifiedCookie == nil || verifiedCookie.Value == "" {
		t.Fatal("expected storefront security verification cookie from email link")
	}
}

func TestStorefrontHandler_AccountSecurityVerify_EmailLinkRequest_ThrottlesRepeatedSends(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	log := logger.NewWithWriter(io.Discard, "error")
	bus := event.NewBus(log)
	publishCount := 0
	bus.On(customer.EventSecurityVerificationRequested, func(_ context.Context, evt event.Event) error {
		publishCount++
		return nil
	})
	authSvc, _ := newStorefrontAuthServiceWithBus(t, bus)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute).WithAccountSecurityEmailLinks("https://shop.test", 45*time.Minute)
	router := newStorefrontRouter(h)
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	csrfCookie := storefrontAccountCSRFCookie(t, router, "/account/security/verify")
	form := url.Values{
		"csrf_token":  {csrfCookie.Value},
		"redirect_to": {"/account/security"},
		"action":      {"email_link"},
	}

	firstRec := httptest.NewRecorder()
	firstReq := httptest.NewRequest("POST", "/account/security/verify", strings.NewReader(form.Encode()))
	firstReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	firstReq.AddCookie(csrfCookie)
	firstReq = firstReq.WithContext(auth.WithIdentity(firstReq.Context(), id))
	router.ServeHTTP(firstRec, firstReq)

	if firstRec.Code != http.StatusSeeOther {
		t.Fatalf("first status = %d, want %d; body: %s", firstRec.Code, http.StatusSeeOther, firstRec.Body.String())
	}

	secondRec := httptest.NewRecorder()
	secondReq := httptest.NewRequest("POST", "/account/security/verify", strings.NewReader(form.Encode()))
	secondReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	secondReq.AddCookie(csrfCookie)
	secondReq = secondReq.WithContext(auth.WithIdentity(secondReq.Context(), id))
	router.ServeHTTP(secondRec, secondReq)

	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d; body: %s", secondRec.Code, http.StatusTooManyRequests, secondRec.Body.String())
	}
	body := secondRec.Body.String()
	if !strings.Contains(body, "Please wait before requesting another verification email.") {
		t.Fatalf("expected throttle message in body: %s", body)
	}
	if publishCount != 1 {
		t.Fatalf("publishCount = %d, want 1", publishCount)
	}
}

func TestStorefrontHandler_AccountSecurity_RendersSensitiveActions(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, _ := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute)
	router := newStorefrontRouter(h)
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	csrfCookie, verifiedCookie := storefrontAccountSecurityVerifiedCookie(t, router, id, "/account/security")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/account/security", nil)
	req.AddCookie(csrfCookie)
	req.AddCookie(verifiedCookie)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/account/security/password") {
		t.Fatalf("expected password form action in body: %s", body)
	}
	if !strings.Contains(body, "/account/security/delete") {
		t.Fatalf("expected delete form action in body: %s", body)
	}
	if !strings.Contains(body, "/account/logout") {
		t.Fatalf("expected logout form action in body: %s", body)
	}
}

func TestStorefrontHandler_AccountPassword_RedirectsToVerification_WhenUnverified(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, _ := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{}).WithAccountSecurity("test-secret", time.Minute)
	router := newStorefrontRouter(h)
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	csrfCookie := storefrontAccountCSRFCookie(t, router, "/account/security/verify")

	form := url.Values{
		"csrf_token":       {csrfCookie.Value},
		"current_password": {"password123"},
		"new_password":     {"newpassword123"},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/account/security/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if rec.Header().Get("Location") != "/account/security/verify?redirect_to=%2Faccount%2Fsecurity" {
		t.Fatalf("location = %q, want %q", rec.Header().Get("Location"), "/account/security/verify?redirect_to=%2Faccount%2Fsecurity")
	}
}

func TestStorefrontHandler_AccountDelete_RequiresConfirmation(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, _ := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	deleter := &storefrontAccountDeleterStub{}
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), deleter).WithAccountSecurity("test-secret", time.Minute)
	router := newStorefrontRouter(h)
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	csrfCookie, verifiedCookie := storefrontAccountSecurityVerifiedCookie(t, router, id, "/account/security")

	form := url.Values{
		"csrf_token": {csrfCookie.Value},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/account/security/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	req.AddCookie(verifiedCookie)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if deleter.deleted != "" {
		t.Fatalf("deleted = %q, want empty", deleter.deleted)
	}
}

func TestStorefrontHandler_AccountLogout_LogoutFailureDoesNotClearCookie(t *testing.T) {
	engine := createTestTheme(t)
	pdp := composition.NewPipeline[composition.ProductContext]()
	plp := composition.NewPipeline[composition.ListingContext]()
	authSvc, repo := newStorefrontAuthService(t)
	out, err := authSvc.Register(context.Background(), appAuth.RegisterInput{Email: "ada@example.com", Password: "password123", FirstName: "Ada", LastName: "Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	repo.bumpErr = errors.New("invalidate failed")
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), &storefrontAccountDeleterStub{})
	router := newStorefrontRouter(h)
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	csrfCookie := storefrontAccountCSRFCookie(t, router, "/account/security")

	form := url.Values{"csrf_token": {csrfCookie.Value}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/account/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != "" {
		t.Fatalf("location = %q, want empty", location)
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "shopanda_storefront_session" && cookie.MaxAge < 0 {
			t.Fatalf("unexpected cleared storefront session cookie on logout failure")
		}
	}
}
