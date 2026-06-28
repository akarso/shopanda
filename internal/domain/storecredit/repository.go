package storecredit

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// Repository persists store credit balances and ledger entries.
type Repository interface {
	// GetBalance returns the current balance for a customer and currency.
	GetBalance(ctx context.Context, customerID, currency string) (shared.Money, error)

	// Issue credits the customer account and appends a ledger entry.
	Issue(ctx context.Context, customerID string, amount shared.Money, note string) error

	// Redeem debits the customer account and appends a ledger entry linked to orderID.
	Redeem(ctx context.Context, customerID, orderID string, amount shared.Money) error

	// ListLedger returns recent ledger entries for a customer, newest first.
	ListLedger(ctx context.Context, customerID, currency string, offset, limit int) ([]Entry, error)
}
