package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/lib/pq"
)

// Compile-time check that VariantRepo implements catalog.VariantRepository.
var _ catalog.VariantRepository = (*VariantRepo)(nil)

// VariantRepo implements catalog.VariantRepository using PostgreSQL.
type VariantRepo struct {
	db *sql.DB
}

// NewVariantRepo returns a new VariantRepo backed by db.
func NewVariantRepo(db *sql.DB) *VariantRepo {
	return &VariantRepo{db: db}
}

// FindByID returns a variant by its ID.
// Returns (nil, nil) when the variant does not exist.
func (r *VariantRepo) FindByID(ctx context.Context, id string) (*catalog.Variant, error) {
	const q = `SELECT id, product_id, sku, name, attributes, created_at, updated_at
		FROM variants WHERE id = $1`

	v, err := r.scanVariant(r.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("variant_repo: find by id: %w", err)
	}
	return v, nil
}

// FindBySKU returns a variant by its SKU.
// Returns (nil, nil) when no variant matches the SKU.
func (r *VariantRepo) FindBySKU(ctx context.Context, sku string) (*catalog.Variant, error) {
	const q = `SELECT id, product_id, sku, name, attributes, created_at, updated_at
		FROM variants WHERE sku = $1`

	v, err := r.scanVariant(r.db.QueryRowContext(ctx, q, sku))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("variant_repo: find by sku: %w", err)
	}
	return v, nil
}

const maxVariantListLimit = 100

// ListByProductID returns variants for the given product ordered by created_at asc.
func (r *VariantRepo) ListByProductID(ctx context.Context, productID string, offset, limit int) ([]catalog.Variant, error) {
	if offset < 0 {
		return nil, apperror.Validation("offset must be >= 0")
	}
	if limit <= 0 {
		return nil, apperror.Validation("limit must be > 0")
	}
	if limit > maxVariantListLimit {
		limit = maxVariantListLimit
	}

	const q = `SELECT id, product_id, sku, name, attributes, created_at, updated_at
		FROM variants WHERE product_id = $1 ORDER BY created_at ASC LIMIT $2 OFFSET $3`

	rows, err := r.db.QueryContext(ctx, q, productID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("variant_repo: list by product: %w", err)
	}
	defer rows.Close()

	var variants []catalog.Variant
	for rows.Next() {
		v, err := r.scanVariant(rows)
		if err != nil {
			return nil, fmt.Errorf("variant_repo: list scan: %w", err)
		}
		variants = append(variants, *v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("variant_repo: list rows: %w", err)
	}
	return variants, nil
}

// skuConflict returns an apperror.Conflict when err is a unique-constraint
// violation on the variants_sku_key index; otherwise it returns nil.
func skuConflict(err error) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" && pqErr.Constraint == "variants_sku_key" {
		return apperror.Conflict("variant with this sku already exists")
	}
	return nil
}

// Create persists a new variant.
func (r *VariantRepo) Create(ctx context.Context, v *catalog.Variant) error {
	if v == nil {
		return fmt.Errorf("variant_repo: create: variant must not be nil")
	}

	attrs, err := json.Marshal(v.Attributes)
	if err != nil {
		return fmt.Errorf("variant_repo: marshal attributes: %w", err)
	}

	const q = `INSERT INTO variants (id, product_id, sku, name, attributes, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`

	_, err = r.db.ExecContext(ctx, q,
		v.ID, v.ProductID, v.SKU, v.Name,
		attrs, v.CreatedAt, v.UpdatedAt,
	)
	if err != nil {
		if ce := skuConflict(err); ce != nil {
			return ce
		}
		return fmt.Errorf("variant_repo: create: %w", err)
	}
	return nil
}

// Update persists changes to an existing variant.
func (r *VariantRepo) Update(ctx context.Context, v *catalog.Variant) error {
	if v == nil {
		return fmt.Errorf("variant_repo: update: variant must not be nil")
	}

	attrs, err := json.Marshal(v.Attributes)
	if err != nil {
		return fmt.Errorf("variant_repo: marshal attributes: %w", err)
	}

	updatedAt := time.Now().UTC()

	const q = `UPDATE variants
		SET sku = $1, name = $2, attributes = $3, updated_at = $4
		WHERE id = $5`

	result, err := r.db.ExecContext(ctx, q,
		v.SKU, v.Name, attrs, updatedAt, v.ID,
	)
	if err != nil {
		if ce := skuConflict(err); ce != nil {
			return ce
		}
		return fmt.Errorf("variant_repo: update: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("variant_repo: update rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("variant not found")
	}
	v.UpdatedAt = updatedAt
	return nil
}

// scanVariant reads a variant from a row scanner.
func (r *VariantRepo) scanVariant(s scanner) (*catalog.Variant, error) {
	var v catalog.Variant
	var attrsJSON []byte

	err := s.Scan(
		&v.ID, &v.ProductID, &v.SKU, &v.Name,
		&attrsJSON, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if len(attrsJSON) > 0 {
		if err := json.Unmarshal(attrsJSON, &v.Attributes); err != nil {
			return nil, fmt.Errorf("unmarshal attributes: %w", err)
		}
	}
	if v.Attributes == nil {
		v.Attributes = make(map[string]interface{})
	}

	return &v, nil
}
