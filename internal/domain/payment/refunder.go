package payment

import "context"

// RefundResult holds the outcome of a refund operation.
type RefundResult struct {
	ProviderRef string // external refund reference (e.g. Stripe refund ID)
}

// Refunder processes refunds through a payment provider.
type Refunder interface {
	// Refund creates a refund for the given amount (in minor units) against
	// the payment's ProviderRef. Returns a non-nil error for transport/system
	// failures; a business-level rejection also returns an error with a
	// descriptive message.
	Refund(ctx context.Context, providerRef string, amount int64, currency string) (RefundResult, error)
}
