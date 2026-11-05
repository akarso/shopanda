package order

import (
	"context"
	"fmt"
	"strings"

	domainOrder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/event"
)

// PublishCreated publishes domainOrder.EventOrderCreated after the order row
// is persisted (checkout CreateOrderStep). Nil bus is a no-op.
func PublishCreated(ctx context.Context, bus paidEventBus, source string, o *domainOrder.Order) error {
	if bus == nil || o == nil {
		return nil
	}
	orderID := strings.TrimSpace(o.ID)
	if orderID == "" {
		return fmt.Errorf("order: publish created: order id must not be empty")
	}
	itemCount := 0
	for _, item := range o.Items() {
		itemCount += item.Quantity
	}
	return bus.Publish(ctx, event.New(domainOrder.EventOrderCreated, source, domainOrder.OrderCreatedData{
		OrderID:      orderID,
		CustomerID:   o.CustomerID,
		ContactEmail: o.ContactEmail,
		Currency:     o.Currency,
		ItemCount:    itemCount,
	}))
}
