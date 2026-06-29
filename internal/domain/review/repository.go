package review

import "context"

// ProductSummary aggregates approved review stats for a product.
type ProductSummary struct {
	AverageRating float64
	ReviewCount   int
}

// Repository defines persistence for product reviews.
type Repository interface {
	// Save inserts a new review.
	Save(ctx context.Context, review *Review) error

	// FindByID returns a review by ID. Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Review, error)

	// FindByProductAndCustomer returns an existing review for the pair.
	// Returns (nil, nil) when not found.
	FindByProductAndCustomer(ctx context.Context, productID, customerID string) (*Review, error)

	// ListApprovedByProduct returns approved reviews for a product, newest first.
	ListApprovedByProduct(ctx context.Context, productID string, offset, limit int) ([]Review, error)

	// SummaryByProduct returns aggregate stats for approved reviews on a product.
	SummaryByProduct(ctx context.Context, productID string) (ProductSummary, error)

	// List returns reviews ordered by created_at desc with optional status filter.
	List(ctx context.Context, status Status, offset, limit int) ([]Review, error)

	// Update persists moderation transitions.
	Update(ctx context.Context, review *Review) error
}
