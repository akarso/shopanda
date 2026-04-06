package catalog

import "context"

// CategoryRepository defines persistence operations for categories.
type CategoryRepository interface {
	// FindByID returns a category by its ID.
	// Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Category, error)

	// FindBySlug returns a category by its slug.
	// Returns (nil, nil) when not found.
	FindBySlug(ctx context.Context, slug string) (*Category, error)

	// FindByParentID returns child categories of the given parent,
	// ordered by position asc, then name asc.
	// Pass nil parentID to get root categories.
	FindByParentID(ctx context.Context, parentID *string) ([]Category, error)

	// FindAll returns all categories ordered by position asc, then name asc.
	FindAll(ctx context.Context) ([]Category, error)

	// Create persists a new category.
	Create(ctx context.Context, c *Category) error

	// Update persists changes to an existing category.
	Update(ctx context.Context, c *Category) error
}
