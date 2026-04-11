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

const maxListLimit = 100

// Compile-time check that ProductRepo implements catalog.ProductRepository.
var _ catalog.ProductRepository = (*ProductRepo)(nil)

// ProductRepo implements catalog.ProductRepository using PostgreSQL.
type ProductRepo struct {
	db *sql.DB
	tx *sql.Tx
}

// NewProductRepo returns a new ProductRepo backed by db.
func NewProductRepo(db *sql.DB) (*ProductRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewProductRepo: nil *sql.DB")
	}
	return &ProductRepo{db: db, tx: nil}, nil
}

// WithTx returns a repo bound to the given transaction.
func (r *ProductRepo) WithTx(tx *sql.Tx) catalog.ProductRepository {
	return &ProductRepo{db: r.db, tx: tx}
}

// FindByID returns a product by its ID.
// Returns (nil, nil) when the product does not exist.
func (r *ProductRepo) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	const q = `SELECT id, name, slug, description, status, attributes, created_at, updated_at
		FROM products WHERE id = $1`

	var querier interface {
		QueryRowContext(context.Context, string, ...interface{}) *sql.Row
	}
	if r.tx != nil {
		querier = r.tx
	} else {
		querier = r.db
	}
	p, err := r.scanProduct(querier.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("product_repo: find by id: %w", err)
	}
	return p, nil
}

// FindBySlug returns a product by its slug.
// Returns (nil, nil) when no product matches the slug.
func (r *ProductRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Product, error) {
	const q = `SELECT id, name, slug, description, status, attributes, created_at, updated_at
		FROM products WHERE slug = $1`

	var querier interface {
		QueryRowContext(context.Context, string, ...interface{}) *sql.Row
	}
	if r.tx != nil {
		querier = r.tx
	} else {
		querier = r.db
	}
	p, err := r.scanProduct(querier.QueryRowContext(ctx, q, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("product_repo: find by slug: %w", err)
	}
	return p, nil
}

// List returns a page of products ordered by created_at desc.
func (r *ProductRepo) List(ctx context.Context, offset, limit int) ([]catalog.Product, error) {
	if offset < 0 {
		return nil, apperror.Validation("offset must be >= 0")
	}
	if limit <= 0 {
		return nil, apperror.Validation("limit must be > 0")
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	const q = `SELECT id, name, slug, description, status, attributes, created_at, updated_at
		FROM products ORDER BY created_at DESC LIMIT $1 OFFSET $2`

	var rows *sql.Rows
	var err error
	if r.tx != nil {
		rows, err = r.tx.QueryContext(ctx, q, limit, offset)
	} else {
		rows, err = r.db.QueryContext(ctx, q, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("product_repo: list: %w", err)
	}
	defer rows.Close()

	var products []catalog.Product
	for rows.Next() {
		p, err := r.scanProduct(rows)
		if err != nil {
			return nil, fmt.Errorf("product_repo: list scan: %w", err)
		}
		products = append(products, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("product_repo: list rows: %w", err)
	}
	return products, nil
}

// Create persists a new product.
func (r *ProductRepo) Create(ctx context.Context, p *catalog.Product) error {
	attrs, err := json.Marshal(p.Attributes)
	if err != nil {
		return fmt.Errorf("product_repo: marshal attributes: %w", err)
	}

	const q = `INSERT INTO products (id, name, slug, description, status, attributes, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	var execer interface {
		ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	}
	if r.tx != nil {
		execer = r.tx
	} else {
		execer = r.db
	}
	_, err = execer.ExecContext(ctx, q,
		p.ID, p.Name, p.Slug, p.Description, string(p.Status),
		attrs, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return apperror.Conflict("product with this slug already exists")
		}
		return fmt.Errorf("product_repo: create: %w", err)
	}
	return nil
}

// Update persists changes to an existing product.
func (r *ProductRepo) Update(ctx context.Context, p *catalog.Product) error {
	attrs, err := json.Marshal(p.Attributes)
	if err != nil {
		return fmt.Errorf("product_repo: marshal attributes: %w", err)
	}

	updatedAt := time.Now().UTC()

	const q = `UPDATE products
		SET name = $1, slug = $2, description = $3, status = $4, attributes = $5, updated_at = $6
		WHERE id = $7`

	var execer interface {
		ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	}
	if r.tx != nil {
		execer = r.tx
	} else {
		execer = r.db
	}
	result, err := execer.ExecContext(ctx, q,
		p.Name, p.Slug, p.Description, string(p.Status),
		attrs, updatedAt, p.ID,
	)
	if err != nil {
		return fmt.Errorf("product_repo: update: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("product_repo: update rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("product not found")
	}
	p.UpdatedAt = updatedAt
	return nil
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...interface{}) error
}

// scanProduct reads a product from a row scanner.
func (r *ProductRepo) scanProduct(s scanner) (*catalog.Product, error) {
	var p catalog.Product
	var status string
	var attrsJSON []byte

	err := s.Scan(
		&p.ID, &p.Name, &p.Slug, &p.Description,
		&status, &attrsJSON, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	p.Status = catalog.Status(status)

	if len(attrsJSON) > 0 {
		if err := json.Unmarshal(attrsJSON, &p.Attributes); err != nil {
			return nil, fmt.Errorf("unmarshal attributes: %w", err)
		}
	}
	if p.Attributes == nil {
		p.Attributes = make(map[string]interface{})
	}

	return &p, nil
}

// FindByCategoryID returns products belonging to the given category,
// ordered by created_at desc.
func (r *ProductRepo) FindByCategoryID(ctx context.Context, categoryID string, offset, limit int) ([]catalog.Product, error) {
	if categoryID == "" {
		return nil, apperror.Validation("category id must not be empty")
	}
	if offset < 0 {
		return nil, apperror.Validation("offset must be >= 0")
	}
	if limit <= 0 {
		return nil, apperror.Validation("limit must be > 0")
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	const q = `SELECT p.id, p.name, p.slug, p.description, p.status, p.attributes, p.created_at, p.updated_at
		FROM products p
		INNER JOIN product_categories pc ON p.id = pc.product_id
		WHERE pc.category_id = $1
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT $2 OFFSET $3`

	var rows *sql.Rows
	var err error
	if r.tx != nil {
		rows, err = r.tx.QueryContext(ctx, q, categoryID, limit, offset)
	} else {
		rows, err = r.db.QueryContext(ctx, q, categoryID, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("product_repo: find by category: %w", err)
	}
	defer rows.Close()

	var products []catalog.Product
	for rows.Next() {
		p, err := r.scanProduct(rows)
		if err != nil {
			return nil, fmt.Errorf("product_repo: find by category scan: %w", err)
		}
		products = append(products, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("product_repo: find by category rows: %w", err)
	}
	return products, nil
}
