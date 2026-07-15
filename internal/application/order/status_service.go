package order

import (
	"context"
	"errors"

	domainOrder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// StatusService applies domain-valid order status transitions for inbound integrations and admin flows.
type StatusService struct {
	orders domainOrder.OrderRepository
}

// NewStatusService returns a StatusService backed by orders.
func NewStatusService(orders domainOrder.OrderRepository) *StatusService {
	if orders == nil {
		panic("order: status service repository must not be nil")
	}
	return &StatusService{orders: orders}
}

// ApplyStatus loads an order and transitions it to next when allowed.
// Returns the previous status and changed=false when the order already has the target status.
func (s *StatusService) ApplyStatus(ctx context.Context, orderID string, next domainOrder.OrderStatus) (*domainOrder.Order, domainOrder.OrderStatus, bool, error) {
	if orderID == "" {
		return nil, "", false, apperror.Validation("order id must not be empty")
	}
	if !next.IsValid() {
		return nil, "", false, apperror.Validation("invalid order status")
	}

	o, err := s.orders.FindByID(ctx, orderID)
	if err != nil {
		return nil, "", false, err
	}
	if o == nil {
		return nil, "", false, apperror.NotFound("order not found")
	}
	previous := o.Status()
	if previous == next {
		return o, previous, false, nil
	}
	if err := ApplyStatusTransition(o, next); err != nil {
		return nil, previous, false, apperror.Validation(err.Error())
	}
	if err := s.orders.UpdateStatus(ctx, o); err != nil {
		return nil, previous, false, err
	}
	return o, previous, true, nil
}

// ApplyStatusTransition applies a domain status transition in memory.
func ApplyStatusTransition(o *domainOrder.Order, next domainOrder.OrderStatus) error {
	switch next {
	case domainOrder.OrderStatusConfirmed:
		return o.Confirm()
	case domainOrder.OrderStatusPaid:
		return o.MarkPaid()
	case domainOrder.OrderStatusCancelled:
		return o.Cancel()
	case domainOrder.OrderStatusFailed:
		return o.Fail()
	default:
		return errors.New("unsupported target status")
	}
}
