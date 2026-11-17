package extapi

import "strings"

// CatalogProduct is a minimal product read model for plugin seed/sync paths.
type CatalogProduct struct {
	ID     string
	Slug   string
	Name   string
	Type   string
	Status string
}

// CatalogVariant is a minimal variant read model for plugin seed/sync paths.
type CatalogVariant struct {
	ID        string
	ProductID string
	// ProductVisible is true when the parent product row was returned by the
	// products repository under the current status/read scope (storefront
	// omits non-active unless WithIncludeNonActiveProducts /
	// WithOperatorProductReadScope). It does not apply catalog/search/
	// individually/purchasable visibility axes — those need AllowProduct
	// with facts. False when the products repo is nil or the parent is
	// missing/outside status scope.
	ProductVisible bool
	// ProductSlug is the parent product slug when ProductVisible is true.
	// Empty with ProductVisible false means unavailable under status scope.
	// Empty with ProductVisible true means the loaded parent has a blank slug
	// column — still fail closed for "belongs to slug X" (use BelongsToProductSlug).
	ProductSlug string
	SKU         string
	Name        string
}

// BelongsToProductSlug reports whether v is the expected product under the
// current catalog read. False when the parent is outside status scope, the
// loaded slug is blank, or the slug does not match. Never treat an empty
// ProductSlug alone as "not my product" (that fails open in cart.validate).
func BelongsToProductSlug(v *CatalogVariant, want string) bool {
	if v == nil || !v.ProductVisible {
		return false
	}
	slug := strings.TrimSpace(v.ProductSlug)
	if slug == "" {
		return false
	}
	return slug == strings.TrimSpace(want)
}
