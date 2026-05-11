package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
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
	mw := shophttp.AuthMiddleware(parser)

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
	mw := shophttp.AuthMiddleware(parser)

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

func TestAuthMiddleware_UsesStorefrontSessionCookie(t *testing.T) {
	id, err := identity.NewIdentity("user-2", identity.RoleCustomer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parser := &stubTokenParser{identity: id}
	mw := shophttp.AuthMiddleware(parser)

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
	mw := shophttp.AuthMiddleware(parser)

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

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	parser := &stubTokenParser{err: errors.New("bad token")}
	mw := shophttp.AuthMiddleware(parser)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer bad-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if parser.lastToken != "bad-token" {
		t.Errorf("parser received token %q, want %q", parser.lastToken, "bad-token")
	}
}

func TestAuthMiddleware_MalformedHeader(t *testing.T) {
	parser := &stubTokenParser{}
	mw := shophttp.AuthMiddleware(parser)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuth_Authenticated(t *testing.T) {
	id, err := identity.NewIdentity("user-1", identity.RoleCustomer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mw := shophttp.RequireAuth()

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
	mw := shophttp.RequireAuth()

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

func TestRequireRole_Matching(t *testing.T) {
	id, err := identity.NewIdentity("user-1", identity.RoleAdmin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mw := shophttp.RequireRole(identity.RoleAdmin)

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

func TestRequireRole_Mismatch(t *testing.T) {
	id, err := identity.NewIdentity("user-1", identity.RoleCustomer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mw := shophttp.RequireRole(identity.RoleAdmin)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	ctx := auth.WithIdentity(req.Context(), id)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRequireRole_Guest(t *testing.T) {
	mw := shophttp.RequireRole(identity.RoleAdmin)

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

func TestRequirePermission_Granted(t *testing.T) {
	// Manager has products.read permission.
	id, err := identity.NewIdentity("user-1", identity.RoleManager)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mw := shophttp.RequirePermission(rbac.ProductsRead)

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

func TestRequirePermission_Denied(t *testing.T) {
	// Support does NOT have settings.write permission.
	id, err := identity.NewIdentity("user-1", identity.RoleSupport)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mw := shophttp.RequirePermission(rbac.SettingsWrite)

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

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if called {
		t.Error("expected handler not to be called")
	}
}

func TestRequirePermission_Guest(t *testing.T) {
	mw := shophttp.RequirePermission(rbac.ProductsRead)

	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
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
	if called {
		t.Error("expected handler not to be called")
	}
}

func TestAdminContextMiddleware_Authenticated(t *testing.T) {
	id, err := identity.NewIdentity("admin-1", identity.RoleAdmin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mw := shophttp.AdminContextMiddleware()

	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		ac, ctxErr := admin.FromContext(r.Context())
		if ctxErr != nil {
			t.Fatalf("FromContext error: %v", ctxErr)
		}
		if ac.AdminID != "admin-1" {
			t.Fatalf("AdminID = %q, want %q", ac.AdminID, "admin-1")
		}
		if len(ac.Permissions) == 0 {
			t.Fatal("Permissions should not be empty for admin role")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !called {
		t.Error("expected handler to be called")
	}
}

func TestAdminContextMiddleware_GuestNoContext(t *testing.T) {
	mw := shophttp.AdminContextMiddleware()

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := admin.FromContext(r.Context()); err == nil {
			t.Fatal("expected no admin context for guest")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), identity.Guest()))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAdminContextMiddleware_RoleCustomer(t *testing.T) {
	id, err := identity.NewIdentity("customer-1", identity.RoleCustomer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mw := shophttp.AdminContextMiddleware()

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, ctxErr := admin.FromContext(r.Context())
		if ctxErr != nil {
			t.Fatalf("FromContext error: %v", ctxErr)
		}
		if ac.AdminID != "customer-1" {
			t.Fatalf("AdminID = %q, want %q", ac.AdminID, "customer-1")
		}
		// Customer role has no admin permissions by design
		if len(ac.Permissions) != 0 {
			t.Fatalf("Permissions should be empty for customer role, got %d", len(ac.Permissions))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAdminContextMiddleware_RoleManager(t *testing.T) {
	id, err := identity.NewIdentity("manager-1", identity.RoleManager)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mw := shophttp.AdminContextMiddleware()

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, ctxErr := admin.FromContext(r.Context())
		if ctxErr != nil {
			t.Fatalf("FromContext error: %v", ctxErr)
		}
		if ac.AdminID != "manager-1" {
			t.Fatalf("AdminID = %q, want %q", ac.AdminID, "manager-1")
		}
		if len(ac.Permissions) == 0 {
			t.Fatal("Permissions should not be empty for manager role")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAdminContextMiddleware_RoleSupport(t *testing.T) {
	id, err := identity.NewIdentity("support-1", identity.RoleSupport)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mw := shophttp.AdminContextMiddleware()

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, ctxErr := admin.FromContext(r.Context())
		if ctxErr != nil {
			t.Fatalf("FromContext error: %v", ctxErr)
		}
		if ac.AdminID != "support-1" {
			t.Fatalf("AdminID = %q, want %q", ac.AdminID, "support-1")
		}
		if len(ac.Permissions) == 0 {
			t.Fatal("Permissions should not be empty for support role")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAdminContextMiddleware_WithScopeHeaders(t *testing.T) {
	id, err := identity.NewIdentity("admin-1", identity.RoleAdmin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mw := shophttp.AdminContextMiddleware()

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, ctxErr := admin.FromContext(r.Context())
		if ctxErr != nil {
			t.Fatalf("FromContext error: %v", ctxErr)
		}
		if ac.StoreID != "store-eu" {
			t.Fatalf("StoreID = %q, want %q", ac.StoreID, "store-eu")
		}
		if ac.Language != "en" {
			t.Fatalf("Language = %q, want %q", ac.Language, "en")
		}
		if ac.Currency != "EUR" {
			t.Fatalf("Currency = %q, want %q", ac.Currency, "EUR")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Admin-Store-ID", " store-eu ")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAdminContextMiddleware_ScopeHeaderSanitization(t *testing.T) {
	id, err := identity.NewIdentity("admin-2", identity.RoleAdmin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mw := shophttp.AdminContextMiddleware()

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, ctxErr := admin.FromContext(r.Context())
		if ctxErr != nil {
			t.Fatalf("FromContext error: %v", ctxErr)
		}
		if len(ac.StoreID) != 64 {
			t.Fatalf("StoreID length = %d, want 64", len(ac.StoreID))
		}
		if ac.Language != "" {
			t.Fatalf("Language = %q, want empty", ac.Language)
		}
		if ac.Currency != "USD" {
			t.Fatalf("Currency = %q, want %q", ac.Currency, "USD")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Admin-Store-ID", strings.Repeat("s", 80))
	req.Header.Set("X-Admin-Language", "   ")
	req.Header.Set("X-Admin-Currency", " USD ")
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
