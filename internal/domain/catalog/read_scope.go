package catalog

import "context"

type productReadScopeKey struct{}
type productVisibilityBypassKey struct{}

// WithIncludeNonActiveProducts marks ctx so product repository reads include
// draft and archived rows. Storefront and cart callers omit this; admin API
// requests and export jobs set it explicitly (PR-1054).
func WithIncludeNonActiveProducts(ctx context.Context) context.Context {
	return context.WithValue(ctx, productReadScopeKey{}, true)
}

// IncludeNonActiveProducts reports whether ctx allows non-active product reads.
func IncludeNonActiveProducts(ctx context.Context) bool {
	v, _ := ctx.Value(productReadScopeKey{}).(bool)
	return v
}

// WithBypassProductVisibility marks ctx so public visibility-axis gates are
// skipped (PR-1056). Admin REST and operator jobs set this; storefront
// callers omit it. Does not change repository Status filtering — use
// WithIncludeNonActiveProducts for draft/archived rows.
func WithBypassProductVisibility(ctx context.Context) context.Context {
	return context.WithValue(ctx, productVisibilityBypassKey{}, true)
}

// BypassProductVisibility reports whether ctx skips four-axis visibility gates.
func BypassProductVisibility(ctx context.Context) bool {
	v, _ := ctx.Value(productVisibilityBypassKey{}).(bool)
	return v
}

// WithOperatorProductReadScope marks ctx for admin/export/import/seed paths:
// include non-active Status rows and bypass axis visibility gates so shared
// list helpers that call AllowProduct/FilterProducts stay consistent with
// admin HTTP (PR-1054 + PR-1056).
func WithOperatorProductReadScope(ctx context.Context) context.Context {
	return WithBypassProductVisibility(WithIncludeNonActiveProducts(ctx))
}

// ActiveForPurchase reports whether p may be sold or checked out on the storefront (PR-1054).
func ActiveForPurchase(p *Product) bool {
	return p != nil && p.Status == StatusActive
}
