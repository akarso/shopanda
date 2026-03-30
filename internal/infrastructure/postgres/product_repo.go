package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

const maxListLimit = 100

// Compile-time check that ProductRepo implements catalog.ProductRepository.
var _ catalog.ProductRepository = (*ProductRepo)(nil)

// ProductRepo implements catalog.ProductRepository using PostgreSQL.
type ProductRepo struct {
	db *sql.DB
}

// NewProductRepo returns a new ProductRepo backed by db.
func NewProductRepo(db *sql.DB) *ProductRepo {
	return &ProductRepo{db: db}
}

// FindByID returns a product by its ID.
// Returns (nil, nil) when the product does not exist.
func (r *ProductRepo) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	const q = `SELECT id, name, slug, description, status, attributes, created_at, updated_at
		FROM products WHERE id = $1`

	p, err := r.scanProduct(r.db.QueryRowContext(ctx, q, id))
	if err == sql.ErrNoRows {
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

	p, err := r.scanProduct(r.db.QueryRowContext(ctx, q, slug))
	if err == sql.ErrNoRows {
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

	rows, err := r.db.QueryContext(ctx, q, limit, offset)
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

	_, err = r.db.ExecContext(ctx, q,
		p.ID, p.Name, p.Slug, p.Description, string(p.Status),
		attrs, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
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

	p.UpdatedAt = time.Now().UTC()

	const q = `UPDATE products
		SET name = $1, slug = $2, description = $3, status = $4, attributes = $5, updated_at = $6
		WHERE id = $7`

	result, err := r.db.ExecContext(ctx, q,
		p.Name, p.Slug, p.Description, string(p.Status),
		attrs, p.UpdatedAt, p.ID,
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
