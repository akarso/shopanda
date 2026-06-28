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
	CreatedAt  time.Time
}

// NewIssueEntry validates an issue ledger entry.
func NewIssueEntry(id, customerID string, amount shared.Money, note string) (Entry, error) {
	return newEntry(id, customerID, amount, KindIssue, "", note)
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
