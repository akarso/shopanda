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
	bus    paidEventBus
}

// NewStatusService returns a StatusService backed by orders.
func NewStatusService(orders domainOrder.OrderRepository) *StatusService {
	if orders == nil {
		panic("order: status service repository must not be nil")
	}
	return &StatusService{orders: orders}
}

// WithEventBus publishes order.paid after a successful transition to paid
// (integration ApplyStatus path). Nil skips publish.
func (s *StatusService) WithEventBus(bus paidEventBus) *StatusService {
	if s == nil {
		return nil
	}
	s.bus = bus
	return s
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
		// Already-paid heal: republish so a prior publish failure can still
		// issue grants (IssueIdempotent is a no-op on existing rows).
		if next == domainOrder.OrderStatusPaid {
			_ = PublishPaid(ctx, s.bus, "order.status_service", o.ID, string(previous), string(next))
		}
		return o, previous, false, nil
	}
	if err := ApplyStatusTransition(o, next); err != nil {
		return nil, previous, false, apperror.Validation(err.Error())
	}
	if err := s.orders.UpdateStatus(ctx, o); err != nil {
		return nil, previous, false, err
	}
	if next == domainOrder.OrderStatusPaid {
		// Persist already succeeded — do not fail the caller on publish.
		// Already-paid retries still republish above.
		_ = PublishPaid(ctx, s.bus, "order.status_service", o.ID, string(previous), string(next))
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
