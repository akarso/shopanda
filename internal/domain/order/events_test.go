package order_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/order"
)

func TestEventConstants(t *testing.T) {
	events := []string{
		order.EventOrderCreated,
		order.EventOrderConfirmed,
		order.EventOrderPaid,
		order.EventOrderCancelled,
		order.EventOrderFailed,
	}
	for _, e := range events {
		if e == "" {
			t.Error("event constant must not be empty")
		}
	}
}
