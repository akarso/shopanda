package payment

import (
	"errors"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// PaymentStatus represents the lifecycle state of a payment.
type PaymentStatus string

const (
	StatusPending   PaymentStatus = "pending"
	StatusCompleted PaymentStatus = "completed"
	StatusFailed    PaymentStatus = "failed"
	StatusRefunded  PaymentStatus = "refunded"
)

// IsValid returns true if s is a known payment status.
func (s PaymentStatus) IsValid() bool {
	switch s {
	case StatusPending, StatusCompleted, StatusFailed, StatusRefunded:
		return true
	}
	return false
}

// PaymentMethod identifies the payment mechanism (e.g. "manual", "stripe").
type PaymentMethod string

const (
	MethodManual PaymentMethod = "manual"
)

// IsValid returns true if m is a known payment method.
func (m PaymentMethod) IsValid() bool {
	switch m {
	case MethodManual:
		return true
	}
	return false
}

// Payment represents a payment attempt for an order.
type Payment struct {
	ID          string
	OrderID     string
	Method      PaymentMethod
	status      PaymentStatus
	Amount      shared.Money
	ProviderRef string // external reference from the payment provider
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewPayment creates a pending payment for the given order.
func NewPayment(id, orderID string, method PaymentMethod, amount shared.Money) (Payment, error) {
	if id == "" {
		return Payment{}, errors.New("payment: id must not be empty")
	}
	if orderID == "" {
		return Payment{}, errors.New("payment: order id must not be empty")
	}
	if !method.IsValid() {
		return Payment{}, errors.New("payment: invalid payment method")
	}
	if !amount.IsPositive() {
		return Payment{}, errors.New("payment: amount must be positive")
	}

	now := time.Now().UTC()
	return Payment{
		ID:        id,
		OrderID:   orderID,
		Method:    method,
		status:    StatusPending,
		Amount:    amount,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// Status returns the current payment status.
func (p Payment) Status() PaymentStatus {
	return p.status
}

// Currency returns the ISO 4217 currency code derived from Amount.
func (p Payment) Currency() string {
	return p.Amount.Currency()
}

// Complete transitions a pending payment to completed.
func (p *Payment) Complete(providerRef string) error {
	if p.status != StatusPending {
		return errors.New("payment: can only complete a pending payment")
	}
	p.status = StatusCompleted
	p.ProviderRef = providerRef
	p.UpdatedAt = time.Now().UTC()
	return nil
}

// Fail transitions a pending payment to failed.
func (p *Payment) Fail() error {
	if p.status != StatusPending {
		return errors.New("payment: can only fail a pending payment")
	}
	p.status = StatusFailed
	p.UpdatedAt = time.Now().UTC()
	return nil
}

// Refund transitions a completed payment to refunded.
func (p *Payment) Refund() error {
	if p.status != StatusCompleted {
		return errors.New("payment: can only refund a completed payment")
	}
	p.status = StatusRefunded
	p.UpdatedAt = time.Now().UTC()
	return nil
}

// setStatusFromDB reconstructs the status from a database string.
func (p *Payment) setStatusFromDB(s string) error {
	status := PaymentStatus(s)
	if !status.IsValid() {
		return errors.New("payment: invalid status from db: " + s)
	}
	p.status = status
	return nil
}

// NewPaymentFromDB reconstructs a Payment from persisted fields.
// Used exclusively by repository implementations for hydration.
func NewPaymentFromDB(id, orderID string, method PaymentMethod, status string, amount shared.Money, providerRef string, createdAt, updatedAt time.Time) (*Payment, error) {
	s := PaymentStatus(status)
	if !s.IsValid() {
		return nil, errors.New("payment: invalid status from db: " + status)
	}
	if !method.IsValid() {
		return nil, errors.New("payment: invalid method from db: " + string(method))
	}
	return &Payment{
		ID:          id,
		OrderID:     orderID,
		Method:      method,
		status:      s,
		Amount:      amount,
		ProviderRef: providerRef,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}
