package pluginsdk

import (
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/pkg/extapi"
)

// MoneyFromShared converts core Money to the public extapi type.
func MoneyFromShared(m shared.Money) extapi.Money {
	out, err := extapi.NewMoney(m.Amount(), m.Currency())
	if err != nil {
		// Core Money is always valid; panic would indicate an invariant break.
		panic("pluginsdk: MoneyFromShared: " + err.Error())
	}
	return out
}

// MoneyToShared converts public Money to core shared.Money.
func MoneyToShared(m extapi.Money) (shared.Money, error) {
	return shared.NewMoney(m.Amount(), m.Currency())
}
