package shipping

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// ShippingRate represents a rate quote from a shipping provider.
type ShippingRate struct {
	ProviderRef string     // provider-specific identifier for the rate
	Cost        shared.Money
	Label       string     // human-readable label (e.g. "Standard Shipping")
}

// Provider defines the interface for shipping rate calculation.
type Provider interface {
	// Method returns the shipping method this provider handles.
	Method() ShippingMethod

	// CalculateRate returns a shipping rate for the given order context.
	// Returns a non-nil error only for system-level failures.
	CalculateRate(ctx context.Context, orderID string, currency string, itemCount int) (ShippingRate, error)
}
