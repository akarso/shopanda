package customer_test

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/customer"
)

func TestNewPasswordResetToken(t *testing.T) {
	rt, plaintext, err := customer.NewPasswordResetToken("rt-1", "cust-1", time.Hour)
	if err != nil {
		t.Fatalf("NewPasswordResetToken: %v", err)
	}
	if rt.ID != "rt-1" {
		t.Errorf("ID = %q, want rt-1", rt.ID)
	}
	if rt.CustomerID != "cust-1" {
		t.Errorf("CustomerID = %q, want cust-1", rt.CustomerID)
	}
	if plaintext == "" {
		t.Error("expected non-empty plaintext token")
	}
	if rt.TokenHash == "" {
		t.Error("expected non-empty token hash")
	}
	if rt.TokenHash == plaintext {
		t.Error("token hash should not equal plaintext")
	}
	if rt.IsExpired() {
		t.Error("token should not be expired immediately")
	}
	if rt.IsUsed() {
		t.Error("token should not be used initially")
	}
}

func TestNewPasswordResetToken_EmptyID(t *testing.T) {
	_, _, err := customer.NewPasswordResetToken("", "cust-1", time.Hour)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewPasswordResetToken_EmptyCustomerID(t *testing.T) {
	_, _, err := customer.NewPasswordResetToken("rt-1", "", time.Hour)
	if err == nil {
		t.Fatal("expected error for empty customer_id")
	}
}

func TestPasswordResetToken_MarkUsed(t *testing.T) {
	rt, _, err := customer.NewPasswordResetToken("rt-1", "cust-1", time.Hour)
	if err != nil {
		t.Fatalf("NewPasswordResetToken: %v", err)
	}

	if err := rt.MarkUsed(); err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}
	if !rt.IsUsed() {
		t.Error("expected token to be used after MarkUsed")
	}

	// Double mark should fail.
	if err := rt.MarkUsed(); err == nil {
		t.Fatal("expected error when marking already used token")
	}
}

func TestPasswordResetToken_IsExpired(t *testing.T) {
	rt, _, err := customer.NewPasswordResetToken("rt-1", "cust-1", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewPasswordResetToken: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if !rt.IsExpired() {
		t.Error("expected token to be expired")
	}
}

func TestHashToken_Deterministic(t *testing.T) {
	h1 := customer.HashToken("test-token")
	h2 := customer.HashToken("test-token")
	if h1 != h2 {
		t.Error("HashToken should be deterministic")
	}
	if h1 == "test-token" {
		t.Error("hash should not equal plaintext")
	}
}

func TestHashToken_DifferentInputs(t *testing.T) {
	h1 := customer.HashToken("token-a")
	h2 := customer.HashToken("token-b")
	if h1 == h2 {
		t.Error("different inputs should produce different hashes")
	}
}
