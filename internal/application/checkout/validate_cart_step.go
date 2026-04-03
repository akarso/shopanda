package checkout

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

// ValidateCartStep verifies that every cart item references a variant
// that still exists in the catalog.
type ValidateCartStep struct {
	variants catalog.VariantRepository
}

// NewValidateCartStep creates a ValidateCartStep.
func NewValidateCartStep(variants catalog.VariantRepository) *ValidateCartStep {
	if variants == nil {
		panic("checkout: variants must not be nil")
	}
	return &ValidateCartStep{variants: variants}
}

func (s *ValidateCartStep) Name() string { return "validate_cart" }

// Execute checks each cart item's variant exists in the catalog.
func (s *ValidateCartStep) Execute(cctx *Context) error {
	if v, ok := cctx.GetMeta("validated"); ok {
		if b, isBool := v.(bool); isBool && b {
			return nil // idempotent: already validated
		}
	}

	if cctx.Cart == nil {
		return fmt.Errorf("validate_cart: cart not loaded")
	}

	ctx := context.Background()
	for _, item := range cctx.Cart.Items {
		v, err := s.variants.FindByID(ctx, item.VariantID)
		if err != nil {
			return fmt.Errorf("validate_cart: lookup variant %s: %w", item.VariantID, err)
		}
		if v == nil {
			return fmt.Errorf("validate_cart: variant %s no longer exists", item.VariantID)
		}
	}

	cctx.SetMeta("validated", true)
	return nil
}
