package catalog

import "context"

// CollectionRepository defines persistence operations for collections.
type CollectionRepository interface {
	// FindByID returns a collection by its ID.
	// Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Collection, error)

	// FindBySlug returns a collection by its slug.
	// Returns (nil, nil) when not found.
	FindBySlug(ctx context.Context, slug string) (*Collection, error)

	// List returns all collections ordered by name asc.
	List(ctx context.Context) ([]Collection, error)

	// Create persists a new collection.
	Create(ctx context.Context, c *Collection) error

	// Update persists changes to an existing collection.
	Update(ctx context.Context, c *Collection) error

	// AddProduct assigns a product to a manual collection.
	AddProduct(ctx context.Context, collectionID, productID string) error

	// RemoveProduct removes a product from a manual collection.
	RemoveProduct(ctx context.Context, collectionID, productID string) error

	// ListProductIDs returns the product IDs assigned to a manual collection,
	// ordered by the assignment order.
	ListProductIDs(ctx context.Context, collectionID string) ([]string, error)
}
