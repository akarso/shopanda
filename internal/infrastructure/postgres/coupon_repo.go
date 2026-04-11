package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/promotion"
)

var _ promotion.CouponRepository = (*CouponRepo)(nil)

// CouponRepo implements promotion.CouponRepository using PostgreSQL.
type CouponRepo struct {
	db *sql.DB
	tx *sql.Tx
}

// NewCouponRepo returns a new CouponRepo backed by db.
func NewCouponRepo(db *sql.DB) (*CouponRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewCouponRepo: nil *sql.DB")
	}
	return &CouponRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
func (r *CouponRepo) WithTx(tx *sql.Tx) *CouponRepo {
	return &CouponRepo{db: r.db, tx: tx}
}

func (r *CouponRepo) queryRow(ctx context.Context, q string, args ...interface{}) *sql.Row {
	if r.tx != nil {
		return r.tx.QueryRowContext(ctx, q, args...)
	}
	return r.db.QueryRowContext(ctx, q, args...)
}

func (r *CouponRepo) query(ctx context.Context, q string, args ...interface{}) (*sql.Rows, error) {
	if r.tx != nil {
		return r.tx.QueryContext(ctx, q, args...)
	}
	return r.db.QueryContext(ctx, q, args...)
}

func (r *CouponRepo) exec(ctx context.Context, q string, args ...interface{}) (sql.Result, error) {
	if r.tx != nil {
		return r.tx.ExecContext(ctx, q, args...)
	}
	return r.db.ExecContext(ctx, q, args...)
}

func (r *CouponRepo) FindByCode(ctx context.Context, code string) (*promotion.Coupon, error) {
	const q = `SELECT id, code, promotion_id, usage_limit, usage_count,
		active, created_at, updated_at
		FROM coupons WHERE code = $1`

	c := &promotion.Coupon{}
	err := r.queryRow(ctx, q, code).Scan(
		&c.ID, &c.Code, &c.PromotionID, &c.UsageLimit, &c.UsageCount,
		&c.Active, &c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("coupon_repo: find by code: %w", err)
	}
	return c, nil
}

func (r *CouponRepo) FindByID(ctx context.Context, id string) (*promotion.Coupon, error) {
	const q = `SELECT id, code, promotion_id, usage_limit, usage_count,
		active, created_at, updated_at
		FROM coupons WHERE id = $1`

	c := &promotion.Coupon{}
	err := r.queryRow(ctx, q, id).Scan(
		&c.ID, &c.Code, &c.PromotionID, &c.UsageLimit, &c.UsageCount,
		&c.Active, &c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("coupon_repo: find by id: %w", err)
	}
	return c, nil
}

func (r *CouponRepo) ListByPromotion(ctx context.Context, promotionID string) ([]promotion.Coupon, error) {
	const q = `SELECT id, code, promotion_id, usage_limit, usage_count,
		active, created_at, updated_at
		FROM coupons WHERE promotion_id = $1
		ORDER BY created_at ASC`

	rows, err := r.query(ctx, q, promotionID)
	if err != nil {
		return nil, fmt.Errorf("coupon_repo: list by promotion: %w", err)
	}
	defer rows.Close()

	var result []promotion.Coupon
	for rows.Next() {
		var c promotion.Coupon
		if err := rows.Scan(
			&c.ID, &c.Code, &c.PromotionID, &c.UsageLimit, &c.UsageCount,
			&c.Active, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("coupon_repo: list scan: %w", err)
		}
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("coupon_repo: list rows: %w", err)
	}
	return result, nil
}

func (r *CouponRepo) Save(ctx context.Context, c *promotion.Coupon) error {
	if c == nil {
		return fmt.Errorf("coupon_repo: save: coupon must not be nil")
	}
	const q = `INSERT INTO coupons
		(id, code, promotion_id, usage_limit, usage_count, active, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (id) DO UPDATE SET
			code         = EXCLUDED.code,
			promotion_id = EXCLUDED.promotion_id,
			usage_limit  = EXCLUDED.usage_limit,
			usage_count  = EXCLUDED.usage_count,
			active       = EXCLUDED.active,
			updated_at   = EXCLUDED.updated_at`

	_, err := r.exec(ctx, q,
		c.ID, c.Code, c.PromotionID, c.UsageLimit, c.UsageCount,
		c.Active, c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("coupon_repo: save: %w", err)
	}
	return nil
}

func (r *CouponRepo) Delete(ctx context.Context, id string) error {
	const q = `DELETE FROM coupons WHERE id = $1`
	res, err := r.exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("coupon_repo: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("coupon_repo: delete rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("coupon_repo: delete: coupon %s not found", id)
	}
	return nil
}
