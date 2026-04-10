package tax

import (
	"fmt"

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
		taxAmount = amount * int64(rate.Rate) / 10000
	case ModeInclusive:
		// price = net + tax => net = price * 10000 / (10000 + rate)
		// tax = price - net
		net := amount * 10000 / (10000 + int64(rate.Rate))
		taxAmount = amount - net
	}

	return shared.NewMoney(taxAmount, currency)
}
