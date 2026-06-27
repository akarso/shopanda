package pricing

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/customergroup"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/lib/pq"
)

// PostgresGroupPriceRepo implements customergroup.GroupPriceRepository.
type PostgresGroupPriceRepo struct {
	db *sql.DB
}

// NewPostgresGroupPriceRepo creates a PostgresGroupPriceRepo.
func NewPostgresGroupPriceRepo(db *sql.DB) (*PostgresGroupPriceRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("b2b group price repo: nil db")
	}
	return &PostgresGroupPriceRepo{db: db}, nil
}

func (r *PostgresGroupPriceRepo) FindByVariantsGroupCurrencyAndStore(ctx context.Context, variantIDs []string, groupID, currency, storeID string) (map[string]*customergroup.GroupPrice, error) {
	if len(variantIDs) == 0 {
		return nil, nil
	}
	groupID = strings.TrimSpace(groupID)
	currency = strings.TrimSpace(currency)
	if groupID == "" || currency == "" {
		return nil, fmt.Errorf("b2b group price repo: group id and currency required")
	}

	const q = `SELECT id, group_id, variant_id, store_id, currency, amount, created_at
		FROM customer_group_prices
		WHERE variant_id = ANY($1) AND group_id = $2 AND currency = $3 AND store_id = $4`
	rows, err := r.db.QueryContext(ctx, q, pq.Array(variantIDs), groupID, currency, storeID)
	if err != nil {
		return nil, fmt.Errorf("b2b group price repo: find batch: %w", err)
	}
	defer rows.Close()

	out, err := scanGroupPricesByVariant(rows)
	if err != nil {
		return nil, err
	}

	if storeID != "" {
		var missing []string
		for _, vid := range variantIDs {
			if out[vid] == nil {
				missing = append(missing, vid)
			}
		}
		if len(missing) > 0 {
			fallback, err := r.FindByVariantsGroupCurrencyAndStore(ctx, missing, groupID, currency, "")
			if err != nil {
				return nil, err
			}
			for vid, price := range fallback {
				out[vid] = price
			}
		}
	}

	return out, nil
}

func (r *PostgresGroupPriceRepo) FindByVariantGroupCurrencyAndStore(ctx context.Context, variantID, groupID, currency, storeID string) (*customergroup.GroupPrice, error) {
	out, err := r.FindByVariantsGroupCurrencyAndStore(ctx, []string{variantID}, groupID, currency, storeID)
	if err != nil {
		return nil, err
	}
	return out[variantID], nil
}

func (r *PostgresGroupPriceRepo) Upsert(ctx context.Context, price *customergroup.GroupPrice) error {
	if price == nil {
		return fmt.Errorf("b2b group price repo: price must not be nil")
	}
	const q = `INSERT INTO customer_group_prices (id, group_id, variant_id, store_id, currency, amount, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (group_id, variant_id, currency, store_id) DO UPDATE
		SET amount = EXCLUDED.amount,
		    id = EXCLUDED.id,
		    created_at = EXCLUDED.created_at`
	_, err := r.db.ExecContext(ctx, q,
		price.ID, price.GroupID, price.VariantID, price.StoreID,
		price.Amount.Currency(), price.Amount.Amount(), price.CreatedAt,
	)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23503" {
			return fmt.Errorf("b2b group price repo: group or variant not found")
		}
		return fmt.Errorf("b2b group price repo: upsert: %w", err)
	}
	return nil
}

func (r *PostgresGroupPriceRepo) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("b2b group price repo: empty id")
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM customer_group_prices WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("b2b group price repo: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("b2b group price repo: delete rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("b2b group price repo: price not found")
	}
	return nil
}

type groupPriceScanner interface {
	Scan(dest ...interface{}) error
}

func scanGroupPrice(s groupPriceScanner) (*customergroup.GroupPrice, error) {
	var p customergroup.GroupPrice
	var amount int64
	var currency string
	if err := s.Scan(&p.ID, &p.GroupID, &p.VariantID, &p.StoreID, &currency, &amount, &p.CreatedAt); err != nil {
		return nil, err
	}
	money, err := shared.NewMoney(amount, currency)
	if err != nil {
		return nil, fmt.Errorf("b2b group price repo: reconstruct money: %w", err)
	}
	p.Amount = money
	return &p, nil
}

func scanGroupPricesByVariant(rows *sql.Rows) (map[string]*customergroup.GroupPrice, error) {
	out := make(map[string]*customergroup.GroupPrice)
	for rows.Next() {
		p, err := scanGroupPrice(rows)
		if err != nil {
			return nil, fmt.Errorf("b2b group price repo: scan: %w", err)
		}
		out[p.VariantID] = p
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("b2b group price repo: rows: %w", err)
	}
	return out, nil
}

var _ customergroup.GroupPriceRepository = (*PostgresGroupPriceRepo)(nil)
