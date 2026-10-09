package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

const maxListLimit = 100

// productSelectColumns is the column list for product scans (keep in sync with scanProduct).
const productSelectColumns = `id, name, slug, description, status, type,
		visibility_catalog_mode, visibility_search_mode, visibility_individually_mode, visibility_purchasable_mode,
		attributes, created_at, updated_at`

const productSelectColumnsPrefixed = `p.id, p.name, p.slug, p.description, p.status, p.type,
		p.visibility_catalog_mode, p.visibility_search_mode, p.visibility_individually_mode, p.visibility_purchasable_mode,
		p.attributes, p.created_at, p.updated_at`

// Compile-time check that ProductRepo implements catalog.ProductRepository.
var _ catalog.ProductRepository = (*ProductRepo)(nil)
var _ catalog.ProductCategoryAssignmentRepository = (*ProductRepo)(nil)

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

func productReadIncludeNonActive(ctx context.Context, filter catalog.ListFilter) bool {
	return filter.IncludeNonActive || catalog.IncludeNonActiveProducts(ctx)
}

// FindByID returns a product by its ID.
// Returns (nil, nil) when the product does not exist or is not active under the read scope.
func (r *ProductRepo) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	q := `SELECT ` + productSelectColumns + `
		FROM products WHERE id = $1`
	args := []interface{}{id}
	if !catalog.IncludeNonActiveProducts(ctx) {
		q += ` AND status = $2`
		args = append(args, string(catalog.StatusActive))
	}

	var querier interface {
		QueryRowContext(context.Context, string, ...interface{}) *sql.Row
	}
	if r.tx != nil {
		querier = r.tx
	} else {
		querier = r.db
	}
	p, err := r.scanProduct(querier.QueryRowContext(ctx, q, args...))
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
	q := `SELECT ` + productSelectColumns + `
		FROM products WHERE slug = $1`
	args := []interface{}{slug}
	if !catalog.IncludeNonActiveProducts(ctx) {
		q += ` AND status = $2`
		args = append(args, string(catalog.StatusActive))
	}

	var querier interface {
		QueryRowContext(context.Context, string, ...interface{}) *sql.Row
	}
	if r.tx != nil {
		querier = r.tx
	} else {
		querier = r.db
	}
	p, err := r.scanProduct(querier.QueryRowContext(ctx, q, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("product_repo: find by slug: %w", err)
	}
	return p, nil
}

// List returns a page of products ordered by created_at desc.
func (r *ProductRepo) List(ctx context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
	if filter.Offset < 0 {
		return nil, apperror.Validation("offset must be >= 0")
	}
	if filter.Limit <= 0 {
		return nil, apperror.Validation("limit must be > 0")
	}
	limit := filter.Limit
	if limit > maxListLimit {
		limit = maxListLimit
	}

	if filter.Type != "" && !filter.Type.IsValid() {
		return nil, apperror.Validation(catalog.InvalidTypeMessage(filter.Type))
	}

	includeNonActive := productReadIncludeNonActive(ctx, filter)
	var where []string
	args := make([]interface{}, 0, 4)
	if !includeNonActive {
		where = append(where, fmt.Sprintf("status = $%d", len(args)+1))
		args = append(args, string(catalog.StatusActive))
	}
	if filter.Type != "" {
		where = append(where, fmt.Sprintf("type = $%d", len(args)+1))
		args = append(args, string(filter.Type))
	}
	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	args = append(args, limit, filter.Offset)

	q := `SELECT ` + productSelectColumns + `
		FROM products`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	q += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, limitArg, offsetArg)

	var rows *sql.Rows
	var err error
	if r.tx != nil {
		rows, err = r.tx.QueryContext(ctx, q, args...)
	} else {
		rows, err = r.db.QueryContext(ctx, q, args...)
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
	if err := validateProduct(p); err != nil {
		return err
	}
	attrs, err := json.Marshal(p.Attributes)
	if err != nil {
		return fmt.Errorf("product_repo: marshal attributes: %w", err)
	}

	const q = `INSERT INTO products (
		id, name, slug, description, status, type,
		visibility_catalog_mode, visibility_search_mode, visibility_individually_mode, visibility_purchasable_mode,
		attributes, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`

	var execer interface {
		ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	}
	if r.tx != nil {
		execer = r.tx
	} else {
		execer = r.db
	}
	_, err = execer.ExecContext(ctx, q,
		p.ID, p.Name, p.Slug, p.Description, string(p.Status), string(p.Type),
		string(p.VisibilityModes.Catalog), string(p.VisibilityModes.Search),
		string(p.VisibilityModes.Individually), string(p.VisibilityModes.Purchasable),
		attrs, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch {
			case pgErr.Code == "23505":
				return apperror.Conflict("product with this slug already exists")
			case pgErr.Code == "23514" && pgErr.ConstraintName == "products_type_check":
				return apperror.Validation(catalog.InvalidTypeMessage(p.Type))
			case pgErr.Code == "23514" && pgErr.ConstraintName == "products_visibility_modes_check":
				return apperror.Validation(visibilityModesValidationMessage(p.VisibilityModes))
			}
		}
		return fmt.Errorf("product_repo: create: %w", err)
	}
	return nil
}

