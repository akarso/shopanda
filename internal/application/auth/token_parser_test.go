package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/jwt"
)

func TestValidatingTokenParser_Parse(t *testing.T) {
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	repo := newMockRepo()
	parser := auth.NewValidatingTokenParser(issuer, repo, 0)

	// Create a customer in the repo.
	c, _ := customer.NewCustomer("user-1", "alice@example.com")
	_ = repo.Create(context.Background(), &c)

	token, _ := issuer.Create("user-1", "customer", 0)
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

func TestValidatingTokenParser_Parse_InvalidToken(t *testing.T) {
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	repo := newMockRepo()
	parser := auth.NewValidatingTokenParser(issuer, repo, 0)

	_, err := parser.Parse(context.Background(), "garbage")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestValidatingTokenParser_Parse_CustomerNotFound(t *testing.T) {
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	repo := newMockRepo()
	parser := auth.NewValidatingTokenParser(issuer, repo, 0)

	token, _ := issuer.Create("nonexistent", "customer", 0)
	_, err := parser.Parse(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for non-existent customer")
	}
}

func TestValidatingTokenParser_Parse_GenerationMismatch(t *testing.T) {
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	repo := newMockRepo()
	parser := auth.NewValidatingTokenParser(issuer, repo, 0)

	c, _ := customer.NewCustomer("user-1", "alice@example.com")
	c.BumpTokenGeneration() // gen = 1
	_ = repo.Create(context.Background(), &c)

	// Token with gen=0 (stale).
	token, _ := issuer.Create("user-1", "customer", 0)
	_, err := parser.Parse(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for generation mismatch")
	}
}
