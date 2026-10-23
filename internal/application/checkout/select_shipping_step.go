package checkout

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// SelectShippingStep calculates a shipping rate and creates a pending shipment.
// Carts whose products are all virtual/downloadable skip the shipping method
// requirement entirely (PR-1050). The requirement must already be stored under
// ShippingRequiredMetaKey by ValidateCartStep.
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
	return &SelectShippingStep{
		providers: providers,
		shipments: shipments,
	}
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

	needsShipping, err := shippingRequiredFromMeta(cctx)
	if err != nil {
		return err
	}
	if !needsShipping {
		cctx.SetMeta("shipping_not_required", true)
		cctx.SetMeta("shipment_selected", true) // step complete; no shipment row
		return nil
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

func shippingRequiredFromMeta(cctx *Context) (bool, error) {
	raw, ok := cctx.GetMeta(ShippingRequiredMetaKey)
	if !ok {
		return true, fmt.Errorf("select_shipping: shipping requirement not resolved (validate_cart must run first)")
	}
	needs, ok := raw.(bool)
	if !ok {
		return true, fmt.Errorf("select_shipping: shipping requirement meta has unexpected type %T", raw)
	}
	return needs, nil
}
