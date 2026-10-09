package catalog

import "context"

type productReadScopeKey struct{}

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

// ActiveForPurchase reports whether p may be sold or checked out on the storefront (PR-1054).
func ActiveForPurchase(p *Product) bool {
	return p != nil && p.Status == StatusActive
}
