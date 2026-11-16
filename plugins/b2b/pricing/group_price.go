package pricing

import (
	"context"
	"errors"
	"time"

	"github.com/akarso/shopanda/pkg/extapi"
)

// GroupPrice is a variant price scoped to a customer group.
// An empty StoreID means the global/default group price for that variant.
type GroupPrice struct {
	ID        string
	GroupID   string
	VariantID string
	StoreID   string
	Amount    extapi.Money
	CreatedAt time.Time
}

// NewGroupPrice creates a GroupPrice with validation.
func NewGroupPrice(id, groupID, variantID, storeID string, amount extapi.Money) (GroupPrice, error) {
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
	if amount.IsNegative() {
		return GroupPrice{}, errors.New("group price: amount must be non-negative")
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

// GroupPriceRepository persists group-scoped variant prices.
type GroupPriceRepository interface {
	// FindByVariantsGroupCurrencyAndStore returns group prices keyed by variant ID.
	// When storeID is non-empty, variants without a store-scoped row fall back to
	// the global group price (empty store_id).
	FindByVariantsGroupCurrencyAndStore(ctx context.Context, variantIDs []string, groupID, currency, storeID string) (map[string]*GroupPrice, error)

	// FindExactByVariantGroupCurrencyAndStore returns the row for the exact store
	// scope only. (nil, nil) when not found. No global fallback.
	FindExactByVariantGroupCurrencyAndStore(ctx context.Context, variantID, groupID, currency, storeID string) (*GroupPrice, error)

	// FindByVariantGroupCurrencyAndStore returns a single group price row with
	// store-scoped fallback to the global group price when storeID is non-empty.
	FindByVariantGroupCurrencyAndStore(ctx context.Context, variantID, groupID, currency, storeID string) (*GroupPrice, error)

	// Upsert creates or updates a group price for variant+group+currency+store.
	Upsert(ctx context.Context, price *GroupPrice) error

	// Delete removes a group price by ID.
	Delete(ctx context.Context, id string) error
}
