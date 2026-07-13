package cartdemo

import (
	"fmt"

	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/pkg/extapi"
)

const (
	// DefaultMinQuantity is the default per-line minimum when config is unset.
	DefaultMinQuantity = 2
	// ValidationCodeMinQuantity is the stable validation issue code for storefront clients.
	ValidationCodeMinQuantity = "cartdemo.min_quantity"
)

// minQuantityValidateHandler returns a cart.validate handler enforcing per-line minimum quantity.
func minQuantityValidateHandler(minQty int) extapi.HookHandler {
	return func(hctx *extapi.HookContext) error {
		if minQty <= 0 {
			return nil
		}
		raw, ok := hctx.Get("cart")
		if !ok || raw == nil {
			return nil
		}
		c, ok := raw.(*domainCart.Cart)
		if !ok || c == nil {
			return nil
		}
		for _, item := range c.Items {
			if item.Quantity < minQty {
				hctx.AppendValidationError(extapi.CartValidationIssue{
					Code:      ValidationCodeMinQuantity,
					Message:   fmt.Sprintf("minimum quantity per line is %d", minQty),
					VariantID: item.VariantID,
				})
			}
		}
		return nil
	}
}
