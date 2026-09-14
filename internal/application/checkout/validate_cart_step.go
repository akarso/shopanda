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

// cartVariantsMetaKey holds a map[string]*catalog.Variant (keyed by
// VariantID) in cctx.Meta, populated by Execute below from the lookups it
// already has to do to confirm each item's variant still exists. Later
// steps that would otherwise re-fetch the same variant a few steps later
// in the same request (e.g. ReserveInventoryStep's stock-event
// publishing) can reuse it via cartVariantFromMeta instead.
const cartVariantsMetaKey = "cart_variants"

// cartVariantFromMeta returns the variant ValidateCartStep already
// resolved for variantID, or nil if it isn't present (ValidateCartStep
// didn't run first, or ran before this key existed) — callers fall back
// to their own lookup in that case, so this is purely an optimization,
// never a correctness dependency.
func cartVariantFromMeta(cctx *Context, variantID string) *catalog.Variant {
	raw, ok := cctx.GetMeta(cartVariantsMetaKey)
	if !ok {
		return nil
	}
	variants, ok := raw.(map[string]*catalog.Variant)
	if !ok {
		return nil
	}
	return variants[variantID]
}

// Execute checks each cart item's variant exists in the catalog.
func (s *ValidateCartStep) Execute(ctx context.Context, cctx *Context) error {
	if v, ok := cctx.GetMeta("validated"); ok {
		if b, isBool := v.(bool); isBool && b {
			return nil // idempotent: already validated
		}
	}

	if cctx.Cart == nil {
		return fmt.Errorf("validate_cart: cart not loaded")
	}

	variants := make(map[string]*catalog.Variant, len(cctx.Cart.Items))
	for _, item := range cctx.Cart.Items {
		v, err := s.variants.FindByID(ctx, item.VariantID)
		if err != nil {
			return fmt.Errorf("validate_cart: lookup variant %s: %w", item.VariantID, err)
		}
		if v == nil {
			return fmt.Errorf("validate_cart: variant %s no longer exists", item.VariantID)
		}
		variants[item.VariantID] = v
	}

	cctx.SetMeta(cartVariantsMetaKey, variants)
	cctx.SetMeta("validated", true)
	return nil
}
