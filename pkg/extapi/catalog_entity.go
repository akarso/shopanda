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
	// ProductSlug is the parent product slug when the products repo is wired
	// and the parent is visible under the current read scope (storefront
	// omits non-active). Empty with a nil error means unavailable — hooks
	// checking "variant belongs to slug X" must fail closed, not skip.
	ProductSlug string
	SKU         string
	Name        string
}
