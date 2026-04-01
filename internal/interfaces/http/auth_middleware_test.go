package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// stubTokenParser implements auth.TokenParser for testing.
type stubTokenParser struct {
	identity identity.Identity
	err      error
}

func (s *stubTokenParser) Parse(_ context.Context, _ string) (identity.Identity, error) {
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
	id, _ := identity.NewIdentity("user-1", identity.RoleCustomer)
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
	id, _ := identity.NewIdentity("user-1", identity.RoleCustomer)
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
	id, _ := identity.NewIdentity("user-1", identity.RoleAdmin)
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
	id, _ := identity.NewIdentity("user-1", identity.RoleCustomer)
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
