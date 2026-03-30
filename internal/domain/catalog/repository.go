package catalog

import "context"

// ProductRepository defines persistence operations for products.
type ProductRepository interface {
	// FindByID returns a product by its ID.
	// Returns a nil product and no error when the product does not exist.
	FindByID(ctx context.Context, id string) (*Product, error)

	// FindBySlug returns a product by its slug.
	FindBySlug(ctx context.Context, slug string) (*Product, error)

	// List returns a page of products ordered by created_at desc.
	List(ctx context.Context, offset, limit int) ([]Product, error)

	// Create persists a new product.
	Create(ctx context.Context, p *Product) error

	// Update persists changes to an existing product.
	Update(ctx context.Context, p *Product) error
}
