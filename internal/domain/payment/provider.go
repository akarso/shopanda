package payment

import "context"

// ProviderResult holds the business-level outcome of a payment provider
// operation. Success indicates whether the payment was approved (true) or
// declined (false). The returned error from Initiate is reserved for
// transport/system-level failures (e.g. network timeout); a declined payment
// returns Success=false with a nil error.
//
// Async providers (e.g. Stripe) set Pending=true and return a ClientSecret
// that the frontend uses to complete payment confirmation. The payment remains
// in pending status until a webhook confirms the outcome.
type ProviderResult struct {
	ProviderRef  string // external reference (e.g. transaction ID)
	Success      bool   // business outcome: approved (true) vs declined (false)
	Pending      bool   // true when awaiting external confirmation (e.g. webhook)
	ClientSecret string // frontend token for async payment confirmation
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
