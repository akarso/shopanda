package webhook

import (
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
)

// DeliverJobType is the background job type for outbound webhook delivery.
const DeliverJobType = "webhook.deliver"

// SupportedEvents lists domain events merchants may subscribe to.
var SupportedEvents = []string{
	order.EventOrderCreated,
	order.EventOrderConfirmed,
	order.EventOrderPaid,
	order.EventOrderCancelled,
	order.EventOrderFailed,
	payment.EventPaymentCompleted,
	payment.EventPaymentFailed,
	payment.EventPaymentRefunded,
}

// SupportedEventSet returns supported events as a lookup set.
func SupportedEventSet() map[string]struct{} {
	out := make(map[string]struct{}, len(SupportedEvents))
	for _, name := range SupportedEvents {
		out[name] = struct{}{}
	}
	return out
}
