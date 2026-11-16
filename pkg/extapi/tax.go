package extapi

import (
	"context"
	"fmt"
	"math"
)

// TaxMode determines how tax is applied to a price.
type TaxMode string

const (
	// TaxModeExclusive means tax is added on top of the price.
	TaxModeExclusive TaxMode = "exclusive"
	// TaxModeInclusive means the price already includes tax.
	TaxModeInclusive TaxMode = "inclusive"
)

// IsValid reports whether m is a recognised tax mode.
func (m TaxMode) IsValid() bool {
	switch m {
	case TaxModeExclusive, TaxModeInclusive:
		return true
	}
	return false
}

// TaxRate defines the tax percentage for a country + tax class combination,
// optionally scoped to a store. An empty StoreID means the global/default
// rate. Rate is stored as basis points (e.g. 2100 = 21.00%).
type TaxRate struct {
	ID      string
	Country string
	Class   string
	StoreID string
	Rate    int
}

// Percentage returns the rate as a float64 percentage (e.g. 21.00 for 2100 bps).
func (r TaxRate) Percentage() float64 {
	return float64(r.Rate) / 100.0
}

// CalculateTax computes the tax amount for price at rate under mode.
// Exclusive: tax = price * rate / 10000. Inclusive: tax = price - net, where
// net = price * 10000 / (10000 + rate). Same formula as the core tax engine.
func CalculateTax(price Money, rate TaxRate, mode TaxMode) (Money, error) {
	if !mode.IsValid() {
		return Money{}, fmt.Errorf("extapi: tax: invalid mode: %q", mode)
	}

	amount := price.Amount()
	currency := price.Currency()

	var taxAmount int64
	switch mode {
	case TaxModeExclusive:
		if rate.Rate != 0 {
			abs := amount
			if abs < 0 {
				abs = -abs
			}
			if abs > math.MaxInt64/int64(rate.Rate) {
				return Money{}, fmt.Errorf("extapi: tax: exclusive calculation overflow")
			}
		}
		taxAmount = amount * int64(rate.Rate) / 10000
	case TaxModeInclusive:
		abs := amount
		if abs < 0 {
			abs = -abs
		}
		if abs > math.MaxInt64/10000 {
			return Money{}, fmt.Errorf("extapi: tax: inclusive calculation overflow")
		}
		net := amount * 10000 / (10000 + int64(rate.Rate))
		taxAmount = amount - net
	}

	return NewMoney(taxAmount, currency)
}

// TaxCalculator applies tax to a pricing context during the pricing pipeline.
// Register via Host.RegisterTaxCalculator.
type TaxCalculator interface {
	Calculate(ctx context.Context, pctx PricingContext) error
}
