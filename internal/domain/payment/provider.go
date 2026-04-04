package payment

import "context"

// ProviderResult holds the outcome of a payment provider operation.
type ProviderResult struct {
	ProviderRef string // external reference (e.g. transaction ID)
	Success     bool
}

// Provider defines the interface for payment processing.
type Provider interface {
	// Method returns the payment method this provider handles.
	Method() PaymentMethod

	// Initiate starts a payment for the given payment record.
	Initiate(ctx context.Context, p *Payment) (ProviderResult, error)
}
