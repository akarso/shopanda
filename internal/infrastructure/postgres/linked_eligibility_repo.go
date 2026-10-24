package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

// Compile-time check that LinkedEligibilityRepo implements catalog.LinkedEligibilityRepository.
var _ catalog.LinkedEligibilityRepository = (*LinkedEligibilityRepo)(nil)

// LinkedEligibilityRepo implements catalog.LinkedEligibilityRepository using PostgreSQL.
// Eligibility rows are catalog-time rules only; they do not affect Purchasable or visibility.
//
// WithTx is adapter-only. The domain LinkedEligibilityRepository port does not declare it.
type LinkedEligibilityRepo struct {
	db *sql.DB
	tx *sql.Tx
}

// linkedEligibilityQueryer is the subset of *sql.DB and *sql.Tx used by this repo.
type linkedEligibilityQueryer interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// NewLinkedEligibilityRepo returns a new LinkedEligibilityRepo backed by db.
func NewLinkedEligibilityRepo(db *sql.DB) (*LinkedEligibilityRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewLinkedEligibilityRepo: nil *sql.DB")
	}
	return &LinkedEligibilityRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
// This method is postgres-only; callers type-assert the concrete adapter.
func (r *LinkedEligibilityRepo) WithTx(tx *sql.Tx) catalog.LinkedEligibilityRepository {
	return &LinkedEligibilityRepo{db: r.db, tx: tx}
}

func (r *LinkedEligibilityRepo) queryer() linkedEligibilityQueryer {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

func mapLinkedEligibilityExecError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23503":
		return apperror.NotFound("not found")
	case "23514":
		return apperror.Validation("linked eligibility constraint violation")
	case "22P02":
		return apperror.Validation("invalid product id")
	default:
		return err
	}
}

