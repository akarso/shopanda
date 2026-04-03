package jwt_test

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/jwt"
)

func TestTokenParser_Parse(t *testing.T) {
	issuer, err := jwt.NewIssuer("test-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	parser := jwt.NewTokenParser(issuer)

	token, _, err := issuer.Create("user-1", "customer", 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id, err := parser.Parse(context.Background(), token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if id.UserID != "user-1" {
		t.Errorf("UserID = %q, want user-1", id.UserID)
	}
	if id.Role != identity.RoleCustomer {
		t.Errorf("Role = %q, want customer", id.Role)
	}
}

func TestTokenParser_Parse_InvalidToken(t *testing.T) {
	issuer, err := jwt.NewIssuer("test-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	parser := jwt.NewTokenParser(issuer)

	_, err = parser.Parse(context.Background(), "garbage")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}
