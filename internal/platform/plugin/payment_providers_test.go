package plugin_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

type stubPaymentProvider struct {
	method payment.PaymentMethod
}

func (s *stubPaymentProvider) Method() payment.PaymentMethod { return s.method }

func (s *stubPaymentProvider) Initiate(_ context.Context, _ *payment.Payment) (payment.ProviderResult, error) {
	return payment.ProviderResult{}, nil
}

func TestApp_RegisterPaymentProvider_MultipleMethods(t *testing.T) {
	app := &plugin.App{}
	app.RegisterPaymentProvider(&stubPaymentProvider{method: payment.MethodManual})
	app.RegisterPaymentProvider(&stubPaymentProvider{method: payment.MethodStripe})

	reg := app.PaymentRegistry()
	if reg == nil || reg.Len() != 2 {
		t.Fatalf("PaymentRegistry().Len() = %d, want 2", reg.Len())
	}
}

func TestApp_RegisterPaymentProvider_DuplicateMethodPanics(t *testing.T) {
	app := &plugin.App{}
	app.RegisterPaymentProvider(&stubPaymentProvider{method: payment.MethodManual})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for duplicate payment method registration")
		}
	}()
	app.RegisterPaymentProvider(&stubPaymentProvider{method: payment.MethodManual})
}
