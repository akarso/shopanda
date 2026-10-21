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

// Compile-time check that BundleComponentRepo implements catalog.BundleComponentRepository.
var _ catalog.BundleComponentRepository = (*BundleComponentRepo)(nil)

// BundleComponentRepo implements catalog.BundleComponentRepository using PostgreSQL.
type BundleComponentRepo struct {
	db *sql.DB
	tx *sql.Tx
}

// bundleQueryer is the subset of *sql.DB and *sql.Tx used by this repo.
type bundleQueryer interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// NewBundleComponentRepo returns a new BundleComponentRepo backed by db.
func NewBundleComponentRepo(db *sql.DB) (*BundleComponentRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewBundleComponentRepo: nil *sql.DB")
	}
	return &BundleComponentRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
func (r *BundleComponentRepo) WithTx(tx *sql.Tx) catalog.BundleComponentRepository {
	return &BundleComponentRepo{db: r.db, tx: tx}
}

func (r *BundleComponentRepo) queryer() bundleQueryer {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

func mapBundleComponentExecError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23503":
		return apperror.NotFound("product not found")
	case "23514":
		return apperror.Validation("bundle component constraint violation")
	case "22P02":
		return apperror.Validation("invalid product id")
	default:
		return err
	}
}

// ListByBundleID returns components for bundleProductID ordered by created_at,
// then component_product_id.
func (r *BundleComponentRepo) ListByBundleID(ctx context.Context, bundleProductID string) ([]catalog.BundleComponent, error) {
	if bundleProductID == "" {
		return nil, fmt.Errorf("bundle_component_repo: list: empty bundle product id")
	}
	const q = `SELECT bundle_product_id, component_product_id, quantity, required, created_at
		FROM bundle_components
		WHERE bundle_product_id = $1
		ORDER BY created_at ASC, component_product_id ASC`

	rows, err := r.queryer().QueryContext(ctx, q, bundleProductID)
	if err != nil {
		if mapped := mapBundleComponentExecError(err); mapped != err {
			return nil, mapped
		}
		return nil, fmt.Errorf("bundle_component_repo: list: %w", err)
	}
	defer rows.Close()

	out := []catalog.BundleComponent{}
	for rows.Next() {
		var c catalog.BundleComponent
		if err := rows.Scan(
			&c.BundleProductID,
			&c.ComponentProductID,
			&c.Quantity,
			&c.Required,
			&c.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("bundle_component_repo: list scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bundle_component_repo: list rows: %w", err)
	}
	return out, nil
}

// CountByBundleID returns how many component rows belong to the bundle product.
func (r *BundleComponentRepo) CountByBundleID(ctx context.Context, bundleProductID string) (int, error) {
	if bundleProductID == "" {
		return 0, fmt.Errorf("bundle_component_repo: count by bundle: empty bundle product id")
	}
	const q = `SELECT COUNT(*) FROM bundle_components WHERE bundle_product_id = $1`
	var n int
	if err := r.queryer().QueryRowContext(ctx, q, bundleProductID).Scan(&n); err != nil {
		if mapped := mapBundleComponentExecError(err); mapped != err {
			return 0, mapped
		}
		return 0, fmt.Errorf("bundle_component_repo: count by bundle: %w", err)
	}
	return n, nil
}

// CountByComponentID returns how many bundles reference the product as a component.
func (r *BundleComponentRepo) CountByComponentID(ctx context.Context, componentProductID string) (int, error) {
	if componentProductID == "" {
		return 0, fmt.Errorf("bundle_component_repo: count by component: empty component product id")
	}
	const q = `SELECT COUNT(*) FROM bundle_components WHERE component_product_id = $1`
	var n int
	if err := r.queryer().QueryRowContext(ctx, q, componentProductID).Scan(&n); err != nil {
		if mapped := mapBundleComponentExecError(err); mapped != err {
			return 0, mapped
		}
		return 0, fmt.Errorf("bundle_component_repo: count by component: %w", err)
	}
	return n, nil
}

// Upsert inserts or updates quantity and required for the composite key.
func (r *BundleComponentRepo) Upsert(ctx context.Context, c *catalog.BundleComponent) error {
	if c == nil {
		return fmt.Errorf("bundle_component_repo: upsert: component must not be nil")
	}
	if err := c.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	const q = `INSERT INTO bundle_components (bundle_product_id, component_product_id, quantity, required)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (bundle_product_id, component_product_id) DO UPDATE
		SET quantity = EXCLUDED.quantity,
		    required = EXCLUDED.required`

	_, err := r.queryer().ExecContext(ctx, q,
		c.BundleProductID,
		c.ComponentProductID,
		c.Quantity,
		c.Required,
	)
	if err != nil {
		if mapped := mapBundleComponentExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("bundle_component_repo: upsert: %w", err)
	}
	return nil
}

// Remove deletes one bundle-component link.
func (r *BundleComponentRepo) Remove(ctx context.Context, bundleProductID, componentProductID string) error {
	if bundleProductID == "" {
		return fmt.Errorf("bundle_component_repo: remove: empty bundle product id")
	}
	if componentProductID == "" {
		return fmt.Errorf("bundle_component_repo: remove: empty component product id")
	}
	const q = `DELETE FROM bundle_components
		WHERE bundle_product_id = $1 AND component_product_id = $2`

	res, err := r.queryer().ExecContext(ctx, q, bundleProductID, componentProductID)
	if err != nil {
		if mapped := mapBundleComponentExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("bundle_component_repo: remove: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("bundle_component_repo: remove rows affected: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("bundle component not found")
	}
	return nil
}
