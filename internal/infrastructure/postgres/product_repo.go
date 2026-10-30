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
const productSelectColumns = `id, name, slug, description, status, type, pricing_mode, stock_mode, explode_order_lines,
		requires_linked_parent,
		visibility_catalog_mode, visibility_search_mode, visibility_individually_mode, visibility_purchasable_mode,
		attributes, created_at, updated_at`

const productSelectColumnsPrefixed = `p.id, p.name, p.slug, p.description, p.status, p.type, p.pricing_mode, p.stock_mode, p.explode_order_lines,
		p.requires_linked_parent,
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

// LockForUpdate locks the product row and returns its type.
// The repo must be bound to a transaction via WithTx. The read does not filter
// on status, so draft and archived products lock the same way as active ones.
// Returns NotFound when no row exists.
func (r *ProductRepo) LockForUpdate(ctx context.Context, id string) (catalog.Type, error) {
	typ, _, _, _, _, err := r.LockTypeAndModes(ctx, id)
	return typ, err
}

// LockTypeAndModes locks the product row and returns the type, pricing_mode,
// stock_mode, explode_order_lines, and status stored on that row. Type-change
// updates must read modes, the explode flag, and status here before applying
// an omitted-field default, so a stale FindByID cannot keep or drop dynamic,
// the flag, or Active (soft-write / omitted status).
func (r *ProductRepo) LockTypeAndModes(ctx context.Context, id string) (catalog.Type, catalog.PricingMode, catalog.StockMode, bool, catalog.Status, error) {
	if r.tx == nil {
		return "", "", "", false, "", fmt.Errorf("product_repo: lock for update: transaction required")
	}
	const q = `SELECT type, pricing_mode, stock_mode, explode_order_lines, status FROM products WHERE id = $1 FOR UPDATE`
	var rawType, rawPricing, rawStock, rawStatus string
	var explode bool
	err := r.tx.QueryRowContext(ctx, q, id).Scan(&rawType, &rawPricing, &rawStock, &explode, &rawStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", false, "", apperror.NotFound("product not found")
	}
	if err != nil {
		return "", "", "", false, "", fmt.Errorf("product_repo: lock for update: %w", err)
	}
	return catalog.Type(rawType), catalog.PricingMode(rawPricing), catalog.StockMode(rawStock), explode, catalog.Status(rawStatus), nil
}

// RequiresLinkedParentForUpdate reads requires_linked_parent on the current
// transaction. The repo must be bound via WithTx. Product admin calls it
// after LockTypeAndModes when an update omits the flag, so the write uses
// the locked row instead of the earlier FindByID snapshot.
func (r *ProductRepo) RequiresLinkedParentForUpdate(ctx context.Context, id string) (bool, error) {
	if r.tx == nil {
		return false, fmt.Errorf("product_repo: requires_linked_parent lock read: transaction required")
	}
	const q = `SELECT requires_linked_parent FROM products WHERE id = $1 FOR UPDATE`
	var flag bool
	err := r.tx.QueryRowContext(ctx, q, id).Scan(&flag)
	if errors.Is(err, sql.ErrNoRows) {
		return false, apperror.NotFound("product not found")
	}
	if err != nil {
		return false, fmt.Errorf("product_repo: requires_linked_parent lock read: %w", err)
	}
	return flag, nil
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

// ListByIDs returns products keyed by ID. Missing IDs are omitted. Empty ids
// returns an empty map. Non-active products are omitted unless ctx opts in,
// matching FindByID. RequiredLeafVariants uses this so a dynamic bundle does
// not load each component with its own query.
func (r *ProductRepo) ListByIDs(ctx context.Context, ids []string) (map[string]*catalog.Product, error) {
	out := make(map[string]*catalog.Product)
	if len(ids) == 0 {
		return out, nil
	}
	seen := make(map[string]struct{}, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return out, nil
	}
	q := `SELECT ` + productSelectColumns + ` FROM products WHERE id = ANY($1)`
	args := []any{unique}
	if !catalog.IncludeNonActiveProducts(ctx) {
		q += ` AND status = $2`
		args = append(args, string(catalog.StatusActive))
	}
	var (
		rows *sql.Rows
		err  error
	)
	if r.tx != nil {
		rows, err = r.tx.QueryContext(ctx, q, args...)
	} else {
		rows, err = r.db.QueryContext(ctx, q, args...)
	}
	if err != nil {
		return nil, fmt.Errorf("product_repo: list by ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		p, err := r.scanProduct(rows)
		if err != nil {
			return nil, fmt.Errorf("product_repo: list by ids scan: %w", err)
		}
		out[p.ID] = p
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("product_repo: list by ids: %w", err)
	}
	return out, nil
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
	q += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`, limitArg, offsetArg)

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
		id, name, slug, description, status, type, pricing_mode, stock_mode, explode_order_lines,
		requires_linked_parent,
		visibility_catalog_mode, visibility_search_mode, visibility_individually_mode, visibility_purchasable_mode,
		attributes, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`

	var execer interface {
		ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	}
	if r.tx != nil {
		execer = r.tx
	} else {
		execer = r.db
	}
	_, err = execer.ExecContext(ctx, q,
		p.ID, p.Name, p.Slug, p.Description, string(p.Status), string(p.Type), string(p.PricingMode), string(p.StockMode), p.ExplodeOrderLines,
		p.RequiresLinkedParent,
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
			case pgErr.Code == "23514" && pgErr.ConstraintName == "products_pricing_mode_check":
				return apperror.Validation(catalog.InvalidPricingModeMessage(p.PricingMode))
			case pgErr.Code == "23514" && pgErr.ConstraintName == "products_stock_mode_check":
				return apperror.Validation(catalog.InvalidStockModeMessage(p.StockMode))
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
		SET name = $1, slug = $2, description = $3, status = $4, type = $5, pricing_mode = $6, stock_mode = $7,
			explode_order_lines = $8,
			requires_linked_parent = $9,
			visibility_catalog_mode = $10, visibility_search_mode = $11,
			visibility_individually_mode = $12, visibility_purchasable_mode = $13,
			attributes = $14, updated_at = $15
		WHERE id = $16`

	var execer interface {
		ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
	}
	if r.tx != nil {
		execer = r.tx
	} else {
		execer = r.db
	}
	result, err := execer.ExecContext(ctx, q,
		p.Name, p.Slug, p.Description, string(p.Status), string(p.Type), string(p.PricingMode), string(p.StockMode), p.ExplodeOrderLines,
		p.RequiresLinkedParent,
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
			case "products_pricing_mode_check":
				return apperror.Validation(catalog.InvalidPricingModeMessage(p.PricingMode))
			case "products_stock_mode_check":
				return apperror.Validation(catalog.InvalidStockModeMessage(p.StockMode))
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
	var pricingMode string
	var stockMode string
	var explodeOrderLines bool
	var requiresLinkedParent bool
	var catalogMode, searchMode, individuallyMode, purchasableMode string
	var attrsJSON []byte

	err := s.Scan(
		&p.ID, &p.Name, &p.Slug, &p.Description,
		&status, &productType, &pricingMode, &stockMode, &explodeOrderLines,
		&requiresLinkedParent,
		&catalogMode, &searchMode, &individuallyMode, &purchasableMode,
		&attrsJSON, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	p.Status = catalog.Status(status)
	p.Type = catalog.Type(productType)
	p.PricingMode = catalog.PricingMode(pricingMode)
	p.StockMode = catalog.StockMode(stockMode)
	p.ExplodeOrderLines = explodeOrderLines
	p.RequiresLinkedParent = requiresLinkedParent
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
