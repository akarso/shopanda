package order

import (
	"errors"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// OrderStatus represents the lifecycle state of an order.
type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusConfirmed OrderStatus = "confirmed"
	OrderStatusPaid      OrderStatus = "paid"
	OrderStatusCancelled OrderStatus = "cancelled"
	OrderStatusFailed    OrderStatus = "failed"
)

// IsValid returns true if s is a recognised order status.
func (s OrderStatus) IsValid() bool {
	switch s {
	case OrderStatusPending, OrderStatusConfirmed, OrderStatusPaid,
		OrderStatusCancelled, OrderStatusFailed:
		return true
	}
	return false
}

// Order represents a finalised purchase snapshot.
type Order struct {
	ID          string
	CustomerID  string
	status      OrderStatus
	Currency    string
	Items       []Item
	TotalAmount shared.Money
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewOrder creates an Order in pending status with validation.
func NewOrder(id, customerID, currency string, items []Item) (Order, error) {
	if id == "" {
		return Order{}, errors.New("order: id must not be empty")
	}
	if customerID == "" {
		return Order{}, errors.New("order: customer id must not be empty")
	}
	if !shared.IsValidCurrency(currency) {
		return Order{}, errors.New("order: invalid currency code")
	}
	if len(items) == 0 {
		return Order{}, errors.New("order: must contain at least one item")
	}
	for i := range items {
		if items[i].UnitPrice.Currency() != currency {
			return Order{}, errors.New("order: item currency mismatch")
		}
	}

	total, err := computeTotal(items, currency)
	if err != nil {
		return Order{}, err
	}

	now := time.Now().UTC()
	return Order{
		ID:          id,
		CustomerID:  customerID,
		status:      OrderStatusPending,
		Currency:    currency,
		Items:       items,
		TotalAmount: total,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// Status returns the current order status.
func (o Order) Status() OrderStatus {
	return o.status
}

// Confirm transitions the order from pending to confirmed.
func (o *Order) Confirm() error {
	if o.status != OrderStatusPending {
		return errors.New("order: can only confirm a pending order")
	}
	o.status = OrderStatusConfirmed
	o.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkPaid transitions the order from confirmed to paid.
func (o *Order) MarkPaid() error {
	if o.status != OrderStatusConfirmed {
		return errors.New("order: can only mark a confirmed order as paid")
	}
	o.status = OrderStatusPaid
	o.UpdatedAt = time.Now().UTC()
	return nil
}

// Cancel transitions the order to cancelled.
// Only pending or confirmed orders may be cancelled.
func (o *Order) Cancel() error {
	if o.status != OrderStatusPending && o.status != OrderStatusConfirmed {
		return errors.New("order: can only cancel a pending or confirmed order")
	}
	o.status = OrderStatusCancelled
	o.UpdatedAt = time.Now().UTC()
	return nil
}

// Fail transitions the order from pending to failed.
func (o *Order) Fail() error {
	if o.status != OrderStatusPending {
		return errors.New("order: can only fail a pending order")
	}
	o.status = OrderStatusFailed
	o.UpdatedAt = time.Now().UTC()
	return nil
}

// SetStatusFromDB restores the status when loading from persistence.
func (o *Order) SetStatusFromDB(s string) error {
	status := OrderStatus(s)
	if !status.IsValid() {
		return errors.New("order: invalid status from db: " + s)
	}
	o.status = status
	return nil
}

// computeTotal sums item line totals.
func computeTotal(items []Item, currency string) (shared.Money, error) {
	total, err := shared.Zero(currency)
	if err != nil {
		return shared.Money{}, err
	}
	for i := range items {
		lt, err := items[i].LineTotal()
		if err != nil {
			return shared.Money{}, err
		}
		total = total.Add(lt)
	}
	return total, nil
}
