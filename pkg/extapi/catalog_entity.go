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
	// column — ClassifyProductSlug treats that as Unknown (fail closed).
	ProductSlug string
	SKU         string
	Name        string
}

// ProductSlugClass distinguishes outcomes for "variant belongs to product slug X".
type ProductSlugClass int

const (
	// ProductSlugUnknown: parent outside status scope, blank loaded slug, or nil.
	// Fail closed in cart.validate — do not skip as "not my product".
	ProductSlugUnknown ProductSlugClass = iota
	// ProductSlugOther: parent loaded with a different non-blank slug — skip the line.
	ProductSlugOther
	// ProductSlugMatch: loaded slug equals want — run product-specific checks.
	ProductSlugMatch
)

// ClassifyProductSlug reports how v relates to want under the current catalog read.
//
//	switch extapi.ClassifyProductSlug(v, "my-slug") {
//	case extapi.ProductSlugMatch:
//	  // validate this line
//	case extapi.ProductSlugOther:
//	  return nil // other product on a mixed cart
//	default:
//	  // fail closed (unknown / blank / not visible)
//	}
func ClassifyProductSlug(v *CatalogVariant, want string) ProductSlugClass {
	want = strings.TrimSpace(want)
	if want == "" {
		// Blank target must not become Other (skip-all fail-open).
		return ProductSlugUnknown
	}
	if v == nil || !v.ProductVisible {
		return ProductSlugUnknown
	}
	slug := strings.TrimSpace(v.ProductSlug)
	if slug == "" {
		return ProductSlugUnknown
	}
	if slug == want {
		return ProductSlugMatch
	}
	return ProductSlugOther
}
