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
	id, err := identity.NewIdentity("user-1", identity.RoleCustomer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !id.IsAuthenticated() {
		t.Error("expected customer to be authenticated")
	}
	if id.IsGuest() {
		t.Error("expected customer to not be guest")
	}
}

func TestIdentity_HasRole(t *testing.T) {
	id, err := identity.NewIdentity("user-1", identity.RoleAdmin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !id.HasRole(identity.RoleAdmin) {
		t.Error("expected admin to have admin role")
	}
	if id.HasRole(identity.RoleCustomer) {
		t.Error("expected admin to not have customer role")
	}
}
