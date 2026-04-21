package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/search"
)

// Compile-time check that SearchEngine implements search.SearchEngine.
var _ search.SearchEngine = (*SearchEngine)(nil)

// SearchEngine implements search.SearchEngine using PostgreSQL full-text search.
type SearchEngine struct {
	db *sql.DB
}

// NewSearchEngine returns a new SearchEngine backed by db.
func NewSearchEngine(db *sql.DB) (*SearchEngine, error) {
	if db == nil {
		return nil, fmt.Errorf("NewSearchEngine: nil *sql.DB")
	}
	return &SearchEngine{db: db}, nil
}

// Name returns "postgres".
func (e *SearchEngine) Name() string { return "postgres" }

// IndexProduct updates the search vector for a product.
// The search_vector trigger on the products table handles normal INSERT/UPDATE,
// so this method is primarily useful for explicit reindexing.
func (e *SearchEngine) IndexProduct(ctx context.Context, p search.Product) error {
	const q = `UPDATE products
		SET search_vector = to_tsvector('english', coalesce($2, '') || ' ' || coalesce($3, ''))
		WHERE id = $1`
	result, err := e.db.ExecContext(ctx, q, p.ID, p.Name, p.Description)
	if err != nil {
		return fmt.Errorf("search_engine: index product: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("search_engine: index product rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("search_engine: product %s not found", p.ID)
	}
	return nil
}

// RemoveProduct clears the search vector for a product, making it unsearchable.
func (e *SearchEngine) RemoveProduct(ctx context.Context, productID string) error {
	const q = `UPDATE products SET search_vector = NULL WHERE id = $1`
	_, err := e.db.ExecContext(ctx, q, productID)
	if err != nil {
		return fmt.Errorf("search_engine: remove product: %w", err)
	}
	return nil
}

// Suggest returns autocomplete suggestions using prefix matching on product names.
func (e *SearchEngine) Suggest(ctx context.Context, prefix string, limit int) ([]search.Suggestion, error) {
	if prefix == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = search.DefaultSuggestLimit
	}
	if limit > search.MaxSuggestLimit {
		limit = search.MaxSuggestLimit
	}

	const q = `SELECT name, slug FROM products WHERE name ILIKE $1 ESCAPE '\' AND status = 'active' ORDER BY name LIMIT $2`
	escaped := escapeLike(prefix)
	rows, err := e.db.QueryContext(ctx, q, escaped+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("search_engine: suggest: %w", err)
	}
	defer rows.Close()

	var suggestions []search.Suggestion
	for rows.Next() {
		var name, slug string
		if err := rows.Scan(&name, &slug); err != nil {
			return nil, fmt.Errorf("search_engine: suggest scan: %w", err)
		}
		suggestions = append(suggestions, search.Suggestion{
			Text: name,
			Type: "product",
			URL:  "/products/" + slug,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search_engine: suggest rows: %w", err)
	}
	return suggestions, nil
}

// Search executes a full-text search query with optional filters, sorting, and facets.
func (e *SearchEngine) Search(ctx context.Context, query search.SearchQuery) (search.SearchResult, error) {
	if err := query.Validate(); err != nil {
		return search.SearchResult{}, err
	}

	limit := query.EffectiveLimit()
	args := []interface{}{}
	argN := 0

	nextArg := func(v interface{}) string {
		argN++
		args = append(args, v)
		return fmt.Sprintf("$%d", argN)
	}

	// Base WHERE clause: only active products with a search vector.
	wheres := []string{"p.status = 'active'", "p.search_vector IS NOT NULL"}
	joins := []string{}
	hasText := strings.TrimSpace(query.Text) != ""

	// Full-text filter.
	var tsQueryRef string
	if hasText {
		textArg := nextArg(query.Text)
		tsQueryRef = fmt.Sprintf("plainto_tsquery('english', %s)", textArg)
		wheres = append(wheres, fmt.Sprintf("p.search_vector @@ %s", tsQueryRef))
	}

	// Category filter.
	if cat, ok := query.Filters["category"]; ok {
		if catStr, ok := cat.(string); ok && catStr != "" {
			catArg := nextArg(catStr)
			joins = append(joins, fmt.Sprintf(
				"INNER JOIN product_categories pc ON pc.product_id = p.id AND pc.category_id = %s", catArg))
		}
	}

	// Price filter.
	if priceFilter, ok := query.Filters["price"]; ok {
		if priceMap, ok := priceFilter.(map[string]interface{}); ok {
			priceConds := []string{}
			if gt, ok := toInt64(priceMap["gt"]); ok {
				priceConds = append(priceConds, fmt.Sprintf("pr.amount > %s", nextArg(gt)))
			}
			if gte, ok := toInt64(priceMap["gte"]); ok {
				priceConds = append(priceConds, fmt.Sprintf("pr.amount >= %s", nextArg(gte)))
			}
			if lt, ok := toInt64(priceMap["lt"]); ok {
				priceConds = append(priceConds, fmt.Sprintf("pr.amount < %s", nextArg(lt)))
			}
			if lte, ok := toInt64(priceMap["lte"]); ok {
				priceConds = append(priceConds, fmt.Sprintf("pr.amount <= %s", nextArg(lte)))
			}
			if len(priceConds) > 0 {
				joins = append(joins, "INNER JOIN variants v ON v.product_id = p.id")
				joins = append(joins, "INNER JOIN prices pr ON pr.variant_id = v.id")
				wheres = append(wheres, priceConds...)
			}
		}
	}

	whereClause := strings.Join(wheres, " AND ")
	joinClause := strings.Join(joins, " ")

	// ORDER BY — used in outer query over deduplicated rows.
	orderBy := "sort_key DESC"
	sortExpr := "p.created_at"
	switch query.Sort {
	case "name":
		sortExpr = "p.name"
		orderBy = "sort_key ASC"
	case "-name":
		sortExpr = "p.name"
		orderBy = "sort_key DESC"
	case "created_at":
		sortExpr = "p.created_at"
		orderBy = "sort_key ASC"
	case "-created_at":
		sortExpr = "p.created_at"
		orderBy = "sort_key DESC"
	default:
		if hasText {
			sortExpr = fmt.Sprintf("ts_rank(p.search_vector, %s)", tsQueryRef)
			orderBy = "sort_key DESC"
		}
	}

	// Count query.
	countQ := fmt.Sprintf(
		"SELECT COUNT(DISTINCT p.id) FROM products p %s WHERE %s",
		joinClause, whereClause)

	var total int
	if err := e.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return search.SearchResult{}, fmt.Errorf("search_engine: count: %w", err)
	}

	if total == 0 {
		return search.SearchResult{
			Products: []search.Product{},
			Facets:   map[string][]search.FacetValue{},
		}, nil
	}

	// Main query: inner subquery deduplicates and projects sort key,
	// outer query applies ORDER BY, LIMIT, OFFSET.
	filterArgs := make([]interface{}, len(args))
	copy(filterArgs, args)
	priceFilter := []string{"v.product_id = sub.id"}
	priceOrder := "CASE WHEN pr.store_id = '' THEN 0 ELSE 1 END"
	if query.Currency != "" {
		priceFilter = append(priceFilter, fmt.Sprintf("pr.currency = %s", nextArg(query.Currency)))
	}
	if query.StoreID != "" {
		storeIDArg := nextArg(query.StoreID)
		priceOrder = fmt.Sprintf("CASE WHEN pr.store_id = %s THEN 0 WHEN pr.store_id = '' THEN 1 ELSE 2 END", storeIDArg)
	}

	limitArg := nextArg(limit)
	offsetArg := nextArg(query.Offset)

	mainQ := fmt.Sprintf(
		`SELECT
			sub.id,
			sub.name,
			sub.slug,
			sub.description,
			sub.created_at,
			sub.attributes,
			COALESCE(
				(
					SELECT pr.amount
					FROM variants v
					INNER JOIN prices pr ON pr.variant_id = v.id
					WHERE %s
					ORDER BY %s, pr.amount ASC, pr.created_at ASC
					LIMIT 1
				),
				0
			) AS price,
			EXISTS(
				SELECT 1
				FROM variants v
				LEFT JOIN stock s ON s.variant_id = v.id
				LEFT JOIN LATERAL (
					SELECT COALESCE(SUM(r.quantity), 0) AS reserved_qty
					FROM reservations r
					WHERE r.variant_id = v.id
					  AND r.status = 'active'
					  AND r.expires_at > now()
				) reserved ON TRUE
				WHERE v.product_id = sub.id
				  AND COALESCE(s.quantity, 0) - COALESCE(reserved.reserved_qty, 0) > 0
			) AS in_stock
		FROM (
			SELECT DISTINCT ON (p.id)
				p.id,
				p.name,
				p.slug,
				p.description,
				p.created_at,
				p.attributes,
				%s AS sort_key
			FROM products p %s
			WHERE %s
			ORDER BY p.id, %s
		) sub
		ORDER BY %s
		LIMIT %s OFFSET %s`,
		strings.Join(priceFilter, " AND "), priceOrder,
		sortExpr, joinClause, whereClause, sortExpr, orderBy, limitArg, offsetArg)

	rows, err := e.db.QueryContext(ctx, mainQ, args...)
	if err != nil {
		return search.SearchResult{}, fmt.Errorf("search_engine: search: %w", err)
	}
	defer rows.Close()

	var products []search.Product
	for rows.Next() {
		var p search.Product
		var attrsJSON []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.CreatedAt, &attrsJSON, &p.Price, &p.InStock); err != nil {
			return search.SearchResult{}, fmt.Errorf("search_engine: scan: %w", err)
		}
		if len(attrsJSON) > 0 {
			if err := json.Unmarshal(attrsJSON, &p.Attributes); err != nil {
				return search.SearchResult{}, fmt.Errorf("search_engine: unmarshal attributes: %w", err)
			}
		}
		if p.Attributes == nil {
			p.Attributes = make(map[string]interface{})
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return search.SearchResult{}, fmt.Errorf("search_engine: rows: %w", err)
	}

	// Facets: category counts from the filtered result set.
	facets, err := e.categoryFacets(ctx, joinClause, whereClause, filterArgs)
	if err != nil {
		return search.SearchResult{}, err
	}

	return search.SearchResult{
		Products: products,
		Total:    total,
		Facets:   facets,
	}, nil
}

// categoryFacets computes category counts for the filtered product set.
func (e *SearchEngine) categoryFacets(ctx context.Context, joinClause, whereClause string, args []interface{}) (map[string][]search.FacetValue, error) {
	q := fmt.Sprintf(
		"SELECT c.name, COUNT(DISTINCT p.id) FROM products p %s INNER JOIN product_categories fpc ON fpc.product_id = p.id INNER JOIN categories c ON c.id = fpc.category_id WHERE %s GROUP BY c.name ORDER BY COUNT(DISTINCT p.id) DESC",
		joinClause, whereClause)

	rows, err := e.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("search_engine: category facets: %w", err)
	}
	defer rows.Close()

	var values []search.FacetValue
	for rows.Next() {
		var fv search.FacetValue
		if err := rows.Scan(&fv.Value, &fv.Count); err != nil {
			return nil, fmt.Errorf("search_engine: facet scan: %w", err)
		}
		values = append(values, fv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search_engine: facet rows: %w", err)
	}

	facets := map[string][]search.FacetValue{}
	if len(values) > 0 {
		facets["category"] = values
	}
	return facets, nil
}

// toInt64 attempts to convert an interface value to int64.
// Handles float64 (from JSON) and int/int64 types.
func toInt64(v interface{}) (int64, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	default:
		return 0, false
	}
}
