#!/usr/bin/env python3
"""Generate Go source files for PR-022: Dev auth provider."""

import os

BASE = os.path.dirname(os.path.abspath(__file__))


def write(rel_path, content):
    path = os.path.join(BASE, rel_path)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(content)
    print(f"  wrote {rel_path}")


# ── 1. Infrastructure: dev token parser ──────────────────────────────────

write("internal/infrastructure/devauth/doc.go", """\
// Package devauth provides a development-only TokenParser implementation.
// Tokens use the format "userID:role" with no cryptographic signing.
// This package must NEVER be used in production.
package devauth
""")

write("internal/infrastructure/devauth/parser.go", """\
package devauth

import (
	"context"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/identity"
)

// Parser is a development-only TokenParser.
// It accepts tokens in the format "userID:role" (e.g. "user-1:admin").
type Parser struct{}

// NewParser creates a development token parser.
func NewParser() *Parser {
	return &Parser{}
}

// Parse extracts an Identity from a dev token string.
// The token must be "userID:role" where role is a valid identity.Role.
func (p *Parser) Parse(_ context.Context, token string) (identity.Identity, error) {
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return identity.Identity{}, fmt.Errorf("devauth: invalid token format")
	}

	userID := parts[0]
	role := identity.Role(parts[1])

	id, err := identity.NewIdentity(userID, role)
	if err != nil {
		return identity.Identity{}, fmt.Errorf("devauth: %w", err)
	}
	return id, nil
}

// Token generates a dev token string for the given user ID and role.
func Token(userID string, role identity.Role) string {
	return userID + ":" + string(role)
}
""")

write("internal/infrastructure/devauth/parser_test.go", """\
package devauth_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/infrastructure/devauth"
)

func TestParser_ValidCustomer(t *testing.T) {
	p := devauth.NewParser()
	id, err := p.Parse(context.Background(), "user-1:customer")
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

func TestParser_ValidAdmin(t *testing.T) {
	p := devauth.NewParser()
	id, err := p.Parse(context.Background(), "admin-1:admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.UserID != "admin-1" {
		t.Errorf("UserID = %q, want %q", id.UserID, "admin-1")
	}
	if id.Role != identity.RoleAdmin {
		t.Errorf("Role = %q, want %q", id.Role, identity.RoleAdmin)
	}
}

func TestParser_InvalidFormat_NoColon(t *testing.T) {
	p := devauth.NewParser()
	_, err := p.Parse(context.Background(), "no-colon-here")
	if err == nil {
		t.Fatal("expected error for missing colon")
	}
}

func TestParser_InvalidFormat_Empty(t *testing.T) {
	p := devauth.NewParser()
	_, err := p.Parse(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestParser_InvalidRole(t *testing.T) {
	p := devauth.NewParser()
	_, err := p.Parse(context.Background(), "user-1:superadmin")
	if err == nil {
		t.Fatal("expected error for invalid role")
	}
}

func TestParser_EmptyUserID(t *testing.T) {
	p := devauth.NewParser()
	_, err := p.Parse(context.Background(), ":admin")
	if err == nil {
		t.Fatal("expected error for empty user id")
	}
}

func TestParser_GuestRole(t *testing.T) {
	p := devauth.NewParser()
	_, err := p.Parse(context.Background(), "user-1:guest")
	if err == nil {
		t.Fatal("expected error for guest role (guests have no userID)")
	}
}

func TestParser_ColonInUserID(t *testing.T) {
	p := devauth.NewParser()
	id, err := p.Parse(context.Background(), "org:user-1:admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.UserID != "org:user-1" {
		t.Errorf("UserID = %q, want %q", id.UserID, "org:user-1")
	}
	if id.Role != identity.RoleAdmin {
		t.Errorf("Role = %q, want %q", id.Role, identity.RoleAdmin)
	}
}

func TestToken(t *testing.T) {
	got := devauth.Token("user-1", identity.RoleAdmin)
	want := "user-1:admin"
	if got != want {
		t.Errorf("Token() = %q, want %q", got, want)
	}
}
""")

# ── 2. Test helpers ──────────────────────────────────────────────────────

write("internal/platform/auth/testhelper/doc.go", """\
// Package testhelper provides utilities for injecting authenticated
// identities into HTTP test requests.
package testhelper
""")

write("internal/platform/auth/testhelper/testhelper.go", """\
package testhelper

import (
	"net/http"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/infrastructure/devauth"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// AuthenticatedRequest sets the Authorization header on r using a dev token
// for the given user ID and role. It also injects the identity into the
// request context so handlers that read from context work without middleware.
func AuthenticatedRequest(r *http.Request, userID string, role identity.Role) *http.Request {
	token := devauth.Token(userID, role)
	r.Header.Set("Authorization", "Bearer "+token)

	id, err := identity.NewIdentity(userID, role)
	if err != nil {
		panic("testhelper: " + err.Error())
	}
	return r.WithContext(auth.WithIdentity(r.Context(), id))
}

// AdminRequest is a convenience wrapper for AuthenticatedRequest with RoleAdmin.
func AdminRequest(r *http.Request, userID string) *http.Request {
	return AuthenticatedRequest(r, userID, identity.RoleAdmin)
}

// CustomerRequest is a convenience wrapper for AuthenticatedRequest with RoleCustomer.
func CustomerRequest(r *http.Request, userID string) *http.Request {
	return AuthenticatedRequest(r, userID, identity.RoleCustomer)
}
""")

write("internal/platform/auth/testhelper/testhelper_test.go", """\
package testhelper_test

import (
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
)

func TestAuthenticatedRequest(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req = testhelper.AuthenticatedRequest(req, "user-1", identity.RoleCustomer)

	// Header set.
	got := req.Header.Get("Authorization")
	if got != "Bearer user-1:customer" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer user-1:customer")
	}

	// Context injected.
	id := auth.IdentityFrom(req.Context())
	if id.UserID != "user-1" {
		t.Errorf("context UserID = %q, want %q", id.UserID, "user-1")
	}
	if id.Role != identity.RoleCustomer {
		t.Errorf("context Role = %q, want %q", id.Role, identity.RoleCustomer)
	}
}

func TestAdminRequest(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req = testhelper.AdminRequest(req, "admin-1")

	id := auth.IdentityFrom(req.Context())
	if id.Role != identity.RoleAdmin {
		t.Errorf("Role = %q, want %q", id.Role, identity.RoleAdmin)
	}
}

func TestCustomerRequest(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req = testhelper.CustomerRequest(req, "cust-1")

	id := auth.IdentityFrom(req.Context())
	if id.Role != identity.RoleCustomer {
		t.Errorf("Role = %q, want %q", id.Role, identity.RoleCustomer)
	}
}
""")

print("PR-022 files generated successfully.")
