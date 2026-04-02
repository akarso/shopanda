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
