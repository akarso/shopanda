package review

import (
	"errors"
	"strings"
	"time"
)

// Status represents moderation state of a product review.
type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

// IsValid returns true when s is a known review status.
func (s Status) IsValid() bool {
	switch s {
	case StatusPending, StatusApproved, StatusRejected:
		return true
	}
	return false
}

// Review is a customer product review awaiting or after moderation.
type Review struct {
	ID           string
	ProductID    string
	CustomerID   string
	Rating       int
	Title        string
	Body         string
	status       Status
	ReviewerName string // populated on read via customer join; not persisted
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewReview creates a review in pending status.
func NewReview(id, productID, customerID string, rating int, title, body string) (Review, error) {
	if id == "" {
		return Review{}, errors.New("review: id must not be empty")
	}
	if productID == "" {
		return Review{}, errors.New("review: product id must not be empty")
	}
	if customerID == "" {
		return Review{}, errors.New("review: customer id must not be empty")
	}
	if rating < 1 || rating > 5 {
		return Review{}, errors.New("review: rating must be between 1 and 5")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Review{}, errors.New("review: body must not be empty")
	}

	now := time.Now().UTC()
	return Review{
		ID:         id,
		ProductID:  productID,
		CustomerID: customerID,
		Rating:     rating,
		Title:      strings.TrimSpace(title),
		Body:       body,
		status:     StatusPending,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

// Status returns the current review status.
func (r Review) Status() Status {
	return r.status
}

// Approve transitions pending → approved.
func (r *Review) Approve() error {
	if r.status != StatusPending {
		return errors.New("review: can only approve a pending review")
	}
	r.status = StatusApproved
	r.touch()
	return nil
}

// Reject transitions pending → rejected.
func (r *Review) Reject() error {
	if r.status != StatusPending {
		return errors.New("review: can only reject a pending review")
	}
	r.status = StatusRejected
	r.touch()
	return nil
}

// SetStatusFromDB restores status when loading from persistence.
func (r *Review) SetStatusFromDB(s string) error {
	status := Status(s)
	if !status.IsValid() {
		return errors.New("review: invalid status from db: " + s)
	}
	r.status = status
	return nil
}

func (r *Review) touch() {
	r.UpdatedAt = time.Now().UTC()
}
