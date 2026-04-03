package order_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/order"
)

func TestEventConstants(t *testing.T) {
	cases := []struct {
		got  string
		want string
	}{
		{order.EventOrderCreated, "order.created"},
		{order.EventOrderConfirmed, "order.confirmed"},
		{order.EventOrderPaid, "order.paid"},
		{order.EventOrderCancelled, "order.cancelled"},
		{order.EventOrderFailed, "order.failed"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("event = %q, want %q", tc.got, tc.want)
		}
	}
}
