package extapi

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
	// ProductVisible is true when the parent product was loaded under the
	// current read scope (storefront omits non-active / visibility-gated
	// parents). False when the products repo is nil, the parent is missing,
	// or the parent is outside scope — do not treat empty ProductSlug alone
	// as "not my product" (that fails open in cart.validate).
	ProductVisible bool
	// ProductSlug is the parent product slug when ProductVisible is true.
	// Empty with ProductVisible false means unavailable under current scope.
	// Empty with ProductVisible true means the loaded parent has a blank slug.
	// Hooks checking "variant belongs to slug X" must fail closed when
	// !ProductVisible or when the slug does not match.
	ProductSlug string
	SKU         string
	Name        string
}
