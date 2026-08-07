package example

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

const defaultExampleFeeMinorUnits = 100

// ExampleFeeStep adds a fixed example fee adjustment to the pricing context.
type ExampleFeeStep struct {
	amountMinor *int64
}

// NewExampleFeeStep returns a pricing step that adds amountMinor units in the cart currency.
// When amountMinor points at live config, admin updates apply without restart.
func NewExampleFeeStep(amountMinor *int64) *ExampleFeeStep {
	return &ExampleFeeStep{amountMinor: amountMinor}
}

func (s *ExampleFeeStep) Name() string { return "example.fee" }

func (s *ExampleFeeStep) Apply(_ context.Context, pctx *pricing.PricingContext) error {
	if len(pctx.Items) == 0 {
		return nil
	}
	amount := int64(defaultExampleFeeMinorUnits)
	if s.amountMinor != nil && *s.amountMinor > 0 {
		amount = *s.amountMinor
	}
	fee, err := shared.NewMoney(amount, pctx.Currency)
	if err != nil {
		return err
	}
	adj, err := pricing.NewAdjustment(pricing.AdjustmentFee, "example.fee", fee)
	if err != nil {
		return err
	}
	adj.Description = "Example plugin fee"
	pctx.Adjustments = append(pctx.Adjustments, adj)
	return nil
}

func newOrderCreatedListener(log logger.Logger) event.Handler {
	return func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(order.OrderCreatedData)
		if !ok {
			log.Info("example.order.created", map[string]interface{}{
				"event_id": evt.ID,
				"note":     "unexpected payload type",
			})
			return nil
		}
		log.Info("example.order.created", map[string]interface{}{
			"event_id":   evt.ID,
			"order_id":   data.OrderID,
			"item_count": data.ItemCount,
		})
		return nil
	}
}
