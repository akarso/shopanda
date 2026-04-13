package pricing

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestNewPriceSnapshot(t *testing.T) {
	amount := shared.MustNewMoney(2999, "EUR")
	s, err := NewPriceSnapshot(id.New(), "variant-1", "", amount)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.VariantID != "variant-1" {
		t.Errorf("variant id = %q, want %q", s.VariantID, "variant-1")
	}
	if !s.Amount.Equal(amount) {
		t.Errorf("amount = %v, want %v", s.Amount, amount)
	}
	if s.RecordedAt.IsZero() {
		t.Error("recorded_at should be set")
	}
}

func TestNewPriceSnapshotEmptyID(t *testing.T) {
	amount := shared.MustNewMoney(100, "EUR")
	_, err := NewPriceSnapshot("", "v1", "", amount)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewPriceSnapshotEmptyVariantID(t *testing.T) {
	amount := shared.MustNewMoney(100, "EUR")
	_, err := NewPriceSnapshot(id.New(), "", "", amount)
	if err == nil {
		t.Fatal("expected error for empty variant id")
	}
}

func TestNewPriceSnapshotZeroAmount(t *testing.T) {
	amount := shared.MustNewMoney(0, "EUR")
	_, err := NewPriceSnapshot(id.New(), "v1", "", amount)
	if err == nil {
		t.Fatal("expected error for zero amount")
	}
}

func TestNewPriceSnapshotNegativeAmount(t *testing.T) {
	amount := shared.MustNewMoney(-100, "EUR")
	_, err := NewPriceSnapshot(id.New(), "v1", "", amount)
	if err == nil {
		t.Fatal("expected error for negative amount")
	}
}

func TestNewPriceSnapshotWithStoreID(t *testing.T) {
	amount := shared.MustNewMoney(1500, "USD")
	s, err := NewPriceSnapshot(id.New(), "v1", "store-1", amount)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.StoreID != "store-1" {
		t.Errorf("store id = %q, want %q", s.StoreID, "store-1")
	}
}
