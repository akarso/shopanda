package storefront_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/identity"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	platformauth "github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/jwt/jwttest"
)

type memCustomers struct {
	byID map[string]*customer.Customer
}

func (m *memCustomers) FindByID(_ context.Context, id string) (*customer.Customer, error) {
	return m.byID[id], nil
}
func (m *memCustomers) FindByEmail(context.Context, string) (*customer.Customer, error) {
	return nil, nil
}
func (m *memCustomers) Create(_ context.Context, c *customer.Customer) error {
	m.byID[c.ID] = c
	return nil
}
func (m *memCustomers) Update(_ context.Context, c *customer.Customer) error {
	m.byID[c.ID] = c
	return nil
}
func (m *memCustomers) ListCustomers(context.Context, int, int) ([]customer.Customer, error) {
	return nil, nil
}
func (m *memCustomers) BumpTokenGeneration(context.Context, string) error { return nil }
func (m *memCustomers) ChangePasswordAndBumpTokenGeneration(context.Context, string, string) error {
	return nil
}
func (m *memCustomers) Delete(context.Context, string) error { return nil }

func TestAuthMiddleware_RealParser_ExpiredBearerIgnoresSession(t *testing.T) {
	issuer, err := jwt.NewIssuer(jwttest.TestSecret, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c, err := customer.NewCustomer("user-1", "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	repo := &memCustomers{byID: map[string]*customer.Customer{c.ID: &c}}
	parser := auth.NewValidatingTokenParser(issuer, repo, 0)

	expired, _, err := issuer.Create("user-1", "customer", 0)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)

	longIssuer, _ := jwt.NewIssuer(jwttest.TestSecret, time.Hour)
	session, _, err := longIssuer.Create("user-1", "customer", 0)
	if err != nil {
		t.Fatal(err)
	}

	mw := storefront.AuthMiddleware(parser)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/account", nil)
	req.Header.Set("Authorization", "Bearer "+expired)
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_session", Value: session})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (expired customer JWT must not fall back to session)", rec.Code)
	}
}

func TestAuthMiddleware_RealParser_TwoDotBearerIs401(t *testing.T) {
	issuer, _ := jwt.NewIssuer(jwttest.TestSecret, time.Hour)
	c, _ := customer.NewCustomer("user-1", "a@example.com")
	repo := &memCustomers{byID: map[string]*customer.Customer{c.ID: &c}}
	parser := auth.NewValidatingTokenParser(issuer, repo, 0)

	longIssuer, _ := jwt.NewIssuer(jwttest.TestSecret, time.Hour)
	session, _, _ := longIssuer.Create("user-1", "customer", 0)

	mw := storefront.AuthMiddleware(parser)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/api/customizer/x", nil)
	req.Header.Set("Authorization", "Bearer a.b.c")
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_session", Value: session})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for JWT-shaped owner token until MD token shape is confirmed opaque", rec.Code)
	}
}

func TestAuthMiddleware_RealParser_OpaqueBearerUsesSession(t *testing.T) {
	issuer, _ := jwt.NewIssuer(jwttest.TestSecret, time.Hour)
	c, _ := customer.NewCustomer("user-1", "a@example.com")
	repo := &memCustomers{byID: map[string]*customer.Customer{c.ID: &c}}
	parser := auth.NewValidatingTokenParser(issuer, repo, 0)
	session, _, _ := issuer.Create("user-1", "customer", 0)

	mw := storefront.AuthMiddleware(parser)
	var got identity.Identity
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = platformauth.IdentityFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/api/customizer/x", nil)
	req.Header.Set("Authorization", "Bearer owner-opaque-token")
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_session", Value: session})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got.UserID != "user-1" {
		t.Fatalf("UserID = %q, want user-1", got.UserID)
	}
}

func TestStorefrontCustomerID_AdminActor(t *testing.T) {
	admin, err := identity.NewIdentity("admin-1", identity.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/account", nil)
	req = req.WithContext(platformauth.WithIdentity(req.Context(), admin))
	// Exercise the same helper storefront cart uses.
	if got := platformauth.StorefrontActorID(platformauth.IdentityFrom(req.Context())); got != "admin-1" {
		t.Fatalf("StorefrontActorID = %q, want admin-1", got)
	}
}
