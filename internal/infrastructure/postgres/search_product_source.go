package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	domainsearch "github.com/akarso/shopanda/internal/domain/search"
)

// Compile-time check that SearchProductSource implements domainsearch.ProductSource.
var _ domainsearch.ProductSource = (*SearchProductSource)(nil)

// SearchProductSource implements domainsearch.ProductSource by reading
// directly from the products table — deliberately not routed through
// catalog.ProductRepository (search.Product avoids importing the catalog
// package; see its own doc comment), and deliberately not scanning
// status/price/stock: this PR's full reindex indexes exactly the fields
// the CLI's inline loop indexed before it (id, name, slug, description,
// created_at, attributes) — a mechanism change, not a behavior change.
// CategoryIDs is the one exception (PR-1037): it's populated via a
// separate batched query against product_categories rather than added to
// the row scan, since it's a one-to-many relationship.
type SearchProductSource struct {
	db *sql.DB
}

// NewSearchProductSource returns a SearchProductSource backed by db.
func NewSearchProductSource(db *sql.DB) (*SearchProductSource, error) {
	if db == nil {
		return nil, fmt.Errorf("NewSearchProductSource: nil *sql.DB")
	}
	return &SearchProductSource{db: db}, nil
}

// CountAll implements domainsearch.ProductSource.
func (s *SearchProductSource) CountAll(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM products`).Scan(&count); err != nil {
		return 0, fmt.Errorf("search_product_source: count: %w", err)
	}
	return count, nil
}

// ListAll implements domainsearch.ProductSource. Ordered by created_at
// then id, matching ProductRepo.List's tie-breaking convention, so paging
// by offset stays stable across calls even when several products share a
// created_at value.
//
// Each call is its own statement, not a shared snapshot: unlike the old
// inline CLI loop (which wrapped its whole scan in one REPEATABLE READ
// transaction), a product created or deleted between two ListAll calls
// during the same run can shift later offsets, causing a row to be
// skipped or (less likely, since new rows sort to the end) seen twice —
// the classic offset-pagination-under-concurrent-writes anomaly. This is
// an accepted, disclosed trade-off for this PR: holding one long-lived
// transaction open for the duration of an entire catalog reindex — now a
// worker-process job that can span many poll ticks, not a short-lived
// one-shot CLI process — is its own operational risk (a long transaction
// blocks autovacuum and holds a snapshot the whole time). See RUNBOOK.md's
// "Search reindex" section for the operator-facing version of this note.
// A future PR can revisit this if concurrent catalog writes during a
// reindex turn out to matter in practice.
func (s *SearchProductSource) ListAll(ctx context.Context, offset, limit int) ([]domainsearch.Product, error) {
	if offset < 0 {
		return nil, fmt.Errorf("search_product_source: offset must be >= 0")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("search_product_source: limit must be > 0")
	}

	const q = `SELECT id, name, slug, description, attributes, created_at
		FROM products ORDER BY created_at, id LIMIT $1 OFFSET $2`

	rows, err := s.db.QueryContext(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("search_product_source: list: %w", err)
	}
	defer rows.Close()
	products, err := scanSearchProducts(rows, "list")
	if err != nil {
		return nil, err
	}
	if err := s.populateCategoryIDs(ctx, products); err != nil {
		return nil, err
	}
	return products, nil
}

// ListByIDs implements domainsearch.ProductSource. Same field set as
// ListAll (this is a scoped equivalent of it, not a different kind of
// read) and the same no-shared-snapshot trade-off applies, though it
// matters less here: the caller already has a fixed ID list in hand
// rather than paging live over the table, so the only anomaly left is a
// product deleted between resolving the scope and this call, which
// simply drops out of the result (see the port's own doc comment).
func (s *SearchProductSource) ListByIDs(ctx context.Context, ids []string) ([]domainsearch.Product, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	const q = `SELECT id, name, slug, description, attributes, created_at
		FROM products WHERE id = ANY($1)`

	rows, err := s.db.QueryContext(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("search_product_source: list by ids: %w", err)
	}
	defer rows.Close()
	products, err := scanSearchProducts(rows, "list by ids")
	if err != nil {
		return nil, err
	}
	if err := s.populateCategoryIDs(ctx, products); err != nil {
		return nil, err
	}
	return products, nil
}

// populateCategoryIDs fills in each product's CategoryIDs via one batched
// query against product_categories, rather than a per-product round trip
// (this runs once per ListAll/ListByIDs page, potentially hundreds of
// products during a full reindex).
func (s *SearchProductSource) populateCategoryIDs(ctx context.Context, products []domainsearch.Product) error {
	if len(products) == 0 {
		return nil
	}
	ids := make([]string, len(products))
	idx := make(map[string]int, len(products))
	for i, p := range products {
		ids[i] = p.ID
		idx[p.ID] = i
	}

	const q = `SELECT product_id, category_id FROM product_categories WHERE product_id = ANY($1) ORDER BY product_id, category_id`
	rows, err := s.db.QueryContext(ctx, q, ids)
	if err != nil {
		return fmt.Errorf("search_product_source: category ids: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var productID, categoryID string
		if err := rows.Scan(&productID, &categoryID); err != nil {
			return fmt.Errorf("search_product_source: category ids scan: %w", err)
		}
		i := idx[productID]
		products[i].CategoryIDs = append(products[i].CategoryIDs, categoryID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("search_product_source: category ids rows: %w", err)
	}
	return nil
}

// ProductIDsByCategory implements domainsearch.ProductSource.
func (s *SearchProductSource) ProductIDsByCategory(ctx context.Context, categoryIDs []string) ([]string, error) {
	if len(categoryIDs) == 0 {
		return nil, fmt.Errorf("search_product_source: at least one category id is required")
	}

	const q = `SELECT DISTINCT product_id FROM product_categories WHERE category_id = ANY($1)`

	rows, err := s.db.QueryContext(ctx, q, categoryIDs)
	if err != nil {
		return nil, fmt.Errorf("search_product_source: product ids by category: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("search_product_source: product ids by category scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search_product_source: product ids by category rows: %w", err)
	}
	return ids, nil
}

// ProductIDsUpdatedSince implements domainsearch.ProductSource.
func (s *SearchProductSource) ProductIDsUpdatedSince(ctx context.Context, since time.Time) ([]string, error) {
	const q = `SELECT id FROM products WHERE updated_at >= $1`

	rows, err := s.db.QueryContext(ctx, q, since)
	if err != nil {
		return nil, fmt.Errorf("search_product_source: product ids updated since: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("search_product_source: product ids updated since scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search_product_source: product ids updated since rows: %w", err)
	}
	return ids, nil
}

// scanSearchProducts scans the shared (id, name, slug, description,
// attributes, created_at) row shape ListAll and ListByIDs both select —
// op names the caller in a wrapped error, so a scan failure is still
// traceable to which query produced it.
func scanSearchProducts(rows *sql.Rows, op string) ([]domainsearch.Product, error) {
	var products []domainsearch.Product
	for rows.Next() {
		var p domainsearch.Product
		var attrsJSON []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &attrsJSON, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("search_product_source: %s scan: %w", op, err)
		}
		if len(attrsJSON) > 0 {
			if err := json.Unmarshal(attrsJSON, &p.Attributes); err != nil {
				return nil, fmt.Errorf("search_product_source: unmarshal attributes: %w", err)
			}
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search_product_source: %s rows: %w", op, err)
	}
	return products, nil
}
