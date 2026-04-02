package inventory_test

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/inventory"
)

func TestNewReservation_Valid(t *testing.T) {
	exp := time.Now().Add(15 * time.Minute)
	r, err := inventory.NewReservation("res-1", "var-1", 3, exp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.ID != "res-1" {
		t.Errorf("ID = %q, want %q", r.ID, "res-1")
	}
	if r.VariantID != "var-1" {
		t.Errorf("VariantID = %q, want %q", r.VariantID, "var-1")
	}
	if r.Quantity != 3 {
		t.Errorf("Quantity = %d, want 3", r.Quantity)
	}
	if r.Status != inventory.ReservationActive {
		t.Errorf("Status = %q, want %q", r.Status, inventory.ReservationActive)
	}
	if r.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestNewReservation_EmptyID(t *testing.T) {
	_, err := inventory.NewReservation("", "var-1", 1, time.Now().Add(time.Minute))
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewReservation_EmptyVariantID(t *testing.T) {
	_, err := inventory.NewReservation("res-1", "", 1, time.Now().Add(time.Minute))
	if err == nil {
		t.Fatal("expected error for empty variant id")
	}
}

func TestNewReservation_ZeroQuantity(t *testing.T) {
	_, err := inventory.NewReservation("res-1", "var-1", 0, time.Now().Add(time.Minute))
	if err == nil {
		t.Fatal("expected error for zero quantity")
	}
}

func TestNewReservation_NegativeQuantity(t *testing.T) {
	_, err := inventory.NewReservation("res-1", "var-1", -1, time.Now().Add(time.Minute))
	if err == nil {
		t.Fatal("expected error for negative quantity")
	}
}

func TestNewReservation_ZeroExpiresAt(t *testing.T) {
	_, err := inventory.NewReservation("res-1", "var-1", 1, time.Time{})
	if err == nil {
		t.Fatal("expected error for zero expires_at")
	}
}

func TestReservation_IsExpired(t *testing.T) {
	exp := time.Now().Add(-time.Minute)
	r, _ := inventory.NewReservation("res-1", "var-1", 1, exp)
	if !r.IsExpired(time.Now()) {
		t.Error("expected reservation to be expired")
	}
}

func TestReservation_IsExpired_ExactBoundary(t *testing.T) {
	exp := time.Now().Add(time.Minute)
	r, _ := inventory.NewReservation("res-1", "var-1", 1, exp)
	if !r.IsExpired(r.ExpiresAt) {
		t.Error("expected reservation to be expired when now == ExpiresAt")
	}
}

func TestReservation_NotExpired(t *testing.T) {
	exp := time.Now().Add(15 * time.Minute)
	r, _ := inventory.NewReservation("res-1", "var-1", 1, exp)
	if r.IsExpired(time.Now()) {
		t.Error("expected reservation to not be expired")
	}
}

func TestReservationStatus_IsValid(t *testing.T) {
	tests := []struct {
		s    inventory.ReservationStatus
		want bool
	}{
		{inventory.ReservationActive, true},
		{inventory.ReservationReleased, true},
		{inventory.ReservationConfirmed, true},
		{"unknown", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.s.IsValid(); got != tt.want {
			t.Errorf("ReservationStatus(%q).IsValid() = %v, want %v", tt.s, got, tt.want)
		}
	}
}
