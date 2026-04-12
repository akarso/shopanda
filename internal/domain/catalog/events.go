package catalog

// Event names for the catalog domain.
const (
	EventProductCreated  = "catalog.product.created"
	EventProductUpdated  = "catalog.product.updated"
	EventCategoryCreated = "catalog.category.created"
	EventCategoryUpdated = "catalog.category.updated"
)

// ProductCreatedData is the payload for catalog.product.created.
type ProductCreatedData struct {
	ProductID string `json:"product_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Status    Status `json:"status"`
}

// ProductUpdatedData is the payload for catalog.product.updated.
type ProductUpdatedData struct {
	ProductID string `json:"product_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Status    Status `json:"status"`
}

// CategoryCreatedData is the payload for catalog.category.created.
type CategoryCreatedData struct {
	CategoryID string `json:"category_id"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
}

// CategoryUpdatedData is the payload for catalog.category.updated.
type CategoryUpdatedData struct {
	CategoryID string `json:"category_id"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
}
