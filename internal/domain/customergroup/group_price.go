package customergroup

import (
	"errors"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// GroupPrice is a variant price scoped to a customer group.
// An empty StoreID means the global/default group price for that variant.
type GroupPrice struct {
	ID        string
	GroupID   string
	VariantID string
	StoreID   string
	Amount    shared.Money
	CreatedAt time.Time
}

// NewGroupPrice creates a GroupPrice with validation.
func NewGroupPrice(id, groupID, variantID, storeID string, amount shared.Money) (GroupPrice, error) {
	if id == "" {
		return GroupPrice{}, errors.New("group price: id must not be empty")
	}
	if groupID == "" {
		return GroupPrice{}, errors.New("group price: group id must not be empty")
	}
	if variantID == "" {
		return GroupPrice{}, errors.New("group price: variant id must not be empty")
	}
	if amount.Currency() == "" {
		return GroupPrice{}, errors.New("group price: amount must have a valid currency")
	}
	if !amount.IsPositive() {
		return GroupPrice{}, errors.New("group price: amount must be positive")
	}
	return GroupPrice{
		ID:        id,
		GroupID:   groupID,
		VariantID: variantID,
		StoreID:   storeID,
		Amount:    amount,
		CreatedAt: time.Now().UTC(),
	}, nil
}