// ListParentsByChildID returns allowlist rows for childProductID ordered by
// created_at, then parent_product_id. The slice is non-nil when the query succeeds.
func (r *LinkedEligibilityRepo) ListParentsByChildID(ctx context.Context, childProductID string) ([]catalog.LinkedEligibleParent, error) {
	if childProductID == "" {
		return nil, fmt.Errorf("linked_eligibility_repo: list parents: empty child product id")
	}
	const q = `SELECT child_product_id, parent_product_id, created_at
		FROM linked_eligible_parents
		WHERE child_product_id = $1
		ORDER BY created_at ASC, parent_product_id ASC`

	rows, err := r.queryer().QueryContext(ctx, q, childProductID)
	if err != nil {
		if mapped := mapLinkedEligibilityExecError(err); mapped != err {
			return nil, mapped
		}
		return nil, fmt.Errorf("linked_eligibility_repo: list parents: %w", err)
	}
	defer rows.Close()

	out := []catalog.LinkedEligibleParent{}
	for rows.Next() {
		var p catalog.LinkedEligibleParent
		if err := rows.Scan(&p.ChildProductID, &p.ParentProductID, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("linked_eligibility_repo: list parents scan: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("linked_eligibility_repo: list parents rows: %w", err)
	}
	return out, nil
}

// ListCategoriesByChildID returns category rules for childProductID ordered by
// created_at, then category_id. The slice is non-nil when the query succeeds.
func (r *LinkedEligibilityRepo) ListCategoriesByChildID(ctx context.Context, childProductID string) ([]catalog.LinkedEligibleCategory, error) {
	if childProductID == "" {
		return nil, fmt.Errorf("linked_eligibility_repo: list categories: empty child product id")
	}
	const q = `SELECT child_product_id, category_id, created_at
		FROM linked_eligible_categories
		WHERE child_product_id = $1
		ORDER BY created_at ASC, category_id ASC`

	rows, err := r.queryer().QueryContext(ctx, q, childProductID)
	if err != nil {
		if mapped := mapLinkedEligibilityExecError(err); mapped != err {
			return nil, mapped
		}
		return nil, fmt.Errorf("linked_eligibility_repo: list categories: %w", err)
	}
	defer rows.Close()

	out := []catalog.LinkedEligibleCategory{}
	for rows.Next() {
		var c catalog.LinkedEligibleCategory
		if err := rows.Scan(&c.ChildProductID, &c.CategoryID, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("linked_eligibility_repo: list categories scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("linked_eligibility_repo: list categories rows: %w", err)
	}
	return out, nil
}

// CountRulesByChildID returns allowlist rows plus category rules for the child.
func (r *LinkedEligibilityRepo) CountRulesByChildID(ctx context.Context, childProductID string) (int, error) {
	if childProductID == "" {
		return 0, fmt.Errorf("linked_eligibility_repo: count rules: empty child product id")
	}
	const q = `SELECT
		(SELECT COUNT(*) FROM linked_eligible_parents WHERE child_product_id = $1) +
		(SELECT COUNT(*) FROM linked_eligible_categories WHERE child_product_id = $1)`
	var n int
	if err := r.queryer().QueryRowContext(ctx, q, childProductID).Scan(&n); err != nil {
		if mapped := mapLinkedEligibilityExecError(err); mapped != err {
			return 0, mapped
		}
		return 0, fmt.Errorf("linked_eligibility_repo: count rules: %w", err)
	}
	return n, nil
}

// InsertParent adds an allowlist row. ON CONFLICT DO NOTHING makes a duplicate a no-op.
func (r *LinkedEligibilityRepo) InsertParent(ctx context.Context, p *catalog.LinkedEligibleParent) error {
	if p == nil {
		return fmt.Errorf("linked_eligibility_repo: insert parent: parent must not be nil")
	}
	if err := p.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	const q = `INSERT INTO linked_eligible_parents (child_product_id, parent_product_id)
		VALUES ($1, $2)
		ON CONFLICT (child_product_id, parent_product_id) DO NOTHING`

	_, err := r.queryer().ExecContext(ctx, q, p.ChildProductID, p.ParentProductID)
	if err != nil {
		if mapped := mapLinkedEligibilityExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("linked_eligibility_repo: insert parent: %w", err)
	}
	return nil
}

// InsertCategory adds a category rule. ON CONFLICT DO NOTHING makes a duplicate a no-op.
func (r *LinkedEligibilityRepo) InsertCategory(ctx context.Context, c *catalog.LinkedEligibleCategory) error {
	if c == nil {
		return fmt.Errorf("linked_eligibility_repo: insert category: category must not be nil")
	}
	if err := c.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	const q = `INSERT INTO linked_eligible_categories (child_product_id, category_id)
		VALUES ($1, $2)
		ON CONFLICT (child_product_id, category_id) DO NOTHING`

	_, err := r.queryer().ExecContext(ctx, q, c.ChildProductID, c.CategoryID)
	if err != nil {
		if mapped := mapLinkedEligibilityExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("linked_eligibility_repo: insert category: %w", err)
	}
	return nil
}

// RemoveParent deletes one allowlist row.
func (r *LinkedEligibilityRepo) RemoveParent(ctx context.Context, childProductID, parentProductID string) error {
	if childProductID == "" {
		return fmt.Errorf("linked_eligibility_repo: remove parent: empty child product id")
	}
	if parentProductID == "" {
		return fmt.Errorf("linked_eligibility_repo: remove parent: empty parent product id")
	}
	const q = `DELETE FROM linked_eligible_parents
		WHERE child_product_id = $1 AND parent_product_id = $2`

	res, err := r.queryer().ExecContext(ctx, q, childProductID, parentProductID)
	if err != nil {
		if mapped := mapLinkedEligibilityExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("linked_eligibility_repo: remove parent: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("linked_eligibility_repo: remove parent rows affected: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("linked eligible parent not found")
	}
	return nil
}

// RemoveCategory deletes one category rule.
func (r *LinkedEligibilityRepo) RemoveCategory(ctx context.Context, childProductID, categoryID string) error {
	if childProductID == "" {
		return fmt.Errorf("linked_eligibility_repo: remove category: empty child product id")
	}
	if categoryID == "" {
		return fmt.Errorf("linked_eligibility_repo: remove category: empty category id")
	}
	const q = `DELETE FROM linked_eligible_categories
		WHERE child_product_id = $1 AND category_id = $2`

	res, err := r.queryer().ExecContext(ctx, q, childProductID, categoryID)
	if err != nil {
		if mapped := mapLinkedEligibilityExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("linked_eligibility_repo: remove category: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("linked_eligibility_repo: remove category rows affected: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("linked eligible category not found")
	}
	return nil
}

// IsEligible is true when an explicit parent row exists or the parent product
// is assigned to any of the child's eligible categories. Product status is
// not consulted.
func (r *LinkedEligibilityRepo) IsEligible(ctx context.Context, childProductID, parentProductID string) (bool, error) {
	if childProductID == "" {
		return false, fmt.Errorf("linked_eligibility_repo: is eligible: empty child product id")
	}
	if parentProductID == "" {
		return false, fmt.Errorf("linked_eligibility_repo: is eligible: empty parent product id")
	}
	if childProductID == parentProductID {
		return false, nil
	}
	const q = `SELECT EXISTS (
		SELECT 1 FROM linked_eligible_parents
		WHERE child_product_id = $1 AND parent_product_id = $2
	) OR EXISTS (
		SELECT 1 FROM linked_eligible_categories lec
		JOIN product_categories pc ON pc.category_id = lec.category_id
		WHERE lec.child_product_id = $1 AND pc.product_id = $2
	)`
	var ok bool
	if err := r.queryer().QueryRowContext(ctx, q, childProductID, parentProductID).Scan(&ok); err != nil {
		if mapped := mapLinkedEligibilityExecError(err); mapped != err {
			return false, mapped
		}
		return false, fmt.Errorf("linked_eligibility_repo: is eligible: %w", err)
	}
	return ok, nil
}
