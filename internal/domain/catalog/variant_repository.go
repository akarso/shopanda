package catalog

import "context"

// VariantRepository defines persistence operations for variants.
type VariantRepository interface {
	// FindByID returns a variant by its ID.
	// Returns a nil variant and no error when the variant does not exist.
	FindByID(ctx context.Context, id string) (*Variant, error)

	// FindBySKU returns a variant by its SKU.
	// Returns a nil variant and no error when no variant matches the SKU.
	FindBySKU(ctx context.Context, sku string) (*Variant, error)

	// ListByProductID returns all variants for the given product.
	ListByProductID(ctx context.Context, productID string) ([]Variant, error)

	// Create persists a new variant.
	Create(ctx context.Context, v *Variant) error

	// Update persists changes to an existing variant.
	Update(ctx context.Context, v *Variant) error
}
