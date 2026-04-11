package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/promotion"
)

var _ promotion.PromotionRepository = (*PromotionRepo)(nil)

// PromotionRepo implements promotion.PromotionRepository using PostgreSQL.
type PromotionRepo struct {
	db *sql.DB
	tx *sql.Tx
}

// NewPromotionRepo returns a new PromotionRepo backed by db.
func NewPromotionRepo(db *sql.DB) (*PromotionRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewPromotionRepo: nil *sql.DB")
	}
	return &PromotionRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
func (r *PromotionRepo) WithTx(tx *sql.Tx) *PromotionRepo {
	return &PromotionRepo{db: r.db, tx: tx}
}

func (r *PromotionRepo) queryRow(ctx context.Context, q string, args ...interface{}) *sql.Row {
	if r.tx != nil {
		return r.tx.QueryRowContext(ctx, q, args...)
	}
	return r.db.QueryRowContext(ctx, q, args...)
}

func (r *PromotionRepo) query(ctx context.Context, q string, args ...interface{}) (*sql.Rows, error) {
	if r.tx != nil {
		return r.tx.QueryContext(ctx, q, args...)
	}
	return r.db.QueryContext(ctx, q, args...)
}

func (r *PromotionRepo) exec(ctx context.Context, q string, args ...interface{}) (sql.Result, error) {
	if r.tx != nil {
		return r.tx.ExecContext(ctx, q, args...)
	}
	return r.db.ExecContext(ctx, q, args...)
}

func (r *PromotionRepo) FindByID(ctx context.Context, id string) (*promotion.Promotion, error) {
	const q = `SELECT id, name, type, priority, active, start_at, end_at,
		conditions, actions, coupon_bound, created_at, updated_at
		FROM promotions WHERE id = $1`

	p := &promotion.Promotion{}
	err := r.queryRow(ctx, q, id).Scan(
		&p.ID, &p.Name, &p.Type, &p.Priority, &p.Active,
		&p.StartAt, &p.EndAt, &p.Conditions, &p.Actions,
		&p.CouponBound, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("promotion_repo: find by id: %w", err)
	}
	return p, nil
}

func (r *PromotionRepo) ListActive(ctx context.Context, typ promotion.PromotionType) ([]promotion.Promotion, error) {
	const q = `SELECT id, name, type, priority, active, start_at, end_at,
		conditions, actions, coupon_bound, created_at, updated_at
		FROM promotions WHERE type = $1 AND active = true
		ORDER BY priority ASC, created_at ASC`

	rows, err := r.query(ctx, q, string(typ))
	if err != nil {
		return nil, fmt.Errorf("promotion_repo: list active: %w", err)
	}
	defer rows.Close()

	var result []promotion.Promotion
	for rows.Next() {
		var p promotion.Promotion
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Type, &p.Priority, &p.Active,
			&p.StartAt, &p.EndAt, &p.Conditions, &p.Actions,
			&p.CouponBound, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("promotion_repo: list scan: %w", err)
		}
		result = append(result, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("promotion_repo: list rows: %w", err)
	}
	return result, nil
}

func (r *PromotionRepo) Save(ctx context.Context, p *promotion.Promotion) error {
	if p == nil {
		return fmt.Errorf("promotion_repo: save: promotion must not be nil")
	}
	const q = `INSERT INTO promotions
		(id, name, type, priority, active, start_at, end_at,
		 conditions, actions, coupon_bound, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (id) DO UPDATE SET
			name         = EXCLUDED.name,
			type         = EXCLUDED.type,
			priority     = EXCLUDED.priority,
			active       = EXCLUDED.active,
			start_at     = EXCLUDED.start_at,
			end_at       = EXCLUDED.end_at,
			conditions   = EXCLUDED.conditions,
			actions      = EXCLUDED.actions,
			coupon_bound = EXCLUDED.coupon_bound,
			updated_at   = EXCLUDED.updated_at`

	_, err := r.exec(ctx, q,
		p.ID, p.Name, string(p.Type), p.Priority, p.Active,
		p.StartAt, p.EndAt, p.Conditions, p.Actions,
		p.CouponBound, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("promotion_repo: save: %w", err)
	}
	return nil
}

func (r *PromotionRepo) Delete(ctx context.Context, id string) error {
	const q = `DELETE FROM promotions WHERE id = $1`
	res, err := r.exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("promotion_repo: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("promotion_repo: delete rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("promotion_repo: delete: promotion %s not found", id)
	}
	return nil
}
