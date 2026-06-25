package returns

import (
	"errors"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// Status represents the RMA lifecycle state.
type Status string

const (
	StatusRequested Status = "requested"
	StatusApproved  Status = "approved"
	StatusReceived  Status = "received"
	StatusRefunded  Status = "refunded"
	StatusRejected  Status = "rejected"
	StatusCancelled Status = "cancelled"
)

// IsValid returns true when s is a known return status.
func (s Status) IsValid() bool {
	switch s {
	case StatusRequested, StatusApproved, StatusReceived, StatusRefunded,
		StatusRejected, StatusCancelled:
		return true
	}
	return false
}

// IsTerminal returns true when no further workflow transitions apply.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusRefunded, StatusRejected, StatusCancelled:
		return true
	}
	return false
}

// Return is a return merchandise authorization linked to an order.
type Return struct {
	ID          string
	OrderID     string
	CustomerID  string
	Reason      string
	status      Status
	Currency    string
	items       []Item
	RestockedAt *time.Time
	RefundedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewReturn creates a return in requested status.
func NewReturn(id, orderID, customerID, reason, currency string, items []Item) (Return, error) {
	if id == "" {
		return Return{}, errors.New("return: id must not be empty")
	}
	if orderID == "" {
		return Return{}, errors.New("return: order id must not be empty")
	}
	if reason == "" {
		return Return{}, errors.New("return: reason must not be empty")
	}
	if !shared.IsValidCurrency(currency) {
		return Return{}, errors.New("return: invalid currency code")
	}
	if len(items) == 0 {
		return Return{}, errors.New("return: must contain at least one item")
	}
	seen := make(map[string]struct{}, len(items))
	for i := range items {
		if items[i].UnitPrice.Currency() != currency {
			return Return{}, errors.New("return: item currency mismatch")
		}
		if _, dup := seen[items[i].VariantID]; dup {
			return Return{}, errors.New("return: duplicate variant id")
		}
		seen[items[i].VariantID] = struct{}{}
	}

	cp := make([]Item, len(items))
	copy(cp, items)

	now := time.Now().UTC()
	return Return{
		ID:         id,
		OrderID:    orderID,
		CustomerID: customerID,
		Reason:     reason,
		status:     StatusRequested,
		Currency:   currency,
		items:      cp,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

// Items returns a defensive copy of return line items.
func (r Return) Items() []Item {
	cp := make([]Item, len(r.items))
	copy(cp, r.items)
	return cp
}

// Status returns the current return status.
func (r Return) Status() Status {
	return r.status
}

// TotalAmount sums line totals for refund/restock calculations.
func (r Return) TotalAmount() (shared.Money, error) {
	total, err := shared.Zero(r.Currency)
	if err != nil {
		return shared.Money{}, err
	}
	for i := range r.items {
		line, err := r.items[i].LineTotal()
		if err != nil {
			return shared.Money{}, err
		}
		total, err = total.AddChecked(line)
		if err != nil {
			return shared.Money{}, err
		}
	}
	return total, nil
}

// Approve transitions requested → approved.
func (r *Return) Approve() error {
	if r.status != StatusRequested {
		return errors.New("return: can only approve a requested return")
	}
	r.status = StatusApproved
	r.touch()
	return nil
}

// Reject transitions requested → rejected.
func (r *Return) Reject() error {
	if r.status != StatusRequested {
		return errors.New("return: can only reject a requested return")
	}
	r.status = StatusRejected
	r.touch()
	return nil
}

// Cancel transitions requested → cancelled.
func (r *Return) Cancel() error {
	if r.status != StatusRequested {
		return errors.New("return: can only cancel a requested return")
	}
	r.status = StatusCancelled
	r.touch()
	return nil
}

// MarkReceived transitions approved → received without recording restock time.
func (r *Return) MarkReceived() error {
	if r.status != StatusApproved {
		return errors.New("return: can only receive an approved return")
	}
	r.status = StatusReceived
	r.touch()
	return nil
}

// RecordRestocked marks inventory restock completion for a received return.
func (r *Return) RecordRestocked(restockedAt time.Time) error {
	if r.status != StatusReceived {
		return errors.New("return: can only record restock for a received return")
	}
	if restockedAt.IsZero() {
		return errors.New("return: restocked_at must not be zero")
	}
	t := restockedAt.UTC()
	r.RestockedAt = &t
	r.touch()
	return nil
}

// MarkRefunded transitions received → refunded.
func (r *Return) MarkRefunded(refundedAt time.Time) error {
	if r.status != StatusReceived {
		return errors.New("return: can only refund a received return")
	}
	if refundedAt.IsZero() {
		return errors.New("return: refunded_at must not be zero")
	}
	r.status = StatusRefunded
	t := refundedAt.UTC()
	r.RefundedAt = &t
	r.touch()
	return nil
}

// SetStatusFromDB restores status when loading from persistence.
func (r *Return) SetStatusFromDB(s string) error {
	status := Status(s)
	if !status.IsValid() {
		return errors.New("return: invalid status from db: " + s)
	}
	r.status = status
	return nil
}

// SetItemsFromDB sets items when loading from persistence.
func (r *Return) SetItemsFromDB(items []Item) error {
	if len(items) == 0 {
		return errors.New("return: items must not be empty")
	}
	for i := range items {
		if items[i].UnitPrice.Currency() != r.Currency {
			return errors.New("return: item currency mismatch")
		}
	}
	cp := make([]Item, len(items))
	copy(cp, items)
	r.items = cp
	return nil
}

func (r *Return) touch() {
	r.UpdatedAt = time.Now().UTC()
}
