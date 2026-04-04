package payment

import "context"

// ProviderResult holds the business-level outcome of a payment provider
// operation. Success indicates whether the payment was approved (true) or
// declined (false). The returned error from Initiate is reserved for
// transport/system-level failures (e.g. network timeout); a declined payment
// returns Success=false with a nil error.
type ProviderResult struct {
	ProviderRef string // external reference (e.g. transaction ID)
	Success     bool   // business outcome: approved (true) vs declined (false)
}

// Provider defines the interface for payment processing.
type Provider interface {
	// Method returns the payment method this provider handles.
	Method() PaymentMethod

	// Initiate starts a payment for the given payment record.
	// Returns a non-nil error only for transport/system failures.
	// Business-level declines are indicated by ProviderResult.Success == false.
	Initiate(ctx context.Context, p *Payment) (ProviderResult, error)
}
