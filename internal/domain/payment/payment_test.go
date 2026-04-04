package payment

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/shared"
)

func validAmount() shared.Money {
	return shared.MustNewMoney(5000, "EUR")
}

// ── PaymentStatus ───────────────────────────────────────────────────────

func TestPaymentStatus_IsValid(t *testing.T) {
	cases := []struct {
		status PaymentStatus
		want   bool
	}{
		{StatusPending, true},
		{StatusCompleted, true},
		{StatusFailed, true},
		{StatusRefunded, true},
		{"bogus", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := tc.status.IsValid(); got != tc.want {
			t.Errorf("PaymentStatus(%q).IsValid() = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// ── PaymentMethod ───────────────────────────────────────────────────────

func TestPaymentMethod_IsValid(t *testing.T) {
	cases := []struct {
		method PaymentMethod
		want   bool
	}{
		{MethodManual, true},
		{"unknown", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := tc.method.IsValid(); got != tc.want {
			t.Errorf("PaymentMethod(%q).IsValid() = %v, want %v", tc.method, got, tc.want)
		}
	}
}

// ── NewPayment ──────────────────────────────────────────────────────────

func TestNewPayment_OK(t *testing.T) {
	p, err := NewPayment("pay-1", "ord-1", MethodManual, validAmount())
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}
	if p.ID != "pay-1" {
		t.Errorf("ID = %q, want pay-1", p.ID)
	}
	if p.OrderID != "ord-1" {
		t.Errorf("OrderID = %q, want ord-1", p.OrderID)
	}
	if p.Method != MethodManual {
		t.Errorf("Method = %q, want manual", p.Method)
	}
	if p.Status() != StatusPending {
		t.Errorf("Status = %q, want pending", p.Status())
	}
	if p.Amount.Amount() != 5000 {
		t.Errorf("Amount = %d, want 5000", p.Amount.Amount())
	}
	if p.Currency != "EUR" {
		t.Errorf("Currency = %q, want EUR", p.Currency)
	}
	if p.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestNewPayment_EmptyID(t *testing.T) {
	_, err := NewPayment("", "ord-1", MethodManual, validAmount())
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewPayment_EmptyOrderID(t *testing.T) {
	_, err := NewPayment("pay-1", "", MethodManual, validAmount())
	if err == nil {
		t.Fatal("expected error for empty order id")
	}
}

func TestNewPayment_InvalidMethod(t *testing.T) {
	_, err := NewPayment("pay-1", "ord-1", "bogus", validAmount())
	if err == nil {
		t.Fatal("expected error for invalid method")
	}
}

func TestNewPayment_ZeroAmount(t *testing.T) {
	zero := shared.MustNewMoney(0, "EUR")
	_, err := NewPayment("pay-1", "ord-1", MethodManual, zero)
	if err == nil {
		t.Fatal("expected error for zero amount")
	}
}

func TestNewPayment_NegativeAmount(t *testing.T) {
	neg := shared.MustNewMoney(-100, "EUR")
	_, err := NewPayment("pay-1", "ord-1", MethodManual, neg)
	if err == nil {
		t.Fatal("expected error for negative amount")
	}
}

// ── Status transitions ──────────────────────────────────────────────────

func TestPayment_Complete(t *testing.T) {
	p, _ := NewPayment("pay-1", "ord-1", MethodManual, validAmount())
	if err := p.Complete("ref-123"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if p.Status() != StatusCompleted {
		t.Errorf("Status = %q, want completed", p.Status())
	}
	if p.ProviderRef != "ref-123" {
		t.Errorf("ProviderRef = %q, want ref-123", p.ProviderRef)
	}
}

func TestPayment_Complete_NotPending(t *testing.T) {
	p, _ := NewPayment("pay-1", "ord-1", MethodManual, validAmount())
	_ = p.Complete("ref-123")
	if err := p.Complete("ref-456"); err == nil {
		t.Fatal("expected error when completing non-pending payment")
	}
}

func TestPayment_Fail(t *testing.T) {
	p, _ := NewPayment("pay-1", "ord-1", MethodManual, validAmount())
	if err := p.Fail(); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if p.Status() != StatusFailed {
		t.Errorf("Status = %q, want failed", p.Status())
	}
}

func TestPayment_Fail_NotPending(t *testing.T) {
	p, _ := NewPayment("pay-1", "ord-1", MethodManual, validAmount())
	_ = p.Complete("ref")
	if err := p.Fail(); err == nil {
		t.Fatal("expected error when failing non-pending payment")
	}
}

func TestPayment_Refund(t *testing.T) {
	p, _ := NewPayment("pay-1", "ord-1", MethodManual, validAmount())
	_ = p.Complete("ref-123")
	if err := p.Refund(); err != nil {
		t.Fatalf("Refund: %v", err)
	}
	if p.Status() != StatusRefunded {
		t.Errorf("Status = %q, want refunded", p.Status())
	}
}

func TestPayment_Refund_NotCompleted(t *testing.T) {
	p, _ := NewPayment("pay-1", "ord-1", MethodManual, validAmount())
	if err := p.Refund(); err == nil {
		t.Fatal("expected error when refunding non-completed payment")
	}
}

// ── SetStatusFromDB ─────────────────────────────────────────────────────

func TestPayment_SetStatusFromDB_Valid(t *testing.T) {
	p := &Payment{}
	for _, s := range []string{"pending", "completed", "failed", "refunded"} {
		if err := p.SetStatusFromDB(s); err != nil {
			t.Errorf("SetStatusFromDB(%q): %v", s, err)
		}
		if p.Status() != PaymentStatus(s) {
			t.Errorf("Status() = %q, want %q", p.Status(), s)
		}
	}
}

func TestPayment_SetStatusFromDB_Invalid(t *testing.T) {
	p := &Payment{}
	if err := p.SetStatusFromDB("bogus"); err == nil {
		t.Fatal("expected error for invalid status")
	}
}