// Update persists changes to an existing product.
func (r *ProductRepo) Update(ctx context.Context, p *catalog.Product) error {
	if err := validateProduct(p); err != nil {
		return err
	}
	attrs, err := json.Marshal(p.Attributes)
	if err != nil {
		return fmt.Errorf("product_repo: marshal attributes: %w", err)
	}

	updatedAt := time.Now().UTC()

	const q = `UPDATE products
		SET name = $1, slug = $2, description = $3, status = $4, type = $5,
			visibility_catalog_mode = $6, visibility_search_mode = $7,
			visibility_individually_mode = $8, visibility_purchasable_mode = $9,
			attributes = $10, updated_at = $11
		WHERE id = $12`

	var execer interface {
		ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	}
	if r.tx != nil {
		execer = r.tx
	} else {
		execer = r.db
	}
	result, err := execer.ExecContext(ctx, q,
		p.Name, p.Slug, p.Description, string(p.Status), string(p.Type),
		string(p.VisibilityModes.Catalog), string(p.VisibilityModes.Search),
		string(p.VisibilityModes.Individually), string(p.VisibilityModes.Purchasable),
		attrs, updatedAt, p.ID,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			switch pgErr.ConstraintName {
			case "products_type_check":
				return apperror.Validation(catalog.InvalidTypeMessage(p.Type))
			case "products_visibility_modes_check":
				return apperror.Validation(visibilityModesValidationMessage(p.VisibilityModes))
			}
		}
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

func validateProduct(p *catalog.Product) error {
	if err := p.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	return nil
}

// visibilityModesValidationMessage maps a DB CHECK (23514) failure to a
// validation string. Domain Validate normally runs first; this is defense when
// CHECK and domain diverge or Validate was bypassed.
func visibilityModesValidationMessage(axes catalog.VisibilityAxes) string {
	for _, m := range []catalog.VisibilityMode{axes.Catalog, axes.Search, axes.Individually, axes.Purchasable} {
		if !m.IsValid() {
			return catalog.InvalidVisibilityModeMessage(m)
		}
	}
	return "visibility mode rejected by database constraint"
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...interface{}) error
}

// scanProduct reads a product from a row scanner.
func (r *ProductRepo) scanProduct(s scanner) (*catalog.Product, error) {
	var p catalog.Product
	var status string
	var productType string
	var catalogMode, searchMode, individuallyMode, purchasableMode string
	var attrsJSON []byte

	err := s.Scan(
		&p.ID, &p.Name, &p.Slug, &p.Description,
		&status, &productType,
		&catalogMode, &searchMode, &individuallyMode, &purchasableMode,
		&attrsJSON, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	p.Status = catalog.Status(status)
	p.Type = catalog.Type(productType)
	p.VisibilityModes = catalog.VisibilityAxes{
		Catalog:      catalog.VisibilityMode(catalogMode),
		Search:       catalog.VisibilityMode(searchMode),
		Individually: catalog.VisibilityMode(individuallyMode),
		Purchasable:  catalog.VisibilityMode(purchasableMode),
	}

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

	q := `SELECT ` + productSelectColumnsPrefixed + `
		FROM products p
		INNER JOIN product_categories pc ON p.id = pc.product_id
		WHERE pc.category_id = $1`
	args := []interface{}{categoryID}
	if !catalog.IncludeNonActiveProducts(ctx) {
		q += ` AND p.status = $2`
		args = append(args, string(catalog.StatusActive))
	}
	q += ` ORDER BY p.created_at DESC, p.id DESC`
	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	q += fmt.Sprintf(` LIMIT $%d OFFSET $%d`, limitArg, offsetArg)
	args = append(args, limit, offset)

	var rows *sql.Rows
	var err error
	if r.tx != nil {
		rows, err = r.tx.QueryContext(ctx, q, args...)
	} else {
		rows, err = r.db.QueryContext(ctx, q, args...)
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

// AssignCategory creates a product-category link when it does not already exist.
func (r *ProductRepo) AssignCategory(ctx context.Context, productID, categoryID string) error {
	if productID == "" {
		return apperror.Validation("product id must not be empty")
	}
	if categoryID == "" {
		return apperror.Validation("category id must not be empty")
	}

	const q = `INSERT INTO product_categories (product_id, category_id)
		VALUES ($1, $2)
		ON CONFLICT (product_id, category_id) DO NOTHING`

	var execer interface {
		ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	}
	if r.tx != nil {
		execer = r.tx
	} else {
		execer = r.db
	}
	if _, err := execer.ExecContext(ctx, q, productID, categoryID); err != nil {
		return fmt.Errorf("product_repo: assign category: %w", err)
	}
	return nil
}

// RemoveCategory deletes a product-category link.
func (r *ProductRepo) RemoveCategory(ctx context.Context, productID, categoryID string) error {
	if productID == "" {
		return apperror.Validation("product id must not be empty")
	}
	if categoryID == "" {
		return apperror.Validation("category id must not be empty")
	}

	const q = `DELETE FROM product_categories WHERE product_id = $1 AND category_id = $2`

	var execer interface {
		ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	}
	if r.tx != nil {
		execer = r.tx
	} else {
		execer = r.db
	}
	if _, err := execer.ExecContext(ctx, q, productID, categoryID); err != nil {
		return fmt.Errorf("product_repo: remove category: %w", err)
	}
	return nil
}

// ListCategoryIDsByProduct returns assigned category IDs for a product.
func (r *ProductRepo) ListCategoryIDsByProduct(ctx context.Context, productID string) ([]string, error) {
	if productID == "" {
		return nil, apperror.Validation("product id must not be empty")
	}

	const q = `SELECT category_id
		FROM product_categories
		WHERE product_id = $1
		ORDER BY category_id ASC`

	var rows *sql.Rows
	var err error
	if r.tx != nil {
		rows, err = r.tx.QueryContext(ctx, q, productID)
	} else {
		rows, err = r.db.QueryContext(ctx, q, productID)
	}
	if err != nil {
		return nil, fmt.Errorf("product_repo: list category ids by product: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var categoryID string
		if err := rows.Scan(&categoryID); err != nil {
			return nil, fmt.Errorf("product_repo: list category ids by product scan: %w", err)
		}
		ids = append(ids, categoryID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("product_repo: list category ids by product rows: %w", err)
	}
	return ids, nil
}
