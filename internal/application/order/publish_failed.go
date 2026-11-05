package order

import (
	"context"
	"fmt"
	"strings"

	domainOrder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/event"
)

// PublishFailed publishes domainOrder.EventOrderFailed after an order is
// marked failed (checkout unpaid compensation, payment.failed handler).
// Nil bus is a no-op.
func PublishFailed(ctx context.Context, bus paidEventBus, source, orderID, oldStatus, newStatus string) error {
	if bus == nil {
		return nil
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return fmt.Errorf("order: publish failed: order id must not be empty")
	}
	if strings.TrimSpace(newStatus) == "" {
		newStatus = string(domainOrder.OrderStatusFailed)
	}
	return bus.Publish(ctx, event.New(domainOrder.EventOrderFailed, source, domainOrder.OrderStatusChangedData{
		OrderID:   orderID,
		OldStatus: oldStatus,
		NewStatus: newStatus,
	}))
}
