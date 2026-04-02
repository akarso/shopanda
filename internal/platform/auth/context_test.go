package auth_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/auth"
)

func TestWithIdentity_RoundTrip(t *testing.T) {
	id, err := identity.NewIdentity("user-1", identity.RoleCustomer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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
