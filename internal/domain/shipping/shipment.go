package shipping

import (
	"errors"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// ShippingStatus represents the lifecycle state of a shipment.
type ShippingStatus string

const (
	StatusPending   ShippingStatus = "pending"
	StatusShipped   ShippingStatus = "shipped"
	StatusDelivered ShippingStatus = "delivered"
	StatusCancelled ShippingStatus = "cancelled"
)

// IsValid returns true if s is a known shipping status.
func (s ShippingStatus) IsValid() bool {
	switch s {
	case StatusPending, StatusShipped, StatusDelivered, StatusCancelled:
		return true
	}
	return false
}

// ShippingMethod identifies the shipping mechanism (e.g. "flat_rate").
type ShippingMethod string

const (
	MethodFlatRate    ShippingMethod = "flat_rate"
	MethodWeightBased ShippingMethod = "weight_based"
)

// IsValid returns true if m is a known shipping method.
func (m ShippingMethod) IsValid() bool {
	switch m {
	case MethodFlatRate, MethodWeightBased:
		return true
	}
	return false
}

// Shipment represents a shipping record for an order.
type Shipment struct {
	ID             string
	OrderID        string
	Method         ShippingMethod
	status         ShippingStatus
	Cost           shared.Money
	TrackingNumber string
	ProviderRef    string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewShipment creates a pending shipment for the given order.
func NewShipment(id, orderID string, method ShippingMethod, cost shared.Money) (Shipment, error) {
	if id == "" {
		return Shipment{}, errors.New("shipping: id must not be empty")
	}
	if orderID == "" {
		return Shipment{}, errors.New("shipping: order id must not be empty")
	}
	if !method.IsValid() {
		return Shipment{}, errors.New("shipping: invalid shipping method")
	}
	if cost.Amount() < 0 {
		return Shipment{}, errors.New("shipping: cost must not be negative")
	}

	now := time.Now().UTC()
	return Shipment{
		ID:        id,
		OrderID:   orderID,
		Method:    method,
		status:    StatusPending,
		Cost:      cost,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// Status returns the current shipping status.
func (s Shipment) Status() ShippingStatus {
	return s.status
}

// Currency returns the ISO 4217 currency code derived from Cost.
func (s Shipment) Currency() string {
	return s.Cost.Currency()
}

// Ship transitions a pending shipment to shipped.
func (s *Shipment) Ship(trackingNumber, providerRef string) error {
	if s.status != StatusPending {
		return errors.New("shipping: can only ship a pending shipment")
	}
	s.status = StatusShipped
	s.TrackingNumber = trackingNumber
	s.ProviderRef = providerRef
	s.UpdatedAt = time.Now().UTC()
	return nil
}

// Deliver transitions a shipped shipment to delivered.
func (s *Shipment) Deliver() error {
	if s.status != StatusShipped {
		return errors.New("shipping: can only deliver a shipped shipment")
	}
	s.status = StatusDelivered
	s.UpdatedAt = time.Now().UTC()
	return nil
}

// Cancel transitions a pending shipment to cancelled.
func (s *Shipment) Cancel() error {
	if s.status != StatusPending {
		return errors.New("shipping: can only cancel a pending shipment")
	}
	s.status = StatusCancelled
	s.UpdatedAt = time.Now().UTC()
	return nil
}

// setStatusFromDB reconstructs the status from a database string.
func (s *Shipment) setStatusFromDB(st string) error {
	status := ShippingStatus(st)
	if !status.IsValid() {
		return errors.New("shipping: invalid status from db: " + st)
	}
	s.status = status
	return nil
}

// NewShipmentFromDB reconstructs a Shipment from persisted fields.
// Used exclusively by repository implementations for hydration.
func NewShipmentFromDB(id, orderID string, method ShippingMethod, status string, cost shared.Money, trackingNumber, providerRef string, createdAt, updatedAt time.Time) (*Shipment, error) {
	st := ShippingStatus(status)
	if !st.IsValid() {
		return nil, errors.New("shipping: invalid status from db: " + status)
	}
	if !method.IsValid() {
		return nil, errors.New("shipping: invalid method from db: " + string(method))
	}
	return &Shipment{
		ID:             id,
		OrderID:        orderID,
		Method:         method,
		status:         st,
		Cost:           cost,
		TrackingNumber: trackingNumber,
		ProviderRef:    providerRef,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}, nil
}
