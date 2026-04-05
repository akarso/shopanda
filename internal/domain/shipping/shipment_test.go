package shipping

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

func validCost() shared.Money {
	return shared.MustNewMoney(500, "EUR")
}

func zeroCost() shared.Money {
	return shared.MustNewMoney(0, "EUR")
}

// ── ShippingStatus ──────────────────────────────────────────────────────

func TestShippingStatus_IsValid(t *testing.T) {
	cases := []struct {
		status ShippingStatus
		want   bool
	}{
		{StatusPending, true},
		{StatusShipped, true},
		{StatusDelivered, true},
		{StatusCancelled, true},
		{"bogus", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := tc.status.IsValid(); got != tc.want {
			t.Errorf("ShippingStatus(%q).IsValid() = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// ── ShippingMethod ──────────────────────────────────────────────────────

func TestShippingMethod_IsValid(t *testing.T) {
	cases := []struct {
		method ShippingMethod
		want   bool
	}{
		{MethodFlatRate, true},
		{"unknown", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := tc.method.IsValid(); got != tc.want {
			t.Errorf("ShippingMethod(%q).IsValid() = %v, want %v", tc.method, got, tc.want)
		}
	}
}

// ── NewShipment ─────────────────────────────────────────────────────────

func TestNewShipment_OK(t *testing.T) {
	s, err := NewShipment("ship-1", "ord-1", MethodFlatRate, validCost())
	if err != nil {
		t.Fatalf("NewShipment: %v", err)
	}
	if s.ID != "ship-1" {
		t.Errorf("ID = %q, want ship-1", s.ID)
	}
	if s.OrderID != "ord-1" {
		t.Errorf("OrderID = %q, want ord-1", s.OrderID)
	}
	if s.Method != MethodFlatRate {
		t.Errorf("Method = %q, want flat_rate", s.Method)
	}
	if s.Status() != StatusPending {
		t.Errorf("Status = %q, want pending", s.Status())
	}
	if s.Cost.Amount() != 500 {
		t.Errorf("Cost = %d, want 500", s.Cost.Amount())
	}
	if s.Currency() != "EUR" {
		t.Errorf("Currency = %q, want EUR", s.Currency())
	}
	if s.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestNewShipment_ZeroCost(t *testing.T) {
	// Free shipping is allowed.
	s, err := NewShipment("ship-1", "ord-1", MethodFlatRate, zeroCost())
	if err != nil {
		t.Fatalf("NewShipment with zero cost: %v", err)
	}
	if s.Cost.Amount() != 0 {
		t.Errorf("Cost = %d, want 0", s.Cost.Amount())
	}
}

func TestNewShipment_EmptyID(t *testing.T) {
	_, err := NewShipment("", "ord-1", MethodFlatRate, validCost())
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewShipment_EmptyOrderID(t *testing.T) {
	_, err := NewShipment("ship-1", "", MethodFlatRate, validCost())
	if err == nil {
		t.Fatal("expected error for empty order id")
	}
}

func TestNewShipment_InvalidMethod(t *testing.T) {
	_, err := NewShipment("ship-1", "ord-1", "bogus", validCost())
	if err == nil {
		t.Fatal("expected error for invalid method")
	}
}

func TestNewShipment_NegativeCost(t *testing.T) {
	neg := shared.MustNewMoney(-100, "EUR")
	_, err := NewShipment("ship-1", "ord-1", MethodFlatRate, neg)
	if err == nil {
		t.Fatal("expected error for negative cost")
	}
}

// ── Status transitions ──────────────────────────────────────────────────

func TestShipment_Ship(t *testing.T) {
	s, _ := NewShipment("ship-1", "ord-1", MethodFlatRate, validCost())
	if err := s.Ship("TRACK-123", "ref-abc"); err != nil {
		t.Fatalf("Ship: %v", err)
	}
	if s.Status() != StatusShipped {
		t.Errorf("Status = %q, want shipped", s.Status())
	}
	if s.TrackingNumber != "TRACK-123" {
		t.Errorf("TrackingNumber = %q, want TRACK-123", s.TrackingNumber)
	}
	if s.ProviderRef != "ref-abc" {
		t.Errorf("ProviderRef = %q, want ref-abc", s.ProviderRef)
	}
}

func TestShipment_Ship_NotPending(t *testing.T) {
	s, _ := NewShipment("ship-1", "ord-1", MethodFlatRate, validCost())
	_ = s.Ship("TRACK-123", "ref-abc")
	if err := s.Ship("TRACK-456", "ref-def"); err == nil {
		t.Fatal("expected error when shipping non-pending shipment")
	}
}

func TestShipment_Deliver(t *testing.T) {
	s, _ := NewShipment("ship-1", "ord-1", MethodFlatRate, validCost())
	_ = s.Ship("TRACK-123", "ref-abc")
	if err := s.Deliver(); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if s.Status() != StatusDelivered {
		t.Errorf("Status = %q, want delivered", s.Status())
	}
}

func TestShipment_Deliver_NotShipped(t *testing.T) {
	s, _ := NewShipment("ship-1", "ord-1", MethodFlatRate, validCost())
	if err := s.Deliver(); err == nil {
		t.Fatal("expected error when delivering non-shipped shipment")
	}
}

func TestShipment_Cancel(t *testing.T) {
	s, _ := NewShipment("ship-1", "ord-1", MethodFlatRate, validCost())
	if err := s.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if s.Status() != StatusCancelled {
		t.Errorf("Status = %q, want cancelled", s.Status())
	}
}

func TestShipment_Cancel_NotPending(t *testing.T) {
	s, _ := NewShipment("ship-1", "ord-1", MethodFlatRate, validCost())
	_ = s.Ship("TRACK-123", "ref")
	if err := s.Cancel(); err == nil {
		t.Fatal("expected error when cancelling non-pending shipment")
	}
}

// ── setStatusFromDB ─────────────────────────────────────────────────────

func TestShipment_SetStatusFromDB_Valid(t *testing.T) {
	s := &Shipment{}
	for _, st := range []string{"pending", "shipped", "delivered", "cancelled"} {
		if err := s.setStatusFromDB(st); err != nil {
			t.Errorf("setStatusFromDB(%q): %v", st, err)
		}
		if s.Status() != ShippingStatus(st) {
			t.Errorf("Status() = %q, want %q", s.Status(), st)
		}
	}
}

func TestShipment_SetStatusFromDB_Invalid(t *testing.T) {
	s := &Shipment{}
	if err := s.setStatusFromDB("bogus"); err == nil {
		t.Fatal("expected error for invalid status")
	}
}

// ── NewShipmentFromDB ───────────────────────────────────────────────────

func TestNewShipmentFromDB_OK(t *testing.T) {
	now := time.Now().UTC()
	s, err := NewShipmentFromDB("ship-1", "ord-1", MethodFlatRate, "shipped", validCost(), "TRACK-42", "ref-42", now, now)
	if err != nil {
		t.Fatalf("NewShipmentFromDB: %v", err)
	}
	if s.Status() != StatusShipped {
		t.Errorf("Status = %q, want shipped", s.Status())
	}
	if s.TrackingNumber != "TRACK-42" {
		t.Errorf("TrackingNumber = %q, want TRACK-42", s.TrackingNumber)
	}
	if s.ProviderRef != "ref-42" {
		t.Errorf("ProviderRef = %q, want ref-42", s.ProviderRef)
	}
	if s.Currency() != "EUR" {
		t.Errorf("Currency = %q, want EUR", s.Currency())
	}
}

func TestNewShipmentFromDB_InvalidStatus(t *testing.T) {
	now := time.Now().UTC()
	_, err := NewShipmentFromDB("ship-1", "ord-1", MethodFlatRate, "bogus", validCost(), "", "", now, now)
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestNewShipmentFromDB_InvalidMethod(t *testing.T) {
	now := time.Now().UTC()
	_, err := NewShipmentFromDB("ship-1", "ord-1", "bogus", "pending", validCost(), "", "", now, now)
	if err == nil {
		t.Fatal("expected error for invalid method")
	}
}
