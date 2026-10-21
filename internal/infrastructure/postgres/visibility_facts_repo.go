package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

// VisibilityFactsRepo loads visibility snapshots for AutoBasis + modes (PR-1057).
//
// HasPrice for a dynamic bundle is true only when the required set is
// non-empty, at most catalog.MaxBundleRequiredComponents, and every required
// component is an active leaf (simple, virtual, or downloadable) with exactly
// one variant that has a global price row (prices.store_id = ”). Amount 0
// counts. The check is currency-agnostic: VisibilityFacts are not store- or
// currency-scoped yet, so a store-only row does not count and any currency on
// the global row does. Listing uses that coarse flag. AddItem does not: a
// dynamic bundle is priced with ResolveUnitPrice in the cart currency, so a
// global EUR row cannot make a USD add succeed. Fixed products use their own
// price row. Grouped parents are never HasPrice, even with a stale row (PR-1062).
//
// HasStoreAssignment is always true: there is no product↔store assignment table
// yet. A future assignment model can flip the column without changing the gate
// API — until then, catalog.AutoBasisCodeNoStoreAssignment is reserved and
// cannot appear in AutoBasisFailureCodes output from this repo.
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

// LoadMany returns VisibilitySnapshot keyed by product ID, restricted to
// active products — correct for every public-gate caller (PR-1057/1058).
func (r *VisibilityFactsRepo) LoadMany(ctx context.Context, productIDs []string) (map[string]catalog.VisibilitySnapshot, error) {
	return r.loadMany(ctx, productIDs, true)
}

// LoadManyAnyStatus returns VisibilitySnapshot keyed by product ID for every
// status (draft/active/archived) — the admin GUI (PR-1059) needs to show
// computed axes and AutoBasis failures for a non-active product too, since
// Status is itself one of the AutoBasis conditions.
func (r *VisibilityFactsRepo) LoadManyAnyStatus(ctx context.Context, productIDs []string) (map[string]catalog.VisibilitySnapshot, error) {
	return r.loadMany(ctx, productIDs, false)
}

func (r *VisibilityFactsRepo) loadMany(ctx context.Context, productIDs []string, activeOnly bool) (map[string]catalog.VisibilitySnapshot, error) {
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

	query := fmt.Sprintf(`
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
			CASE
				WHEN p.type = 'grouped' THEN FALSE
				WHEN p.type = 'bundle' AND p.pricing_mode = 'dynamic' THEN
					(SELECT COUNT(*) FROM bundle_components bc WHERE bc.bundle_product_id = p.id AND bc.required) BETWEEN 1 AND %d
					AND NOT EXISTS (
						SELECT 1
						FROM bundle_components bc
						WHERE bc.bundle_product_id = p.id
							AND bc.required
							AND NOT (
								(SELECT COUNT(*) FROM variants v WHERE v.product_id = bc.component_product_id) = 1
								AND EXISTS (
									SELECT 1
									FROM variants v
									JOIN products cp ON cp.id = v.product_id
									JOIN prices pr ON pr.variant_id = v.id AND pr.store_id = ''
									WHERE v.product_id = bc.component_product_id
										AND cp.status = 'active'
										AND cp.type IN ('simple', 'virtual', 'downloadable')
								)
							)
					)
				ELSE
					EXISTS (
						SELECT 1
						FROM variants v
						JOIN prices pr ON pr.variant_id = v.id
						WHERE v.product_id = p.id
					)
			END AS has_price,
			EXISTS (
				SELECT 1
				FROM product_categories pc
				WHERE pc.product_id = p.id
			) AS has_category
		FROM products p
		WHERE p.id = ANY($1)
			AND ($2::bool = false OR p.status = 'active')`, catalog.MaxBundleRequiredComponents)

	rows, err := r.db.QueryContext(ctx, query, ids, activeOnly)
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
