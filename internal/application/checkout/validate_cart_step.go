package checkout

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// ValidateCartStep verifies that every cart item references a variant
// that still exists in the catalog, and records whether the cart needs
// physical shipping (before reserve/create-order so a later lookup failure
// cannot orphan an order — PR-1050).
type ValidateCartStep struct {
	variants        catalog.VariantRepository
	products        catalog.ProductRepository
	visibilityFacts catalog.VisibilityFactsSource
	visibilityOpts  catalog.VisibilityOptions
}

// NewValidateCartStep creates a ValidateCartStep.
func NewValidateCartStep(variants catalog.VariantRepository, products catalog.ProductRepository) *ValidateCartStep {
	if variants == nil {
		panic("checkout: variants must not be nil")
	}
	if products == nil {
		panic("checkout: products must not be nil")
	}
	return &ValidateCartStep{variants: variants, products: products}
}

// WithVisibilityFacts enables the four-axis Purchasable check on checkout.
// Nil facts leaves the axis gate off.
func (s *ValidateCartStep) WithVisibilityFacts(facts catalog.VisibilityFactsSource, opts catalog.VisibilityOptions) *ValidateCartStep {
	s.visibilityFacts = facts
	s.visibilityOpts = opts
	return s
}

func (s *ValidateCartStep) Name() string { return "validate_cart" }

// checkPurchasableAxis loads visibility facts once for every distinct product
// in the cart and rejects the first line (in cart order) whose product is not
// purchasable.
func (s *ValidateCartStep) checkPurchasableAxis(ctx context.Context, cctx *Context, variants map[string]*catalog.Variant, productIDs []string) error {
	allowed, err := catalog.FilterIDsWithFacts(ctx, productIDs, catalog.VisibilitySurfacePurchasable, s.visibilityFacts, s.visibilityOpts)
	if err != nil {
		return fmt.Errorf("validate_cart: purchasability: %w", err)
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, id := range allowed {
		allowedSet[id] = struct{}{}
	}
	for _, item := range cctx.Cart.Items {
		if _, ok := allowedSet[variants[item.VariantID].ProductID]; !ok {
			return apperror.Validation(fmt.Sprintf(
				"validate_cart: product for variant %s is not currently purchasable", item.VariantID,
			))
		}
	}
	return nil
}

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

// Execute checks each cart item's variant exists in the catalog and stores
// ShippingRequiredMetaKey for SelectShippingStep.
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
	productsByID := make(map[string]*catalog.Product, len(cctx.Cart.Items))
	productIDs := make([]string, 0, len(cctx.Cart.Items))
	for _, item := range cctx.Cart.Items {
		v, err := s.variants.FindByID(ctx, item.VariantID)
		if err != nil {
			return fmt.Errorf("validate_cart: lookup variant %s: %w", item.VariantID, err)
		}
		if v == nil {
			return fmt.Errorf("validate_cart: variant %s no longer exists", item.VariantID)
		}
		p, ok := productsByID[v.ProductID]
		if !ok {
			p, err = s.products.FindByID(ctx, v.ProductID)
			if err != nil {
				return fmt.Errorf("validate_cart: lookup product %s: %w", v.ProductID, err)
			}
			productsByID[v.ProductID] = p
			productIDs = append(productIDs, v.ProductID)
		}
		if !catalog.ActiveForPurchase(p) {
			return apperror.Validation(fmt.Sprintf(
				"validate_cart: product for variant %s is not available for purchase", item.VariantID,
			))
		}
		variants[item.VariantID] = v
	}

	if err := s.checkPurchasableAxis(ctx, cctx, variants, productIDs); err != nil {
		return err
	}

	cctx.SetMeta(cartVariantsMetaKey, variants)

	needsShipping, err := CartRequiresPhysicalShipping(ctx, cctx.Cart, s.products, s.variants,
		func(variantID string) *catalog.Variant { return variants[variantID] },
		func(productID string) *catalog.Product { return productsByID[productID] },
	)
	if err != nil {
		return fmt.Errorf("validate_cart: shipping requirement: %w", err)
	}
	cctx.SetMeta(ShippingRequiredMetaKey, needsShipping)

	cctx.SetMeta("validated", true)
	return nil
}
