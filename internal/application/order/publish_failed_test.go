package order

import (
	"context"
	"testing"

	domainOrder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestPublishFailed_NilBusNoop(t *testing.T) {
	if err := PublishFailed(context.Background(), nil, "test", "ord-1", "pending", "failed"); err != nil {
		t.Fatalf("PublishFailed(nil bus) = %v", err)
	}
}

func TestPublishFailed_EmptyOrderID(t *testing.T) {
	bus := event.NewBus(logger.New("error"))
	if err := PublishFailed(context.Background(), bus, "test", "  ", "pending", "failed"); err == nil {
		t.Fatal("expected error for empty order id")
	}
}

func TestPublishFailed_PublishesPayload(t *testing.T) {
	bus := event.NewBus(logger.New("error"))
	var got domainOrder.OrderStatusChangedData
	bus.On(domainOrder.EventOrderFailed, func(_ context.Context, evt event.Event) error {
		got = evt.Data.(domainOrder.OrderStatusChangedData)
		return nil
	})
	if err := PublishFailed(context.Background(), bus, "checkout.workflow", "ord-1", "pending", "failed"); err != nil {
		t.Fatal(err)
	}
	if got.OrderID != "ord-1" || got.OldStatus != "pending" || got.NewStatus != "failed" {
		t.Fatalf("payload = %+v", got)
	}
}
