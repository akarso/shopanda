package manualpay

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/payment"
)

// Compile-time check that Provider implements payment.Provider.
var _ payment.Provider = (*Provider)(nil)

// Provider is the built-in manual payment provider.
// It immediately approves every payment, producing a deterministic
// provider reference of the form "manual:<paymentID>".
type Provider struct{}

// NewProvider returns a new manual payment provider.
func NewProvider() *Provider {
	return &Provider{}
}

// Method returns payment.MethodManual.
func (p *Provider) Method() payment.PaymentMethod {
	return payment.MethodManual
}

// Initiate approves the payment immediately and returns a reference.
// It never returns an error or a declined result.
func (p *Provider) Initiate(_ context.Context, py *payment.Payment) (payment.ProviderResult, error) {
	if py == nil {
		return payment.ProviderResult{}, fmt.Errorf("manualpay: payment must not be nil")
	}
	return payment.ProviderResult{
		ProviderRef: "manual:" + py.ID,
		Success:     true,
	}, nil
}
