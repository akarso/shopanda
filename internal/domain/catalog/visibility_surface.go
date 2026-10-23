package catalog

import "context"

// VisibilitySurface is which public/storefront surface is consuming a product
// (PR-1056). Each surface maps to one resolved visibility axis. Unknown
// surfaces fail closed.
type VisibilitySurface string

const (
	VisibilitySurfaceCatalog      VisibilitySurface = "catalog"
	VisibilitySurfaceSearch       VisibilitySurface = "search"
	VisibilitySurfaceIndividually VisibilitySurface = "individually"
	VisibilitySurfacePurchasable  VisibilitySurface = "purchasable"
)

// AllowedOn reports whether the resolved visibility permits this surface.
func (v Visibility) AllowedOn(surface VisibilitySurface) bool {
	switch surface {
	case VisibilitySurfaceCatalog:
		return v.VisibleInCatalog
	case VisibilitySurfaceSearch:
		return v.VisibleInSearch
	case VisibilitySurfaceIndividually:
		return v.VisibleIndividually
	case VisibilitySurfacePurchasable:
		return v.Purchasable
	default:
		return false
	}
}

// AllowProduct reports whether p passes the four-axis visibility gate for
// surface on the public API. It does NOT replace Status filtering (PR-1054):
// forced VisibilityModeVisible still resolves true for draft/archived; callers
// must keep repository active-only scope (or ActiveForPurchase) in addition.
//
// When BypassProductVisibility(ctx) is set (admin / operator paths), always
// true. Callers supply AutoBasis facts on in/opts (wired in PR-1057/1058).
// A nil facts loader that yields empty inputs fails closed for auto axes but
// still allows forced-visible — load real facts before relying on auto.
func AllowProduct(ctx context.Context, p *Product, surface VisibilitySurface, in VisibilityInputs, opts VisibilityOptions) bool {
	if p == nil {
		return false
	}
	if BypassProductVisibility(ctx) {
		return true
	}
	return p.Visibility(in, opts).AllowedOn(surface)
}

// FilterProducts keeps products allowed on surface, preserving order.
// Always returns a new slice (never aliases products), including on bypass.
//
// This is an in-memory post-fetch filter only. Do not apply it after a SQL
// LIMIT/OFFSET without a matching query-level predicate and recount plan
// (PR-1057) — page size and totals will otherwise be wrong.
//
// facts supplies per-product AutoBasis inputs; nil facts use empty inputs
// (auto axes deny; forced-visible still allow). opts is shared config.
func FilterProducts(
	ctx context.Context,
	products []Product,
	surface VisibilitySurface,
	facts func(Product) VisibilityInputs,
	opts VisibilityOptions,
) []Product {
	if len(products) == 0 {
		return []Product{}
	}
	if BypassProductVisibility(ctx) {
		return append([]Product(nil), products...)
	}
	if facts == nil {
		facts = func(Product) VisibilityInputs { return VisibilityInputs{} }
	}
	out := make([]Product, 0, len(products))
	for i := range products {
		p := &products[i]
		if AllowProduct(ctx, p, surface, facts(*p), opts) {
			out = append(out, products[i])
		}
	}
	return out
}
