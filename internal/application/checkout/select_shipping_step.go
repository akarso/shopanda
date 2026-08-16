package checkout

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// SelectShippingStep calculates a shipping rate and creates a pending shipment.
type SelectShippingStep struct {
	providers *shipping.ProviderRegistry
	shipments shipping.ShipmentRepository
}

// NewSelectShippingStep creates a SelectShippingStep.
func NewSelectShippingStep(
	providers *shipping.ProviderRegistry,
	shipments shipping.ShipmentRepository,
) *SelectShippingStep {
	if providers == nil || providers.Len() == 0 {
		panic("checkout: shipping registry must not be empty")
	}
	if shipments == nil {
		panic("checkout: shipment repository must not be nil")
	}
	return &SelectShippingStep{providers: providers, shipments: shipments}
}

func (s *SelectShippingStep) Name() string { return "select_shipping" }

func (s *SelectShippingStep) Execute(ctx context.Context, cctx *Context) error {
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
	provider, err := s.providers.Resolve(cctx.Input.ShippingMethod)
	if err != nil {
		return apperror.Validation("selected shipping method is unavailable")
	}

	rate, err := provider.CalculateRate(
		ctx,
		cctx.Order.ID,
		cctx.Currency,
		cctx.Cart.TotalQuantity(),
	)
	if err != nil {
		return fmt.Errorf("select_shipping: calculate rate: %w", err)
	}

	shipment, err := shipping.NewShipment(id.New(), cctx.Order.ID, provider.Method(), rate.Cost)
	if err != nil {
		return fmt.Errorf("select_shipping: create shipment: %w", err)
	}

	if err := s.shipments.Create(ctx, &shipment); err != nil {
		return fmt.Errorf("select_shipping: save shipment: %w", err)
	}

	cctx.SetMeta("shipment", &shipment)
	cctx.SetMeta("shipment_selected", true)
	return nil
}
