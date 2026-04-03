package jwt_test

import (
	"strings"
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

	token, err := issuer.Create("user-1", "customer", 0)
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
	issuer, err := jwt.NewIssuer("secret-a", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer(a): %v", err)
	}
	other, err := jwt.NewIssuer("secret-b", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer(b): %v", err)
	}

	token, err := issuer.Create("user-1", "customer", 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = other.Parse(token)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestParse_Expired(t *testing.T) {
	issuer, err := jwt.NewIssuer("secret", time.Second)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	token, err := issuer.Create("user-1", "customer", 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	time.Sleep(2 * time.Second)
	_, err = issuer.Parse(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestParse_Malformed(t *testing.T) {
	issuer, err := jwt.NewIssuer("secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	_, err = issuer.Parse("not-a-jwt")
	if err == nil {
		t.Fatal("expected error for malformed token")
	}
}

func TestCreate_EmptySubject(t *testing.T) {
	issuer, err := jwt.NewIssuer("secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	_, err = issuer.Create("", "customer", 0)
	if err == nil {
		t.Fatal("expected error for empty subject")
	}
}

func TestParse_TamperedPayload(t *testing.T) {
	issuer, err := jwt.NewIssuer("secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	token, err := issuer.Create("user-1", "customer", 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Swap a character in the payload section.
	parts := splitToken(token)
	runes := []rune(parts[1])
	if runes[0] == 'A' {
		runes[0] = 'B'
	} else {
		runes[0] = 'A'
	}
	tampered := parts[0] + "." + string(runes) + "." + parts[2]

	_, err = issuer.Parse(tampered)
	if err == nil {
		t.Fatal("expected error for tampered payload")
	}
}

func TestParse_TamperedHeader(t *testing.T) {
	issuer, err := jwt.NewIssuer("secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	token, err := issuer.Create("user-1", "customer", 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	parts := splitToken(token)
	tampered := "AAAA" + "." + parts[1] + "." + parts[2]

	_, err = issuer.Parse(tampered)
	if err == nil {
		t.Fatal("expected error for tampered header")
	}
}

func TestParse_EmptyParts(t *testing.T) {
	issuer, err := jwt.NewIssuer("secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	for _, tok := range []string{"", ".", "..", "a.b", "a.b.c.d"} {
		_, err = issuer.Parse(tok)
		if err == nil {
			t.Errorf("expected error for token %q", tok)
		}
	}
}

func TestCreate_DifferentTokensPerCall(t *testing.T) {
	issuer, err := jwt.NewIssuer("secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	t1, _ := issuer.Create("user-1", "customer", 0)
	time.Sleep(time.Second)
	t2, _ := issuer.Create("user-1", "customer", 0)

	if t1 == t2 {
		t.Error("expected different tokens for different iat")
	}
}

func TestCreate_GenClaim(t *testing.T) {
	issuer, err := jwt.NewIssuer("secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}

	token, err := issuer.Create("user-1", "customer", 42)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	claims, err := issuer.Parse(token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.Gen != 42 {
		t.Errorf("Gen = %d, want 42", claims.Gen)
	}
}

func TestTTL(t *testing.T) {
	issuer, err := jwt.NewIssuer("secret", 2*time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	if issuer.TTL() != 2*time.Hour {
		t.Errorf("TTL = %v, want 2h", issuer.TTL())
	}
}

// splitToken splits a JWT into its three dot-separated parts.
func splitToken(token string) [3]string {
	var out [3]string
	i := 0
	for _, part := range strings.SplitN(token, ".", 3) {
		out[i] = part
		i++
	}
	return out
}
