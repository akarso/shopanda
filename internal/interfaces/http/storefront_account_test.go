package http_test

import (
	"context"
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

type storefrontAccountCustomerRepoStub struct {
	customers map[string]*customer.Customer
	byEmail   map[string]*customer.Customer
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
	repo := newStorefrontAccountCustomerRepoStub()
	issuer, err := jwt.NewIssuer("test-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	log := logger.NewWithWriter(io.Discard, "error")
	return appAuth.NewService(repo, &storefrontAccountResetRepoStub{}, issuer, event.NewBus(log), log, time.Hour), repo
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
	h := shophttp.NewStorefrontHandler(engine, &mockStorefrontRepo{}, newStorefrontCategoryMock(), pdp, plp, newStorefrontSearchMock()).WithAccount(authSvc, newStorefrontAccountOrderRepoStub(), deleter)
	router := newStorefrontRouter(h)
	id, err := identity.NewIdentity(out.CustomerID, identity.RoleCustomer)
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	csrfCookie := storefrontAccountCSRFCookie(t, router, "/account/profile")

	form := url.Values{
		"csrf_token": {csrfCookie.Value},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/account/profile/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrfCookie)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if deleter.deleted != "" {
		t.Fatalf("deleted = %q, want empty", deleter.deleted)
	}
}
