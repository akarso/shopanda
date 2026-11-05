package order

import (
	"context"
	"testing"

	domainOrder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestPublishCreated_NilBusNoop(t *testing.T) {
	o := &domainOrder.Order{ID: "ord-1"}
	if err := PublishCreated(context.Background(), nil, "test", o); err != nil {
		t.Fatalf("PublishCreated(nil bus) = %v", err)
	}
}

func TestPublishCreated_EmptyOrderID(t *testing.T) {
	bus := event.NewBus(logger.New("error"))
	err := PublishCreated(context.Background(), bus, "test", &domainOrder.Order{ID: "  "})
	if err == nil {
		t.Fatal("expected error for empty order id")
	}
}

func TestPublishCreated_PublishesPayload(t *testing.T) {
	bus := event.NewBus(logger.New("error"))
	var got domainOrder.OrderCreatedData
	var name string
	bus.On(domainOrder.EventOrderCreated, func(_ context.Context, evt event.Event) error {
		name = evt.Name
		got = evt.Data.(domainOrder.OrderCreatedData)
		return nil
	})
	item, err := domainOrder.NewItem("v1", "SKU", "Widget", 2, shared.MustNewMoney(100, "EUR"))
	if err != nil {
		t.Fatal(err)
	}
	o, err := domainOrder.NewOrder("ord-1", "cust-1", "a@b.co", "EUR", []domainOrder.Item{item})
	if err != nil {
		t.Fatal(err)
	}
	if err := PublishCreated(context.Background(), bus, "checkout.create_order", &o); err != nil {
		t.Fatal(err)
	}
	if name != domainOrder.EventOrderCreated {
		t.Fatalf("event = %q", name)
	}
	if got.OrderID != "ord-1" || got.CustomerID != "cust-1" || got.Currency != "EUR" || got.ItemCount != 2 {
		t.Fatalf("payload = %+v", got)
	}
}
