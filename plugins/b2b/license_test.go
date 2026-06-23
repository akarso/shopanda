package b2b_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/plugins/b2b"
)

func TestValidate_EmptyKey(t *testing.T) {
	ok, err := b2b.Validate("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("empty key should not validate")
	}
}

func TestValidate_DevKey(t *testing.T) {
	ok, err := b2b.Validate("DEV-local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("DEV- key should validate in stub mode")
	}
}

func TestValidate_ProductionKeyNotImplemented(t *testing.T) {
	ok, err := b2b.Validate("SHOPANDA-PROD-0001")
	if ok {
		t.Fatal("production key should not validate in stub mode")
	}
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected not-implemented error, got ok=%v err=%v", ok, err)
	}
}
