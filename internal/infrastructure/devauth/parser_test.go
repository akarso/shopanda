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
