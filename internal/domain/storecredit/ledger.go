package storecredit

import (
	"errors"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// Kind identifies ledger entry direction.
type Kind string

const (
	KindIssue  Kind = "issue"
	KindRedeem Kind = "redeem"
)

// IsValid reports whether k is a known ledger kind.
func (k Kind) IsValid() bool {
	return k == KindIssue || k == KindRedeem
}

// Entry is an immutable store credit ledger row.
type Entry struct {
	ID         string
	CustomerID string
	Currency   string
	Amount     shared.Money
	Kind       Kind
	OrderID    string
	Note       string
	// IdempotencyKey, when non-empty, is unique per (customer, key)
	// (enforced by the repository — see migration 064's partial unique
	// index). Only Issue entries set this. Redeem entries are NOT
	// deduplicated by OrderID: there is no unique constraint on it, and a
	// checkout retry generates a fresh order ID each attempt, so OrderID
	// changes on every retry rather than staying stable — it cannot serve
	// as a replay-detection key. See CreateOrderStep's rollback-key
	// comments (internal/application/checkout/create_order_step.go) for
	// why a naive stable key (e.g. cart ID) is not a safe substitute
	// either, and what a correct fix would require.
	IdempotencyKey string
	CreatedAt      time.Time
}

// NewIssueEntry validates an issue ledger entry. idempotencyKey is optional;
// when set, the repository rejects a second Issue with the same
// (customerID, idempotencyKey) pair instead of crediting twice.
func NewIssueEntry(id, customerID string, amount shared.Money, note, idempotencyKey string) (Entry, error) {
	e, err := newEntry(id, customerID, amount, KindIssue, "", note)
	if err != nil {
		return Entry{}, err
	}
	e.IdempotencyKey = strings.TrimSpace(idempotencyKey)
	return e, nil
}

// NewRedeemEntry validates a redeem ledger entry.
func NewRedeemEntry(id, customerID, orderID string, amount shared.Money) (Entry, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return Entry{}, errors.New("store credit: order id must not be empty for redeem")
	}
	return newEntry(id, customerID, amount, KindRedeem, orderID, "")
}

func newEntry(id, customerID string, amount shared.Money, kind Kind, orderID, note string) (Entry, error) {
	if id == "" {
		return Entry{}, errors.New("store credit: id must not be empty")
	}
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return Entry{}, errors.New("store credit: customer id must not be empty")
	}
	if !kind.IsValid() {
		return Entry{}, errors.New("store credit: invalid kind")
	}
	if amount.Currency() == "" {
		return Entry{}, errors.New("store credit: amount must have a valid currency")
	}
	if !amount.IsPositive() {
		return Entry{}, errors.New("store credit: amount must be positive")
	}
	return Entry{
		ID:         id,
		CustomerID: customerID,
		Currency:   amount.Currency(),
		Amount:     amount,
		Kind:       kind,
		OrderID:    strings.TrimSpace(orderID),
		Note:       strings.TrimSpace(note),
		CreatedAt:  time.Now().UTC(),
	}, nil
}
