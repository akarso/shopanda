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

	// ListByProductID returns variants for the given product.
	// offset must be >= 0, limit must be > 0. The implementation may cap limit.
	ListByProductID(ctx context.Context, productID string, offset, limit int) ([]Variant, error)

	// Create persists a new variant.
	Create(ctx context.Context, v *Variant) error

	// Update persists changes to an existing variant.
	Update(ctx context.Context, v *Variant) error

	// WithTx returns a repository bound to the given transaction.
	WithTx(tx Tx) VariantRepository
}
