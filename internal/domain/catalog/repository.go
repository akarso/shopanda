package catalog

import "context"

// ProductRepository defines persistence operations for products.
type ProductRepository interface {
	// FindByID returns a product by its ID.
	// Returns a nil product and no error when the product does not exist.
	FindByID(ctx context.Context, id string) (*Product, error)

	// FindBySlug returns a product by its slug.
	// Returns a nil product and no error when no product matches the slug.
	FindBySlug(ctx context.Context, slug string) (*Product, error)

	// List returns a page of products ordered by created_at desc.
	// offset must be >= 0; implementations must return an error for negative values.
	// limit must be > 0; implementations must return an error for non-positive values.
	// Implementations should cap limit to a reasonable maximum (e.g. 100).
	List(ctx context.Context, offset, limit int) ([]Product, error)

	// Create persists a new product.
	Create(ctx context.Context, p *Product) error

	// Update persists changes to an existing product.
	Update(ctx context.Context, p *Product) error
}
