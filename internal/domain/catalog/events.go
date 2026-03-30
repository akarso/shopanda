package catalog

// Event names for the catalog domain.
const (
	EventProductCreated = "catalog.product.created"
	EventProductUpdated = "catalog.product.updated"
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
