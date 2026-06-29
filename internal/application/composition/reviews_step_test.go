package composition_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	reviewsApp "github.com/akarso/shopanda/internal/application/reviews"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainReview "github.com/akarso/shopanda/internal/domain/review"
)

type reviewsDisplayRepo struct {
	domainReview.Repository
	summary domainReview.ProductSummary
	reviews []domainReview.Review
}

func (r *reviewsDisplayRepo) SummaryByProduct(_ context.Context, _ string) (domainReview.ProductSummary, error) {
	return r.summary, nil
}

func (r *reviewsDisplayRepo) ListApprovedByProduct(_ context.Context, _ string, _, _ int) ([]domainReview.Review, error) {
	return r.reviews, nil
}

func TestReviewsStep_AppendsBlockWhenReviewsExist(t *testing.T) {
	repo := &reviewsDisplayRepo{
		summary: domainReview.ProductSummary{AverageRating: 4.5, ReviewCount: 1},
		reviews: []domainReview.Review{{
			ID:           "r1",
			Rating:       5,
			Body:         "Great",
			ReviewerName: "Alex",
		}},
	}
	svc := reviewsApp.NewService(repo, &stubReviewsProductRepo{})
	step := composition.NewReviewsStep(svc)

	pctx := composition.NewProductContext(&catalog.Product{ID: "p1"})
	pctx.Ctx = context.Background()
	if err := step.Apply(pctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(pctx.Blocks) != 1 || pctx.Blocks[0].Type != "product_reviews" {
		t.Fatalf("blocks = %+v, want product_reviews block", pctx.Blocks)
	}
}

func TestReviewsStep_SkipsWhenNoReviews(t *testing.T) {
	repo := &reviewsDisplayRepo{summary: domainReview.ProductSummary{}}
	svc := reviewsApp.NewService(repo, &stubReviewsProductRepo{})
	step := composition.NewReviewsStep(svc)

	pctx := composition.NewProductContext(&catalog.Product{ID: "p1"})
	pctx.Ctx = context.Background()
	if err := step.Apply(pctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(pctx.Blocks) != 0 {
		t.Fatalf("expected no blocks, got %+v", pctx.Blocks)
	}
}

type stubReviewsProductRepo struct {
	catalog.ProductRepository
}

func (s *stubReviewsProductRepo) FindByID(_ context.Context, _ string) (*catalog.Product, error) {
	return nil, nil
}
