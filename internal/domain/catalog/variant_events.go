package catalog

// Event names for variant lifecycle.
const (
	EventVariantCreated = "catalog.variant.created"
	EventVariantUpdated = "catalog.variant.updated"
)

// VariantCreatedData is the payload for catalog.variant.created.
type VariantCreatedData struct {
	VariantID string `json:"variant_id"`
	ProductID string `json:"product_id"`
	SKU       string `json:"sku"`
}

// VariantUpdatedData is the payload for catalog.variant.updated.
type VariantUpdatedData struct {
	VariantID string `json:"variant_id"`
	ProductID string `json:"product_id"`
	SKU       string `json:"sku"`
}
