package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	domainsearch "github.com/akarso/shopanda/internal/domain/search"
)

// Compile-time check that SearchCategorySource implements domainsearch.CategorySource.
var _ domainsearch.CategorySource = (*SearchCategorySource)(nil)

// SearchCategorySource implements domainsearch.CategorySource by reading
// directly from the categories/product_categories tables — deliberately
// not routed through catalog.CategoryRepository (search.Category avoids
// importing the catalog package; see its own doc comment).
type SearchCategorySource struct {
	db *sql.DB
}

// NewSearchCategorySource returns a SearchCategorySource backed by db.
func NewSearchCategorySource(db *sql.DB) (*SearchCategorySource, error) {
	if db == nil {
		return nil, fmt.Errorf("NewSearchCategorySource: nil *sql.DB")
	}
	return &SearchCategorySource{db: db}, nil
}

// GetByID implements domainsearch.CategorySource.
func (s *SearchCategorySource) GetByID(ctx context.Context, categoryID string) (domainsearch.Category, bool, error) {
	const q = `SELECT c.name, c.slug, c.parent_id,
		(SELECT COUNT(DISTINCT product_id) FROM product_categories WHERE category_id = c.id)
		FROM categories c WHERE c.id = $1`

	var c domainsearch.Category
	var parentID sql.NullString
	err := s.db.QueryRowContext(ctx, q, categoryID).Scan(&c.Name, &c.Slug, &parentID, &c.ProductCount)
	if errors.Is(err, sql.ErrNoRows) {
		return domainsearch.Category{}, false, nil
	}
	if err != nil {
		return domainsearch.Category{}, false, fmt.Errorf("search_category_source: get by id: %w", err)
	}
	c.ID = categoryID
	if parentID.Valid {
		c.ParentID = parentID.String
	}
	return c, true, nil
}
