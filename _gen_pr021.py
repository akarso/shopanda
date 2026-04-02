#!/usr/bin/env python3
"""Generate Go source files for PR-021: Identity middleware + roles."""

import os

BASE = os.path.dirname(os.path.abspath(__file__))


def write(rel_path, content):
    path = os.path.join(BASE, rel_path)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(content)
    print(f"  wrote {rel_path}")


# ── 1. Domain: identity package ──────────────────────────────────────────

write("internal/domain/identity/doc.go", """\
// Package identity defines the Identity type and role enumeration.
package identity
""")

write("internal/domain/identity/identity.go", """\
package identity

import "errors"

// Role represents a user role.
type Role string

const (
	RoleGuest    Role = "guest"
	RoleCustomer Role = "customer"
	RoleAdmin    Role = "admin"
)

// IsValid returns true if r is a recognised role.
func (r Role) IsValid() bool {
	switch r {
	case RoleGuest, RoleCustomer, RoleAdmin:
		return true
	}
	return false
}

// Identity represents an authenticated (or anonymous) user.
type Identity struct {
	UserID string
	Role   Role
}

// NewIdentity creates an Identity with the given user ID and role.
func NewIdentity(userID string, role Role) (Identity, error) {
	if userID == "" {
		return Identity{}, errors.New("identity: user id must not be empty")
	}
	if !role.IsValid() {
		return Identity{}, errors.New("identity: invalid role")
	}
	return Identity{UserID: userID, Role: role}, nil
}

// Guest returns a guest identity (no user ID).
func Guest() Identity {
	return Identity{Role: RoleGuest}
}

// IsGuest returns true if the identity is a guest.
func (i Identity) IsGuest() bool {
	return i.Role == RoleGuest
}

// IsAuthenticated returns true if the identity is not a guest.
func (i Identity) IsAuthenticated() bool {
	return i.Role != RoleGuest
}

// HasRole returns true if the identity has the given role.
func (i Identity) HasRole(role Role) bool {
	return i.Role == role
}
""")

write("internal/domain/identity/identity_test.go", """\
package identity_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
)

func TestRoleIsValid(t *testing.T) {
	tests := []struct {
		role identity.Role
		want bool
	}{
		{identity.RoleGuest, true},
		{identity.RoleCustomer, true},
		{identity.RoleAdmin, true},
		{"unknown", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.role.IsValid(); got != tt.want {
			t.Errorf("Role(%q).IsValid() = %v, want %v", tt.role, got, tt.want)
		}
	}
}

func TestNewIdentity(t *testing.T) {
	id, err := identity.NewIdentity("user-1", identity.RoleCustomer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.UserID != "user-1" {
		t.Errorf("UserID = %q, want %q", id.UserID, "user-1")
	}
	if id.Role != identity.RoleCustomer {
		t.Errorf("Role = %q, want %q", id.Role, identity.RoleCustomer)
	}
}

func TestNewIdentity_EmptyUserID(t *testing.T) {
	_, err := identity.NewIdentity("", identity.RoleCustomer)
	if err == nil {
		t.Fatal("expected error for empty user id")
	}
}

func TestNewIdentity_InvalidRole(t *testing.T) {
	_, err := identity.NewIdentity("user-1", "invalid")
	if err == nil {
		t.Fatal("expected error for invalid role")
	}
}

func TestGuest(t *testing.T) {
	g := identity.Guest()
	if !g.IsGuest() {
		t.Error("expected guest to be guest")
	}
	if g.IsAuthenticated() {
		t.Error("expected guest to not be authenticated")
	}
	if g.UserID != "" {
		t.Errorf("guest UserID = %q, want empty", g.UserID)
	}
}

func TestIdentity_IsAuthenticated(t *testing.T) {
	id, _ := identity.NewIdentity("user-1", identity.RoleCustomer)
	if !id.IsAuthenticated() {
		t.Error("expected customer to be authenticated")
	}
	if id.IsGuest() {
		t.Error("expected customer to not be guest")
	}
}

func TestIdentity_HasRole(t *testing.T) {
	id, _ := identity.NewIdentity("user-1", identity.RoleAdmin)
	if !id.HasRole(identity.RoleAdmin) {
		t.Error("expected admin to have admin role")
	}
	if id.HasRole(identity.RoleCustomer) {
		t.Error("expected admin to not have customer role")
	}
}
""")

# ── 2. Platform: auth package ────────────────────────────────────────────

write("internal/platform/auth/context.go", """\
package auth

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/identity"
)

type ctxKey string

const identityKey ctxKey = "identity"

// WithIdentity stores an Identity in the context.
func WithIdentity(ctx context.Context, id identity.Identity) context.Context {
	return context.WithValue(ctx, identityKey, id)
}

// IdentityFrom extracts the Identity from the context.
// Returns a guest identity if none is present.
func IdentityFrom(ctx context.Context) identity.Identity {
	if v, ok := ctx.Value(identityKey).(identity.Identity); ok {
		return v
	}
	return identity.Guest()
}
""")

write("internal/platform/auth/token.go", """\
package auth

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/identity"
)

// TokenParser extracts an Identity from a bearer token string.
// Returns an error if the token is invalid or expired.
type TokenParser interface {
	Parse(ctx context.Context, token string) (identity.Identity, error)
}
""")

write("internal/platform/auth/context_test.go", """\
package auth_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/auth"
)

func TestWithIdentity_RoundTrip(t *testing.T) {
	id, _ := identity.NewIdentity("user-1", identity.RoleCustomer)
	ctx := auth.WithIdentity(context.Background(), id)
	got := auth.IdentityFrom(ctx)
	if got.UserID != id.UserID || got.Role != id.Role {
		t.Errorf("got %+v, want %+v", got, id)
	}
}

func TestIdentityFrom_EmptyContext(t *testing.T) {
	got := auth.IdentityFrom(context.Background())
	if !got.IsGuest() {
		t.Errorf("expected guest identity from empty context, got %+v", got)
	}
}
""")

# ── 3. Interfaces: auth middleware ───────────────────────────────────────

write("internal/interfaces/http/auth_middleware.go", """\
package http

import (
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// AuthMiddleware parses the Authorization header and injects an Identity
// into the request context. If no token is present, a guest identity is
// injected. If the token is invalid, a 401 response is returned.
func AuthMiddleware(parser auth.TokenParser) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				ctx := auth.WithIdentity(r.Context(), identity.Guest())
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if !strings.HasPrefix(header, "Bearer ") {
				JSONError(w, apperror.Unauthorized("invalid authorization header"))
				return
			}
			token := header[len("Bearer "):]

			id, err := parser.Parse(r.Context(), token)
			if err != nil {
				JSONError(w, apperror.Unauthorized("invalid or expired token"))
				return
			}

			ctx := auth.WithIdentity(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth rejects unauthenticated (guest) requests with a 401 response.
func RequireAuth() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.IdentityFrom(r.Context())
			if id.IsGuest() {
				JSONError(w, apperror.Unauthorized("authentication required"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole rejects requests that do not have the specified role.
// Returns 401 for guests and 403 for authenticated users with wrong role.
func RequireRole(role identity.Role) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.IdentityFrom(r.Context())
			if id.IsGuest() {
				JSONError(w, apperror.Unauthorized("authentication required"))
				return
			}
			if !id.HasRole(role) {
				JSONError(w, apperror.Forbidden("insufficient permissions"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
""")

write("internal/interfaces/http/auth_middleware_test.go", """\
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
""")

print("PR-021 files generated successfully.")
