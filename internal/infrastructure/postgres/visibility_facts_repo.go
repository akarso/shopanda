package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

// VisibilityFactsRepo loads visibility snapshots for AutoBasis + modes (PR-1057).
//
// HasStoreAssignment is always true: there is no product↔store assignment table
// yet. A future assignment model can flip the column without changing the gate API.
type VisibilityFactsRepo struct {
	db *sql.DB
}

// NewVisibilityFactsRepo creates a VisibilityFactsRepo.
func NewVisibilityFactsRepo(db *sql.DB) (*VisibilityFactsRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("postgres: db is required")
	}
	return &VisibilityFactsRepo{db: db}, nil
}

// LoadMany returns VisibilitySnapshot keyed by product ID.
func (r *VisibilityFactsRepo) LoadMany(ctx context.Context, productIDs []string) (map[string]catalog.VisibilitySnapshot, error) {
	out := make(map[string]catalog.VisibilitySnapshot, len(productIDs))
	if len(productIDs) == 0 {
		return out, nil
	}

	seen := make(map[string]struct{}, len(productIDs))
	ids := make([]string, 0, len(productIDs))
	for _, id := range productIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return out, nil
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT
			p.id,
			p.status,
			p.visibility_catalog_mode,
			p.visibility_search_mode,
			p.visibility_individually_mode,
			p.visibility_purchasable_mode,
			EXISTS (
				SELECT 1
				FROM variants v
				JOIN stock s ON s.variant_id = v.id
				WHERE v.product_id = p.id AND s.quantity > 0
			) AS quantity_positive,
			EXISTS (
				SELECT 1
				FROM variants v
				JOIN prices pr ON pr.variant_id = v.id
				WHERE v.product_id = p.id
			) AS has_price,
			EXISTS (
				SELECT 1
				FROM product_categories pc
				WHERE pc.product_id = p.id
			) AS has_category
		FROM products p
		WHERE p.id = ANY($1) AND p.status = 'active'
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("visibility facts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id                                string
			status                            string
			catalogMode, searchMode           string
			individuallyMode, purchasableMode string
			qty, price, category              bool
		)
		if err := rows.Scan(
			&id, &status,
			&catalogMode, &searchMode, &individuallyMode, &purchasableMode,
			&qty, &price, &category,
		); err != nil {
			return nil, fmt.Errorf("visibility facts scan: %w", err)
		}
		out[id] = catalog.VisibilitySnapshot{
			Status: catalog.Status(status),
			Modes: catalog.VisibilityAxes{
				Catalog:      catalog.VisibilityMode(catalogMode),
				Search:       catalog.VisibilityMode(searchMode),
				Individually: catalog.VisibilityMode(individuallyMode),
				Purchasable:  catalog.VisibilityMode(purchasableMode),
			},
			Inputs: catalog.VisibilityInputs{
				QuantityPositive:   qty,
				HasPrice:           price,
				HasStoreAssignment: true,
				HasCategory:        category,
			},
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("visibility facts rows: %w", err)
	}
	return out, nil
}
