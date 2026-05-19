package catalog

import "context"

// ProductCategoryAssignmentRepository manages explicit product-category links.
type ProductCategoryAssignmentRepository interface {
	AssignCategory(ctx context.Context, productID, categoryID string) error
	RemoveCategory(ctx context.Context, productID, categoryID string) error
	ListCategoryIDsByProduct(ctx context.Context, productID string) ([]string, error)
}
