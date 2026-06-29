package reviews

import (
	"context"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
	domainReview "github.com/akarso/shopanda/internal/domain/review"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

const defaultListLimit = 20
const defaultDisplayLimit = 10

// Service orchestrates product review submission and moderation.
type Service struct {
	reviews  domainReview.Repository
	products catalog.ProductRepository
}

// NewService creates a review service.
func NewService(reviews domainReview.Repository, products catalog.ProductRepository) *Service {
	if reviews == nil {
		panic("reviews.NewService: nil reviews repository")
	}
	if products == nil {
		panic("reviews.NewService: nil products repository")
	}
	return &Service{reviews: reviews, products: products}
}

// SubmitInput is customer review submission data.
type SubmitInput struct {
	Rating int
	Title  string
	Body   string
}

// Submit creates a pending review for an authenticated customer.
func (s *Service) Submit(ctx context.Context, productID, customerID string, in SubmitInput) (*domainReview.Review, error) {
	if customerID == "" {
		return nil, apperror.Validation("customer id must not be empty")
	}
	product, err := s.loadProduct(ctx, productID)
	if err != nil {
		return nil, err
	}
	if product.Status != catalog.StatusActive {
		return nil, apperror.Validation("reviews are only allowed for active products")
	}

	existing, err := s.reviews.FindByProductAndCustomer(ctx, productID, customerID)
	if err != nil {
		return nil, fmt.Errorf("reviews: find existing: %w", err)
	}
	if existing != nil {
		return nil, apperror.Validation("you have already reviewed this product")
	}

	rev, err := domainReview.NewReview(id.New(), productID, customerID, in.Rating, in.Title, in.Body)
	if err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.reviews.Save(ctx, &rev); err != nil {
		if strings.Contains(err.Error(), "duplicate review") {
			return nil, apperror.Validation("you have already reviewed this product")
		}
		return nil, fmt.Errorf("reviews: save: %w", err)
	}
	return &rev, nil
}

// Approve transitions a pending review to approved.
func (s *Service) Approve(ctx context.Context, reviewID string) (*domainReview.Review, error) {
	rev, err := s.loadReview(ctx, reviewID)
	if err != nil {
		return nil, err
	}
	if err := rev.Approve(); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.reviews.Update(ctx, rev); err != nil {
		return nil, fmt.Errorf("reviews: approve update: %w", err)
	}
	return rev, nil
}

// Reject transitions a pending review to rejected.
func (s *Service) Reject(ctx context.Context, reviewID string) (*domainReview.Review, error) {
	rev, err := s.loadReview(ctx, reviewID)
	if err != nil {
		return nil, err
	}
	if err := rev.Reject(); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if err := s.reviews.Update(ctx, rev); err != nil {
		return nil, fmt.Errorf("reviews: reject update: %w", err)
	}
	return rev, nil
}

// Get returns a review by ID.
func (s *Service) Get(ctx context.Context, reviewID string) (*domainReview.Review, error) {
	return s.loadReview(ctx, reviewID)
}

// List returns reviews with optional status filter.
func (s *Service) List(ctx context.Context, status domainReview.Status, offset, limit int) ([]domainReview.Review, error) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	return s.reviews.List(ctx, status, offset, limit)
}

// ListApprovedByProduct returns approved reviews for public display.
func (s *Service) ListApprovedByProduct(ctx context.Context, productID string, offset, limit int) ([]domainReview.Review, error) {
	if _, err := s.loadProduct(ctx, productID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultListLimit
	}
	return s.reviews.ListApprovedByProduct(ctx, productID, offset, limit)
}

// DisplayData holds approved review summary and recent entries for PDP rendering.
type DisplayData struct {
	Summary domainReview.ProductSummary
	Reviews []domainReview.Review
}

// DisplayForProduct returns summary and recent approved reviews for a product.
func (s *Service) DisplayForProduct(ctx context.Context, productID string) (DisplayData, error) {
	summary, err := s.reviews.SummaryByProduct(ctx, productID)
	if err != nil {
		return DisplayData{}, fmt.Errorf("reviews: summary: %w", err)
	}
	if summary.ReviewCount == 0 {
		return DisplayData{Summary: summary}, nil
	}
	reviews, err := s.reviews.ListApprovedByProduct(ctx, productID, 0, defaultDisplayLimit)
	if err != nil {
		return DisplayData{}, fmt.Errorf("reviews: list approved: %w", err)
	}
	return DisplayData{Summary: summary, Reviews: reviews}, nil
}

func (s *Service) loadReview(ctx context.Context, reviewID string) (*domainReview.Review, error) {
	if reviewID == "" {
		return nil, apperror.Validation("review id must not be empty")
	}
	rev, err := s.reviews.FindByID(ctx, reviewID)
	if err != nil {
		return nil, fmt.Errorf("reviews: find: %w", err)
	}
	if rev == nil {
		return nil, apperror.NotFound("review not found")
	}
	return rev, nil
}

func (s *Service) loadProduct(ctx context.Context, productID string) (*catalog.Product, error) {
	if productID == "" {
		return nil, apperror.Validation("product id must not be empty")
	}
	product, err := s.products.FindByID(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("reviews: find product: %w", err)
	}
	if product == nil {
		return nil, apperror.NotFound("product not found")
	}
	return product, nil
}
