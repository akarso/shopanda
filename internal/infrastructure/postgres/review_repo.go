package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	domainReview "github.com/akarso/shopanda/internal/domain/review"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/lib/pq"
)

var _ domainReview.Repository = (*ReviewRepo)(nil)

// ReviewRepo implements review.Repository using PostgreSQL.
type ReviewRepo struct {
	db *sql.DB
}

// NewReviewRepo returns a new ReviewRepo backed by db.
func NewReviewRepo(db *sql.DB) (*ReviewRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewReviewRepo: nil *sql.DB")
	}
	return &ReviewRepo{db: db}, nil
}

const reviewCols = `r.id, r.product_id, r.customer_id, r.rating, r.title, r.body, r.status, r.created_at, r.updated_at`

type reviewScanner interface {
	Scan(dest ...interface{}) error
}

func hydrateReview(s reviewScanner) (*domainReview.Review, error) {
	var rev domainReview.Review
	var status string
	err := s.Scan(
		&rev.ID, &rev.ProductID, &rev.CustomerID, &rev.Rating, &rev.Title, &rev.Body,
		&status, &rev.CreatedAt, &rev.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if err := rev.SetStatusFromDB(status); err != nil {
		return nil, err
	}
	return &rev, nil
}

func hydrateReviewWithName(s reviewScanner) (*domainReview.Review, error) {
	var rev domainReview.Review
	var status string
	err := s.Scan(
		&rev.ID, &rev.ProductID, &rev.CustomerID, &rev.Rating, &rev.Title, &rev.Body,
		&status, &rev.CreatedAt, &rev.UpdatedAt, &rev.ReviewerName,
	)
	if err != nil {
		return nil, err
	}
	if err := rev.SetStatusFromDB(status); err != nil {
		return nil, err
	}
	return &rev, nil
}

// Save inserts a new review.
func (r *ReviewRepo) Save(ctx context.Context, rev *domainReview.Review) error {
	if rev == nil {
		return fmt.Errorf("review_repo: save: review must not be nil")
	}
	const q = `INSERT INTO product_reviews
		(id, product_id, customer_id, rating, title, body, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.db.ExecContext(ctx, q,
		rev.ID, rev.ProductID, rev.CustomerID, rev.Rating, rev.Title, rev.Body,
		string(rev.Status()), rev.CreatedAt, rev.UpdatedAt,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" &&
			pqErr.Constraint == "product_reviews_product_id_customer_id_key" {
			return fmt.Errorf("review_repo: duplicate review: %w", err)
		}
		return fmt.Errorf("review_repo: save: %w", err)
	}
	return nil
}

// FindByID returns a review by ID.
func (r *ReviewRepo) FindByID(ctx context.Context, id string) (*domainReview.Review, error) {
	if id == "" {
		return nil, fmt.Errorf("review_repo: find: empty id")
	}
	q := `SELECT ` + reviewCols + ` FROM product_reviews r WHERE r.id = $1`
	rev, err := hydrateReview(r.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("review_repo: find by id: %w", err)
	}
	return rev, nil
}

// FindByProductAndCustomer returns a review for the product/customer pair.
func (r *ReviewRepo) FindByProductAndCustomer(ctx context.Context, productID, customerID string) (*domainReview.Review, error) {
	if productID == "" || customerID == "" {
		return nil, fmt.Errorf("review_repo: find by product and customer: empty id")
	}
	q := `SELECT ` + reviewCols + ` FROM product_reviews r
		WHERE r.product_id = $1 AND r.customer_id = $2`
	rev, err := hydrateReview(r.db.QueryRowContext(ctx, q, productID, customerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("review_repo: find by product and customer: %w", err)
	}
	return rev, nil
}

// ListApprovedByProduct returns approved reviews with reviewer first name.
func (r *ReviewRepo) ListApprovedByProduct(ctx context.Context, productID string, offset, limit int) ([]domainReview.Review, error) {
	if productID == "" {
		return nil, fmt.Errorf("review_repo: list approved: empty product id")
	}
	if limit <= 0 {
		limit = 10
	}
	if offset < 0 {
		return nil, fmt.Errorf("review_repo: list approved: negative offset")
	}
	const q = `SELECT ` + reviewCols + `, COALESCE(NULLIF(TRIM(c.first_name), ''), 'Customer') AS reviewer_name
		FROM product_reviews r
		INNER JOIN customers c ON c.id = r.customer_id
		WHERE r.product_id = $1 AND r.status = 'approved'
		ORDER BY r.created_at DESC
		OFFSET $2 LIMIT $3`
	rows, err := r.db.QueryContext(ctx, q, productID, offset, limit)
	if err != nil {
		return nil, fmt.Errorf("review_repo: list approved: %w", err)
	}
	defer rows.Close()

	var out []domainReview.Review
	for rows.Next() {
		rev, err := hydrateReviewWithName(rows)
		if err != nil {
			return nil, fmt.Errorf("review_repo: list approved scan: %w", err)
		}
		out = append(out, *rev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("review_repo: list approved rows: %w", err)
	}
	return out, nil
}

// SummaryByProduct returns aggregate stats for approved reviews.
func (r *ReviewRepo) SummaryByProduct(ctx context.Context, productID string) (domainReview.ProductSummary, error) {
	if productID == "" {
		return domainReview.ProductSummary{}, fmt.Errorf("review_repo: summary: empty product id")
	}
	const q = `SELECT COALESCE(AVG(rating)::float8, 0), COUNT(*)
		FROM product_reviews
		WHERE product_id = $1 AND status = 'approved'`
	var summary domainReview.ProductSummary
	err := r.db.QueryRowContext(ctx, q, productID).Scan(&summary.AverageRating, &summary.ReviewCount)
	if err != nil {
		return domainReview.ProductSummary{}, fmt.Errorf("review_repo: summary: %w", err)
	}
	return summary, nil
}

// List returns reviews with optional status filter.
func (r *ReviewRepo) List(ctx context.Context, status domainReview.Status, offset, limit int) ([]domainReview.Review, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("review_repo: list: limit must be positive")
	}
	if offset < 0 {
		return nil, fmt.Errorf("review_repo: list: offset must not be negative")
	}

	var (
		rows *sql.Rows
		err  error
	)
	if status != "" {
		if !status.IsValid() {
			return nil, fmt.Errorf("review_repo: list: invalid status %q", status)
		}
		const q = `SELECT ` + reviewCols + ` FROM product_reviews r
			WHERE r.status = $1
			ORDER BY r.created_at DESC
			OFFSET $2 LIMIT $3`
		rows, err = r.db.QueryContext(ctx, q, string(status), offset, limit)
	} else {
		const q = `SELECT ` + reviewCols + ` FROM product_reviews r
			ORDER BY r.created_at DESC
			OFFSET $1 LIMIT $2`
		rows, err = r.db.QueryContext(ctx, q, offset, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("review_repo: list: %w", err)
	}
	defer rows.Close()

	var out []domainReview.Review
	for rows.Next() {
		rev, err := hydrateReview(rows)
		if err != nil {
			return nil, fmt.Errorf("review_repo: list scan: %w", err)
		}
		out = append(out, *rev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("review_repo: list rows: %w", err)
	}
	return out, nil
}

// Update persists moderation transitions when the review still has priorStatus.
func (r *ReviewRepo) Update(ctx context.Context, rev *domainReview.Review, priorStatus domainReview.Status) error {
	if rev == nil {
		return fmt.Errorf("review_repo: update: review must not be nil")
	}
	const q = `UPDATE product_reviews
		SET status = $2, updated_at = $3
		WHERE id = $1 AND status = $4`
	res, err := r.db.ExecContext(ctx, q, rev.ID, string(rev.Status()), rev.UpdatedAt, string(priorStatus))
	if err != nil {
		return fmt.Errorf("review_repo: update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("review_repo: update rows: %w", err)
	}
	if n == 0 {
		return apperror.Conflict("review state changed")
	}
	return nil
}
