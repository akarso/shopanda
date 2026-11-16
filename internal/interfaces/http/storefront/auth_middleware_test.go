package storefront_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// stubTokenParser implements auth.TokenParser for testing.
type stubTokenParser struct {
	identity  identity.Identity
	err       error
	lastToken string
}

func (s *stubTokenParser) Parse(_ context.Context, token string) (identity.Identity, error) {
	s.lastToken = token
	return s.identity, s.err
}

func TestAuthMiddleware_NoHeader(t *testing.T) {
	parser := &stubTokenParser{}
	mw := storefront.AuthMiddleware(parser)

	var gotID identity.Identity
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = auth.IdentityFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !gotID.IsGuest() {
		t.Errorf("expected guest identity, got %+v", gotID)
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	id, err := identity.NewIdentity("user-1", identity.RoleCustomer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parser := &stubTokenParser{identity: id}
	mw := storefront.AuthMiddleware(parser)

	var gotID identity.Identity
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = auth.IdentityFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotID.UserID != "user-1" {
		t.Errorf("UserID = %q, want %q", gotID.UserID, "user-1")
	}
	if parser.lastToken != "valid-token" {
		t.Errorf("parser received token %q, want %q", parser.lastToken, "valid-token")
	}
}

func TestAuthMiddleware_BearerCaseAndPadding(t *testing.T) {
	id, err := identity.NewIdentity("user-pad", identity.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}
	parser := &stubTokenParser{identity: id}
	mw := storefront.AuthMiddleware(parser)

	var gotID identity.Identity
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = auth.IdentityFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "bearer  padded-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotID.UserID != "user-pad" {
		t.Fatalf("UserID = %q, want user-pad", gotID.UserID)
	}
	if parser.lastToken != "padded-token" {
		t.Fatalf("parser received %q, want padded-token", parser.lastToken)
	}
}

func TestAuthMiddleware_UsesStorefrontSessionCookie(t *testing.T) {
	id, err := identity.NewIdentity("user-2", identity.RoleCustomer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parser := &stubTokenParser{identity: id}
	mw := storefront.AuthMiddleware(parser)

	var gotID identity.Identity
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = auth.IdentityFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/account/orders", nil)
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_session", Value: "cookie-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotID.UserID != "user-2" {
		t.Fatalf("UserID = %q, want %q", gotID.UserID, "user-2")
	}
	if parser.lastToken != "cookie-token" {
		t.Fatalf("parser received token %q, want %q", parser.lastToken, "cookie-token")
	}
}

func TestAuthMiddleware_BearerTakesPrecedence(t *testing.T) {
	id, err := identity.NewIdentity("user-3", identity.RoleCustomer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parser := &stubTokenParser{identity: id}
	mw := storefront.AuthMiddleware(parser)

	var gotID identity.Identity
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = auth.IdentityFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/account/orders", nil)
	req.Header.Set("Authorization", "Bearer header-token")
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_session", Value: "cookie-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotID.UserID != "user-3" {
		t.Fatalf("UserID = %q, want %q", gotID.UserID, "user-3")
	}
	if parser.lastToken != "header-token" {
		t.Fatalf("parser received token %q, want %q", parser.lastToken, "header-token")
	}
}

func TestAuthMiddleware_InvalidCustomerTokenIs401EvenWithSession(t *testing.T) {
	parser := &stubTokenParser{err: errors.New("jwt: token expired")}
	mw := storefront.AuthMiddleware(parser)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer expired-customer-jwt")
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_session", Value: "cookie-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if parser.lastToken != "expired-customer-jwt" {
		t.Errorf("parser received token %q, want expired-customer-jwt", parser.lastToken)
	}
}

func TestAuthMiddleware_StaffBearerDefersToCustomerSession_OnStorefront(t *testing.T) {
	admin, err := identity.NewIdentity("admin-1", identity.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	shopper, err := identity.NewIdentity("cust-1", identity.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}
	p := &mapParser{byToken: map[string]parseResult{
		"admin-jwt":    {id: admin},
		"cookie-token": {id: shopper},
	}}
	mw := storefront.AuthMiddleware(p)

	for _, path := range []string{"/cart", "/api/customizer/save"} {
		var gotID identity.Identity
		handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotID = auth.IdentityFrom(r.Context())
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer admin-jwt")
		req.AddCookie(&http.Cookie{Name: "shopanda_storefront_session", Value: "cookie-token"})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", path, rec.Code)
		}
		if gotID.UserID != "cust-1" {
			t.Fatalf("%s: UserID = %q, want cust-1 (session over staff bearer)", path, gotID.UserID)
		}
	}
}

func TestAuthMiddleware_StaffBearerWinsOnAdminAndAuthAPIs(t *testing.T) {
	admin, err := identity.NewIdentity("admin-1", identity.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	shopper, err := identity.NewIdentity("cust-1", identity.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}
	p := &mapParser{byToken: map[string]parseResult{
		"admin-jwt":    {id: admin},
		"cookie-token": {id: shopper},
	}}
	mw := storefront.AuthMiddleware(p)

	for _, path := range []string{"/api/v1/admin/products", "/api/v1/auth/me"} {
		var gotID identity.Identity
		handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotID = auth.IdentityFrom(r.Context())
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer admin-jwt")
		req.AddCookie(&http.Cookie{Name: "shopanda_storefront_session", Value: "cookie-token"})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", path, rec.Code)
		}
		if gotID.UserID != "admin-1" {
			t.Fatalf("%s: UserID = %q, want admin-1 (bearer wins over session)", path, gotID.UserID)
		}
	}
}

func TestAuthMiddleware_StaffBearerAloneUsesStaff(t *testing.T) {
	admin, err := identity.NewIdentity("admin-1", identity.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	p := &mapParser{byToken: map[string]parseResult{
		"admin-jwt": {id: admin},
	}}
	mw := storefront.AuthMiddleware(p)

	var gotID identity.Identity
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = auth.IdentityFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/account", nil)
	req.Header.Set("Authorization", "Bearer admin-jwt")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if gotID.UserID != "admin-1" || !gotID.HasRole(identity.RoleAdmin) {
		t.Fatalf("got %+v, want admin-1", gotID)
	}
}

func TestAuthMiddleware_EmptyBearerIs401(t *testing.T) {
	mw := storefront.AuthMiddleware(&stubTokenParser{})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthMiddleware_OpaqueBearerFallsBackToSessionCookie(t *testing.T) {
	id, err := identity.NewIdentity("user-session", identity.RoleCustomer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := &mapParser{byToken: map[string]parseResult{
		"owner-token":  {err: fmt.Errorf("%w: opaque", auth.ErrNotCustomerToken)},
		"cookie-token": {id: id},
	}}
	mw := storefront.AuthMiddleware(p)

	var gotID identity.Identity
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = auth.IdentityFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/customizer/save", nil)
	req.Header.Set("Authorization", "Bearer owner-token")
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_session", Value: "cookie-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotID.UserID != "user-session" {
		t.Fatalf("UserID = %q, want user-session", gotID.UserID)
	}
	if p.lastToken != "cookie-token" {
		t.Fatalf("lastToken = %q, want cookie-token", p.lastToken)
	}
}

func TestAuthMiddleware_OpaqueBearerWithoutSessionIsGuest(t *testing.T) {
	p := &mapParser{byToken: map[string]parseResult{
		"owner-token": {err: auth.ErrNotCustomerToken},
	}}
	mw := storefront.AuthMiddleware(p)

	var gotID identity.Identity
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = auth.IdentityFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/customizer/save", nil)
	req.Header.Set("Authorization", "Bearer owner-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !gotID.IsGuest() {
		t.Fatalf("expected guest, got %+v", gotID)
	}
}

func TestAuthMiddleware_OpaqueBearerExpiredSessionIsGuest(t *testing.T) {
	p := &mapParser{byToken: map[string]parseResult{
		"owner-token":  {err: auth.ErrNotCustomerToken},
		"cookie-token": {err: errors.New("jwt: token expired")},
	}}
	mw := storefront.AuthMiddleware(p)

	var gotID identity.Identity
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = auth.IdentityFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/customizer/save", nil)
	req.Header.Set("Authorization", "Bearer owner-token")
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_session", Value: "cookie-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !gotID.IsGuest() {
		t.Fatalf("expected guest, got %+v", gotID)
	}
}

func TestAuthMiddleware_NonBearerAuthorizationUsesSessionCookie(t *testing.T) {
	id, err := identity.NewIdentity("user-4", identity.RoleCustomer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parser := &stubTokenParser{identity: id}
	mw := storefront.AuthMiddleware(parser)

	var gotID identity.Identity
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = auth.IdentityFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/customizer/save", nil)
	req.Header.Set("Authorization", "Owner abc")
	req.AddCookie(&http.Cookie{Name: "shopanda_storefront_session", Value: "cookie-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotID.UserID != "user-4" {
		t.Fatalf("UserID = %q, want user-4", gotID.UserID)
	}
}

func TestAuthMiddleware_NonBearerWithoutSessionIsGuest(t *testing.T) {
	parser := &stubTokenParser{}
	mw := storefront.AuthMiddleware(parser)

	var gotID identity.Identity
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = auth.IdentityFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !gotID.IsGuest() {
		t.Fatalf("expected guest, got %+v", gotID)
	}
}

type parseResult struct {
	id  identity.Identity
	err error
}

type mapParser struct {
	byToken   map[string]parseResult
	lastToken string
	calls     int
}

func (p *mapParser) Parse(_ context.Context, token string) (identity.Identity, error) {
	p.calls++
	p.lastToken = token
	res, ok := p.byToken[token]
	if !ok {
		return identity.Identity{}, errors.New("unexpected token: " + token)
	}
	return res.id, res.err
}

func TestRequireAuth_Authenticated(t *testing.T) {
	id, err := identity.NewIdentity("user-1", identity.RoleCustomer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mw := storefront.RequireAuth()

	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	ctx := auth.WithIdentity(req.Context(), id)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !called {
		t.Error("expected handler to be called")
	}
}

func TestRequireAuth_Guest(t *testing.T) {
	mw := storefront.RequireAuth()

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	ctx := auth.WithIdentity(req.Context(), identity.Guest())
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
