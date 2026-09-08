package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	domainsearch "github.com/akarso/shopanda/internal/domain/search"
)

// Compile-time check that SearchProductSource implements domainsearch.ProductSource.
var _ domainsearch.ProductSource = (*SearchProductSource)(nil)

// SearchProductSource implements domainsearch.ProductSource by reading
// directly from the products table — deliberately not routed through
// catalog.ProductRepository (search.Product avoids importing the catalog
// package; see its own doc comment), and deliberately not scanning
// status/category/price/stock: this PR's full reindex indexes exactly the
// fields the CLI's inline loop indexed before it (id, name, slug,
// description, created_at, attributes) — a mechanism change, not a
// behavior change.
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

	var products []domainsearch.Product
	for rows.Next() {
		var p domainsearch.Product
		var attrsJSON []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &attrsJSON, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("search_product_source: list scan: %w", err)
		}
		if len(attrsJSON) > 0 {
			if err := json.Unmarshal(attrsJSON, &p.Attributes); err != nil {
				return nil, fmt.Errorf("search_product_source: unmarshal attributes: %w", err)
			}
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search_product_source: list rows: %w", err)
	}
	return products, nil
}
