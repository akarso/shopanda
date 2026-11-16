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
	ID          string
	ProductID   string
	ProductSlug string // parent product slug when known (avoids a second FindProduct)
	SKU         string
	Name        string
}
