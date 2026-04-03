package password_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/password"
)

func TestHash_And_Compare(t *testing.T) {
	hash, err := password.Hash("secret123")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if hash == "secret123" {
		t.Fatal("hash must not equal plaintext")
	}
	if err := password.Compare(hash, "secret123"); err != nil {
		t.Fatalf("Compare with correct password: %v", err)
	}
}

func TestCompare_WrongPassword(t *testing.T) {
	hash, err := password.Hash("correct")
	if err != nil {
		t.Fatalf("Hash setup: %v", err)
	}
	if err := password.Compare(hash, "wrong"); err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestHash_DifferentResults(t *testing.T) {
	h1, err := password.Hash("same")
	if err != nil {
		t.Fatalf("Hash(1): %v", err)
	}
	h2, err := password.Hash("same")
	if err != nil {
		t.Fatalf("Hash(2): %v", err)
	}
	if h1 == h2 {
		t.Error("expected different hashes for same input (bcrypt uses random salt)")
	}
}
