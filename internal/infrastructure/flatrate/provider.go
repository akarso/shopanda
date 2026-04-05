package flatrate

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
)

// Compile-time check that Provider implements shipping.Provider.
var _ shipping.Provider = (*Provider)(nil)

// Provider is the built-in flat rate shipping provider.
// It returns a fixed shipping cost regardless of order contents.
type Provider struct {
	cost shared.Money
}

// NewProvider returns a new flat rate shipping provider with the given cost.
func NewProvider(cost shared.Money) *Provider {
	return &Provider{cost: cost}
}

// Method returns shipping.MethodFlatRate.
func (p *Provider) Method() shipping.ShippingMethod {
	return shipping.MethodFlatRate
}

// CalculateRate returns a flat shipping rate.
// Returns an error if the requested currency does not match the configured cost currency.
func (p *Provider) CalculateRate(_ context.Context, orderID string, currency string, _ int) (shipping.ShippingRate, error) {
	if currency != p.cost.Currency() {
		return shipping.ShippingRate{}, fmt.Errorf("flatrate: unsupported currency %q", currency)
	}
	return shipping.ShippingRate{
		ProviderRef: "flat_rate:" + orderID,
		Cost:        p.cost,
		Label:       "Flat Rate Shipping",
	}, nil
}
