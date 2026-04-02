package jwt_test

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/jwt"
)

func TestNewIssuer_EmptySecret(t *testing.T) {
	_, err := jwt.NewIssuer("", time.Hour)
	if err == nil {
		t.Fatal("expected error for empty secret")
	}
}

func TestNewIssuer_ZeroTTL(t *testing.T) {
	_, err := jwt.NewIssuer("secret", 0)
	if err == nil {
		t.Fatal("expected error for zero TTL")
	}
}

func TestCreate_And_Parse(t *testing.T) {
	issuer, err := jwt.NewIssuer("test-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	token, err := issuer.Create("user-1", "customer")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := issuer.Parse(token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.Sub != "user-1" {
		t.Errorf("Sub = %q, want user-1", claims.Sub)
	}
	if claims.Role != "customer" {
		t.Errorf("Role = %q, want customer", claims.Role)
	}
	if claims.Exp <= claims.Iat {
		t.Error("Exp should be after Iat")
	}
}

func TestParse_InvalidSignature(t *testing.T) {
	issuer, _ := jwt.NewIssuer("secret-a", time.Hour)
	other, _ := jwt.NewIssuer("secret-b", time.Hour)

	token, _ := issuer.Create("user-1", "customer")
	_, err := other.Parse(token)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestParse_Expired(t *testing.T) {
	issuer, _ := jwt.NewIssuer("secret", time.Second)
	token, _ := issuer.Create("user-1", "customer")
	time.Sleep(2 * time.Second)
	_, err := issuer.Parse(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestParse_Malformed(t *testing.T) {
	issuer, _ := jwt.NewIssuer("secret", time.Hour)
	_, err := issuer.Parse("not-a-jwt")
	if err == nil {
		t.Fatal("expected error for malformed token")
	}
}

func TestCreate_EmptySubject(t *testing.T) {
	issuer, _ := jwt.NewIssuer("secret", time.Hour)
	_, err := issuer.Create("", "customer")
	if err == nil {
		t.Fatal("expected error for empty subject")
	}
}
