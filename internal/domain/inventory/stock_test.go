package inventory_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/inventory"
)

func TestNewStockEntry_Valid(t *testing.T) {
	s, err := inventory.NewStockEntry("variant-1", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.VariantID != "variant-1" {
		t.Errorf("VariantID = %q, want %q", s.VariantID, "variant-1")
	}
	if s.Quantity != 10 {
		t.Errorf("Quantity = %d, want %d", s.Quantity, 10)
	}
	if s.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

func TestNewStockEntry_ZeroQuantity(t *testing.T) {
	s, err := inventory.NewStockEntry("variant-1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Quantity != 0 {
		t.Errorf("Quantity = %d, want 0", s.Quantity)
	}
}

func TestNewStockEntry_EmptyVariantID(t *testing.T) {
	_, err := inventory.NewStockEntry("", 10)
	if err == nil {
		t.Fatal("expected error for empty variant id")
	}
}

func TestNewStockEntry_NegativeQuantity(t *testing.T) {
	_, err := inventory.NewStockEntry("variant-1", -1)
	if err == nil {
		t.Fatal("expected error for negative quantity")
	}
}

func TestStockEntry_IsAvailable(t *testing.T) {
	tests := []struct {
		qty  int
		want bool
	}{
		{0, false},
		{1, true},
		{100, true},
	}
	for _, tt := range tests {
		s, _ := inventory.NewStockEntry("v", tt.qty)
		if got := s.IsAvailable(); got != tt.want {
			t.Errorf("IsAvailable() with qty=%d: got %v, want %v", tt.qty, got, tt.want)
		}
	}
}

func TestStockEntry_HasStock(t *testing.T) {
	s, _ := inventory.NewStockEntry("v", 5)
	tests := []struct {
		needed int
		want   bool
	}{
		{0, true},
		{5, true},
		{6, false},
	}
	for _, tt := range tests {
		if got := s.HasStock(tt.needed); got != tt.want {
			t.Errorf("HasStock(%d) with qty=5: got %v, want %v", tt.needed, got, tt.want)
		}
	}
}
