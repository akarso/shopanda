package checkout

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

// ShippingRequiredMetaKey holds a bool in checkout.Context.Meta set by
// ValidateCartStep before reserve/create-order. SelectShippingStep reads it
// and must not re-query the catalog (avoids orphaning an order on lookup failure).
const ShippingRequiredMetaKey = "shipping_required"

// CartRequiresPhysicalShipping reports whether checkout must collect a shipping
// method for the given cart lines (PR-1050).
//
// Fail-closed rules (require shipping = true):
//   - empty cart / no items
//   - nil products or variants repository
//   - variant or product lookup error
//   - missing variant
//   - missing product (treated as physical / TypeSimple)
//
// Only when every resolved line is virtual or downloadable does this return false.
// variantLookup / productLookup are optional: when non-nil they are tried before
// repository reads (ValidateCartStep meta reuse — PR-1050 / PR-1054).
func CartRequiresPhysicalShipping(
	ctx context.Context,
	c *cart.Cart,
	products catalog.ProductRepository,
	variants catalog.VariantRepository,
	variantLookup func(variantID string) *catalog.Variant,
	productLookup func(productID string) *catalog.Product,
) (bool, error) {
	if c == nil || len(c.Items) == 0 {
		return true, nil
	}
	if products == nil || variants == nil {
		return true, nil
	}

	types := make([]catalog.Type, 0, len(c.Items))
	seenProducts := make(map[string]catalog.Type, len(c.Items))

	for _, item := range c.Items {
		variant := (*catalog.Variant)(nil)
		if variantLookup != nil {
			variant = variantLookup(item.VariantID)
		}
		if variant == nil {
			v, err := variants.FindByID(ctx, item.VariantID)
			if err != nil {
				return true, fmt.Errorf("shipping requirement: lookup variant %s: %w", item.VariantID, err)
			}
			if v == nil {
				return true, fmt.Errorf("shipping requirement: variant %s no longer exists", item.VariantID)
			}
			variant = v
		}

		if typ, ok := seenProducts[variant.ProductID]; ok {
			types = append(types, typ)
			continue
		}

		product := (*catalog.Product)(nil)
		if productLookup != nil {
			product = productLookup(variant.ProductID)
		}
		if product == nil {
			p, err := products.FindByID(ctx, variant.ProductID)
			if err != nil {
				return true, fmt.Errorf("shipping requirement: lookup product %s: %w", variant.ProductID, err)
			}
			product = p
		}
		if product == nil {
			seenProducts[variant.ProductID] = catalog.TypeSimple
			types = append(types, catalog.TypeSimple)
			continue
		}
		seenProducts[variant.ProductID] = product.Type
		types = append(types, product.Type)
	}

	return catalog.AnyRequiresPhysicalShipping(types), nil
}
