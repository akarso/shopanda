package catalog

import "context"

// ListFilter controls paginated product listing. Empty Type means all types
// (PR-1051 admin ?type= filter; other callers leave Type unset).
// By default only StatusActive rows are returned; set IncludeNonActive or use
// WithIncludeNonActiveProducts on ctx for admin/export (PR-1054).
type ListFilter struct {
	Type             Type
	Offset           int
	Limit            int
	IncludeNonActive bool
}

// ProductRepository defines persistence operations for products.
type ProductRepository interface {
	// FindByID returns a product by its ID.
	// Returns a nil product and no error when the product does not exist or is
	// not visible under the current read scope (non-active unless ctx opts in).
	FindByID(ctx context.Context, id string) (*Product, error)

	// FindBySlug returns a product by its slug.
	// Returns a nil product and no error when no product matches the slug or
	// the match is not visible under the current read scope.
	FindBySlug(ctx context.Context, slug string) (*Product, error)

	// List returns a page of products ordered by created_at desc.
	// filter.Offset must be >= 0; implementations must return an error for negative values.
	// filter.Limit must be > 0; implementations must return an error for non-positive values.
	// Implementations should cap Limit to a reasonable maximum (e.g. 100).
	// When filter.Type is non-empty, implementations must reject invalid values
	// (same InvalidTypeMessage as Product.Validate); empty Type means all types.
	List(ctx context.Context, filter ListFilter) ([]Product, error)

	// FindByCategoryID returns products belonging to the given category,
	// ordered by created_at desc. Non-active products are omitted unless ctx
	// opts in via WithIncludeNonActiveProducts (PR-1054).
	// offset must be >= 0; implementations must return an error for negative values.
	// limit must be > 0; implementations must return an error for non-positive values.
	// Implementations should cap limit to a reasonable maximum (e.g. 100).
	FindByCategoryID(ctx context.Context, categoryID string, offset, limit int) ([]Product, error)

	// Create persists a new product.
	Create(ctx context.Context, p *Product) error

	// Update persists changes to an existing product.
	Update(ctx context.Context, p *Product) error
}
