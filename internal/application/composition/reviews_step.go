package composition

import (
	"fmt"

	reviewsApp "github.com/akarso/shopanda/internal/application/reviews"
)

// ReviewsStep adds a product_reviews block to the PDP when approved reviews exist.
type ReviewsStep struct {
	reviews *reviewsApp.Service
}

// NewReviewsStep creates a ReviewsStep.
func NewReviewsStep(reviews *reviewsApp.Service) *ReviewsStep {
	if reviews == nil {
		panic("composition.NewReviewsStep: nil reviews service")
	}
	return &ReviewsStep{reviews: reviews}
}

func (s *ReviewsStep) Name() string { return "product_reviews" }

func (s *ReviewsStep) Apply(ctx *ProductContext) error {
	if ctx == nil || ctx.Product == nil {
		return nil
	}

	data, err := s.reviews.DisplayForProduct(ctx.Ctx, ctx.Product.ID)
	if err != nil {
		return fmt.Errorf("product reviews: %w", err)
	}
	if data.Summary.ReviewCount == 0 {
		return nil
	}

	reviewItems := make([]map[string]interface{}, 0, len(data.Reviews))
	for _, rev := range data.Reviews {
		reviewItems = append(reviewItems, map[string]interface{}{
			"id":             rev.ID,
			"rating":         rev.Rating,
			"title":          rev.Title,
			"body":           rev.Body,
			"reviewer_name":  rev.ReviewerName,
			"created_at":     rev.CreatedAt.UTC().Format("2006-01-02"),
		})
	}

	ctx.Blocks = append(ctx.Blocks, Block{
		Type: "product_reviews",
		Data: map[string]interface{}{
			"average_rating": data.Summary.AverageRating,
			"review_count":   data.Summary.ReviewCount,
			"reviews":        reviewItems,
		},
	})
	return nil
}
