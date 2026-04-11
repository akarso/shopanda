package tax

import (
	"fmt"
	"math"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// Calculate computes the tax amount for the given price and rate.
// In exclusive mode, tax = price * rate / 10000
// In inclusive mode, tax = price - (price * 10000 / (10000 + rate))
func Calculate(price shared.Money, rate TaxRate, mode TaxMode) (shared.Money, error) {
	if !mode.IsValid() {
		return shared.Money{}, fmt.Errorf("tax: invalid mode: %q", mode)
	}

	amount := price.Amount()
	currency := price.Currency()

	var taxAmount int64
	switch mode {
	case ModeExclusive:
		if rate.Rate != 0 {
			abs := amount
			if abs < 0 {
				abs = -abs
			}
			if abs > math.MaxInt64/int64(rate.Rate) {
				return shared.Money{}, fmt.Errorf("tax: exclusive calculation overflow")
			}
		}
		taxAmount = amount * int64(rate.Rate) / 10000
	case ModeInclusive:
		abs := amount
		if abs < 0 {
			abs = -abs
		}
		if abs > math.MaxInt64/10000 {
			return shared.Money{}, fmt.Errorf("tax: inclusive calculation overflow")
		}
		// price = net + tax => net = price * 10000 / (10000 + rate)
		// tax = price - net
		net := amount * 10000 / (10000 + int64(rate.Rate))
		taxAmount = amount - net
	}

	return shared.NewMoney(taxAmount, currency)
}
