package reviews_test

import (
	"context"
	"testing"

	reviewsApp "github.com/akarso/shopanda/internal/application/reviews"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainReview "github.com/akarso/shopanda/internal/domain/review"
)

type stubReviewRepo struct {
	domainReview.Repository
	saved   *domainReview.Review
	byID    map[string]*domainReview.Review
	byPair  map[string]*domainReview.Review
	updated *domainReview.Review
}

func (s *stubReviewRepo) Save(_ context.Context, rev *domainReview.Review) error {
	cp := *rev
	s.saved = &cp
	return nil
}

func (s *stubReviewRepo) FindByID(_ context.Context, id string) (*domainReview.Review, error) {
	if s.byID == nil {
		return nil, nil
	}
	return s.byID[id], nil
}

func (s *stubReviewRepo) FindByProductAndCustomer(_ context.Context, productID, customerID string) (*domainReview.Review, error) {
	if s.byPair == nil {
		return nil, nil
	}
	return s.byPair[productID+":"+customerID], nil
}

func (s *stubReviewRepo) Update(_ context.Context, rev *domainReview.Review) error {
	cp := *rev
	s.updated = &cp
	return nil
}

type stubProductRepo struct {
	catalog.ProductRepository
	product *catalog.Product
}

func (s *stubProductRepo) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	if s.product != nil && s.product.ID == id {
		return s.product, nil
	}
	return nil, nil
}

func TestService_Submit_CreatesPendingReview(t *testing.T) {
	product := &catalog.Product{ID: "p1", Status: catalog.StatusActive}
	reviews := &stubReviewRepo{}
	products := &stubProductRepo{product: product}
	svc := reviewsApp.NewService(reviews, products)

	rev, err := svc.Submit(context.Background(), "p1", "c1", reviewsApp.SubmitInput{
		Rating: 5,
		Title:  "Nice",
		Body:   "Works well",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if rev.Status() != domainReview.StatusPending {
		t.Errorf("status = %q, want pending", rev.Status())
	}
	if reviews.saved == nil {
		t.Fatal("expected review saved")
	}
}

func TestService_Approve_FromPending(t *testing.T) {
	rev, err := domainReview.NewReview("r1", "p1", "c1", 5, "", "Good")
	if err != nil {
		t.Fatalf("NewReview: %v", err)
	}
	reviews := &stubReviewRepo{byID: map[string]*domainReview.Review{"r1": &rev}}
	products := &stubProductRepo{}
	svc := reviewsApp.NewService(reviews, products)

	out, err := svc.Approve(context.Background(), "r1")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if out.Status() != domainReview.StatusApproved {
		t.Errorf("status = %q, want approved", out.Status())
	}
}

func TestService_Submit_RejectsDuplicate(t *testing.T) {
	existing, err := domainReview.NewReview("r1", "p1", "c1", 4, "", "Already reviewed")
	if err != nil {
		t.Fatalf("NewReview: %v", err)
	}
	reviews := &stubReviewRepo{byPair: map[string]*domainReview.Review{"p1:c1": &existing}}
	products := &stubProductRepo{product: &catalog.Product{ID: "p1", Status: catalog.StatusActive}}
	svc := reviewsApp.NewService(reviews, products)

	_, err = svc.Submit(context.Background(), "p1", "c1", reviewsApp.SubmitInput{Rating: 5, Body: "Again"})
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}
