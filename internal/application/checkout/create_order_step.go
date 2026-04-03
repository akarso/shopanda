package checkout

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/platform/id"
)

// CreateOrderStep builds and persists an order from the cart and pricing snapshot.
type CreateOrderStep struct {
	orders   order.OrderRepository
	variants catalog.VariantRepository
}

// NewCreateOrderStep creates a CreateOrderStep.
func NewCreateOrderStep(orders order.OrderRepository, variants catalog.VariantRepository) *CreateOrderStep {
	if orders == nil {
		panic("checkout: orders must not be nil")
	}
	if variants == nil {
		panic("checkout: variants must not be nil")
	}
	return &CreateOrderStep{orders: orders, variants: variants}
}

func (s *CreateOrderStep) Name() string { return "create_order" }

// Execute creates an order with items sourced from the pricing snapshot.
// Sets cctx.Order and stores order ID in Meta["created_order_id"].
func (s *CreateOrderStep) Execute(cctx *Context) error {
	if v, ok := cctx.GetMeta("created_order_id"); ok {
		if _, isStr := v.(string); isStr && v.(string) != "" {
			return nil // idempotent
		}
	}

	if cctx.Cart == nil {
		return fmt.Errorf("create_order: cart not loaded")
	}

	raw, ok := cctx.GetMeta("pricing")
	if !ok {
		return fmt.Errorf("create_order: pricing context not found in meta")
	}
	pctx, ok := raw.(*pricing.PricingContext)
	if !ok {
		return fmt.Errorf("create_order: invalid pricing context type")
	}

	// Build a map from variantID → pricing item for price lookup.
	priceByVariant := make(map[string]pricing.PricingItem, len(pctx.Items))
	for _, pi := range pctx.Items {
		priceByVariant[pi.VariantID] = pi
	}

	ctx := context.Background()
	items := make([]order.Item, 0, len(cctx.Cart.Items))
	for _, ci := range cctx.Cart.Items {
		pi, found := priceByVariant[ci.VariantID]
		if !found {
			return fmt.Errorf("create_order: no pricing for variant %s", ci.VariantID)
		}

		v, err := s.variants.FindByID(ctx, ci.VariantID)
		if err != nil {
			return fmt.Errorf("create_order: lookup variant %s: %w", ci.VariantID, err)
		}
		if v == nil {
			return fmt.Errorf("create_order: variant %s not found", ci.VariantID)
		}

		oi, err := order.NewItem(ci.VariantID, v.SKU, v.Name, ci.Quantity, pi.UnitPrice)
		if err != nil {
			return fmt.Errorf("create_order: item %s: %w", ci.VariantID, err)
		}
		items = append(items, oi)
	}

	o, err := order.NewOrder(id.New(), cctx.CustomerID, cctx.Currency, items)
	if err != nil {
		return fmt.Errorf("create_order: %w", err)
	}

	if err := s.orders.Save(ctx, &o); err != nil {
		return fmt.Errorf("create_order: save: %w", err)
	}

	cctx.Order = &o
	cctx.SetMeta("created_order_id", o.ID)
	return nil
}
