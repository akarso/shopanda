package review_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/review"
)

func TestNewReview_Valid(t *testing.T) {
	rev, err := review.NewReview("r1", "p1", "c1", 5, "Great", "Loved it")
	if err != nil {
		t.Fatalf("NewReview: %v", err)
	}
	if rev.Status() != review.StatusPending {
		t.Errorf("status = %q, want pending", rev.Status())
	}
	if rev.Title != "Great" {
		t.Errorf("title = %q", rev.Title)
	}
}

func TestNewReview_InvalidRating(t *testing.T) {
	_, err := review.NewReview("r1", "p1", "c1", 0, "", "body")
	if err == nil {
		t.Fatal("expected error for rating 0")
	}
}

func TestReview_ApproveReject(t *testing.T) {
	rev, err := review.NewReview("r1", "p1", "c1", 4, "", "Solid product")
	if err != nil {
		t.Fatalf("NewReview: %v", err)
	}
	if err := rev.Approve(); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if rev.Status() != review.StatusApproved {
		t.Errorf("status = %q, want approved", rev.Status())
	}
	if err := rev.Reject(); err == nil {
		t.Fatal("expected reject after approve to fail")
	}
}
