package checkout

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/id"
)

// SelectShippingStep calculates a shipping rate and creates a pending shipment.
type SelectShippingStep struct {
	provider  shipping.Provider
	shipments shipping.ShipmentRepository
}

// NewSelectShippingStep creates a SelectShippingStep.
func NewSelectShippingStep(
	provider shipping.Provider,
	shipments shipping.ShipmentRepository,
) *SelectShippingStep {
	if provider == nil {
		panic("checkout: shipping provider must not be nil")
	}
	if shipments == nil {
		panic("checkout: shipment repository must not be nil")
	}
	return &SelectShippingStep{provider: provider, shipments: shipments}
}

func (s *SelectShippingStep) Name() string { return "select_shipping" }

func (s *SelectShippingStep) Execute(cctx *Context) error {
	if cctx == nil {
		return fmt.Errorf("select_shipping: checkout context must not be nil")
	}
	if v, ok := cctx.GetMeta("shipment_selected"); ok {
		if b, isBool := v.(bool); isBool && b {
			return nil // idempotent
		}
	}

	if cctx.Order == nil {
		return fmt.Errorf("select_shipping: order not created yet")
	}
	if cctx.Cart == nil {
		return fmt.Errorf("select_shipping: cart not loaded")
	}

	rate, err := s.provider.CalculateRate(
		context.Background(),
		cctx.Order.ID,
		cctx.Currency,
		len(cctx.Cart.Items),
	)
	if err != nil {
		return fmt.Errorf("select_shipping: calculate rate: %w", err)
	}

	shipment, err := shipping.NewShipment(id.New(), cctx.Order.ID, s.provider.Method(), rate.Cost)
	if err != nil {
		return fmt.Errorf("select_shipping: create shipment: %w", err)
	}

	if err := s.shipments.Create(context.Background(), &shipment); err != nil {
		return fmt.Errorf("select_shipping: save shipment: %w", err)
	}

	cctx.SetMeta("shipment", &shipment)
	cctx.SetMeta("shipment_selected", true)
	return nil
}
