package order

import (
	"context"
	"fmt"
	"strings"

	domainOrder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/event"
)

// paidEventBus is the subset of *event.Bus used to publish order.paid.
type paidEventBus interface {
	Publish(ctx context.Context, evt event.Event) error
}

// PublishPaid publishes domainOrder.EventOrderPaid after money settles.
// Call from sync InitiatePaymentStep success, $0 / payment_skipped checkout,
// payment.completed webhooks (after persist), and ApplyStatus(paid).
// Do not publish from checkout.completed alone (would grant before Stripe capture).
// Publish errors from sync bus handlers are returned so callers can log/fail.
func PublishPaid(ctx context.Context, bus paidEventBus, source, orderID, oldStatus, newStatus string) error {
	if bus == nil {
		return nil
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return fmt.Errorf("order: publish paid: order id must not be empty")
	}
	if strings.TrimSpace(newStatus) == "" {
		newStatus = string(domainOrder.OrderStatusPaid)
	}
	return bus.Publish(ctx, event.New(domainOrder.EventOrderPaid, source, domainOrder.OrderStatusChangedData{
		OrderID:   orderID,
		OldStatus: oldStatus,
		NewStatus: newStatus,
	}))
}
