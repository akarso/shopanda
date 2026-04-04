package manualpay

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/shared"
)

func TestProvider_Method(t *testing.T) {
	p := NewProvider()
	if got := p.Method(); got != payment.MethodManual {
		t.Errorf("Method() = %q, want %q", got, payment.MethodManual)
	}
}

func TestProvider_Initiate_OK(t *testing.T) {
	prov := NewProvider()
	amount := shared.MustNewMoney(2500, "EUR")
	py, err := payment.NewPayment("pay-1", "ord-1", payment.MethodManual, amount)
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}
	result, err := prov.Initiate(context.Background(), &py)
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	if !result.Success {
		t.Error("expected Success to be true")
	}
	if result.ProviderRef != "manual:pay-1" {
		t.Errorf("ProviderRef = %q, want %q", result.ProviderRef, "manual:pay-1")
	}
}

func TestProvider_Initiate_NilPayment(t *testing.T) {
	prov := NewProvider()
	_, err := prov.Initiate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil payment")
	}
}

func TestProvider_ImplementsInterface(t *testing.T) {
	var _ payment.Provider = (*Provider)(nil)
}
