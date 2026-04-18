package shipping

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// ── NewZone ─────────────────────────────────────────────────────────────

func TestNewZone_OK(t *testing.T) {
	z, err := NewZone("z-1", "Europe", []string{"DE", "FR"}, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if z.ID != "z-1" {
		t.Errorf("ID = %q, want z-1", z.ID)
	}
	if z.Name != "Europe" {
		t.Errorf("Name = %q, want Europe", z.Name)
	}
	if len(z.Countries) != 2 {
		t.Errorf("Countries len = %d, want 2", len(z.Countries))
	}
	if z.Priority != 10 {
		t.Errorf("Priority = %d, want 10", z.Priority)
	}
	if !z.Active {
		t.Error("Active should be true by default")
	}
	if z.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestNewZone_EmptyID(t *testing.T) {
	_, err := NewZone("", "Europe", []string{"DE"}, 0)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewZone_EmptyName(t *testing.T) {
	_, err := NewZone("z-1", "", []string{"DE"}, 0)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestNewZone_NoCountries(t *testing.T) {
	_, err := NewZone("z-1", "Europe", nil, 0)
	if err == nil {
		t.Fatal("expected error for nil countries")
	}
	_, err = NewZone("z-1", "Europe", []string{}, 0)
	if err == nil {
		t.Fatal("expected error for empty countries")
	}
}

func TestNewZone_InvalidCountryCode(t *testing.T) {
	_, err := NewZone("z-1", "Europe", []string{"DEU"}, 0)
	if err == nil {
		t.Fatal("expected error for 3-char country code")
	}
	_, err = NewZone("z-1", "Europe", []string{"D"}, 0)
	if err == nil {
		t.Fatal("expected error for 1-char country code")
	}
}

// ── NewRateTier ─────────────────────────────────────────────────────────

func TestNewRateTier_OK(t *testing.T) {
	price := shared.MustNewMoney(500, "EUR")
	rt, err := NewRateTier("rt-1", "z-1", 0, 5.0, price)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt.ID != "rt-1" {
		t.Errorf("ID = %q, want rt-1", rt.ID)
	}
	if rt.ZoneID != "z-1" {
		t.Errorf("ZoneID = %q, want z-1", rt.ZoneID)
	}
	if rt.MinWeight != 0 {
		t.Errorf("MinWeight = %v, want 0", rt.MinWeight)
	}
	if rt.MaxWeight != 5.0 {
		t.Errorf("MaxWeight = %v, want 5", rt.MaxWeight)
	}
	if rt.Price.Amount() != 500 {
		t.Errorf("Price = %d, want 500", rt.Price.Amount())
	}
}

func TestNewRateTier_UnlimitedMaxWeight(t *testing.T) {
	price := shared.MustNewMoney(1000, "EUR")
	rt, err := NewRateTier("rt-1", "z-1", 5.0, 0, price)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt.MaxWeight != 0 {
		t.Errorf("MaxWeight = %v, want 0 (unlimited)", rt.MaxWeight)
	}
}

func TestNewRateTier_EmptyID(t *testing.T) {
	price := shared.MustNewMoney(500, "EUR")
	_, err := NewRateTier("", "z-1", 0, 5.0, price)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewRateTier_EmptyZoneID(t *testing.T) {
	price := shared.MustNewMoney(500, "EUR")
	_, err := NewRateTier("rt-1", "", 0, 5.0, price)
	if err == nil {
		t.Fatal("expected error for empty zone id")
	}
}

func TestNewRateTier_NegativeMinWeight(t *testing.T) {
	price := shared.MustNewMoney(500, "EUR")
	_, err := NewRateTier("rt-1", "z-1", -1, 5.0, price)
	if err == nil {
		t.Fatal("expected error for negative min weight")
	}
}

func TestNewRateTier_NegativeMaxWeight(t *testing.T) {
	price := shared.MustNewMoney(500, "EUR")
	_, err := NewRateTier("rt-1", "z-1", 0, -1, price)
	if err == nil {
		t.Fatal("expected error for negative max weight")
	}
}

func TestNewRateTier_MaxLessThanMin(t *testing.T) {
	price := shared.MustNewMoney(500, "EUR")
	_, err := NewRateTier("rt-1", "z-1", 10, 5, price)
	if err == nil {
		t.Fatal("expected error when max < min")
	}
}
