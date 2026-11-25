package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/voucher"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

var _ voucher.Repository = (*VoucherRepo)(nil)

const (
	vouchersPK                  = "vouchers_pkey"
	vouchersCodeUniq            = "vouchers_code_unique"
	vouchersOrderVariantSeqUniq = "vouchers_order_variant_seq_unique"
	voucherSelectColumns        = `id, code, order_id, order_item_id, product_id, variant_id, customer_id,
		seq, max_redemptions, redeemed_count, voided_at, issued_at, created_at`
)

// VoucherRepo implements voucher.Repository using PostgreSQL.
type VoucherRepo struct {
	db *sql.DB
	tx *sql.Tx
}

type voucherQueryer interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// NewVoucherRepo returns a new VoucherRepo backed by db.
func NewVoucherRepo(db *sql.DB) (*VoucherRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewVoucherRepo: nil *sql.DB")
	}
	return &VoucherRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
func (r *VoucherRepo) WithTx(tx *sql.Tx) voucher.Repository {
	return &VoucherRepo{db: r.db, tx: tx}
}

func (r *VoucherRepo) queryer() voucherQueryer {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

func mapVoucherExecError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23505":
		if pgErr.ConstraintName == vouchersOrderVariantSeqUniq {
			return apperror.Wrap(apperror.CodeConflict, "voucher already exists for order line sequence", voucher.ErrAlreadyIssued)
		}
		if pgErr.ConstraintName == vouchersCodeUniq {
			return voucher.ErrCodeTaken
		}
		if pgErr.ConstraintName == vouchersPK {
			return apperror.Conflict("voucher already exists")
		}
		return apperror.Conflict("voucher conflict")
	case "23503":
		return apperror.NotFound("related order or product not found")
	case "23514":
		return apperror.Validation("voucher constraint violation")
	case "22P02":
		return apperror.Validation("invalid voucher id")
	default:
		return err
	}
}

func scanVoucher(scanner interface {
	Scan(dest ...interface{}) error
}) (*voucher.Voucher, error) {
	var (
		v      voucher.Voucher
		voided sql.NullTime
	)
	err := scanner.Scan(
		&v.ID, &v.Code, &v.OrderID, &v.OrderItemID, &v.ProductID, &v.VariantID, &v.CustomerID,
		&v.Seq, &v.MaxRedemptions, &v.RedeemedCount, &voided, &v.IssuedAt, &v.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if voided.Valid {
		t := voided.Time.UTC()
		v.VoidedAt = &t
	}
	v.IssuedAt = v.IssuedAt.UTC()
	v.CreatedAt = v.CreatedAt.UTC()
	return &v, nil
}

// Insert writes a new voucher. ON CONFLICT DO NOTHING on order/variant/seq
// maps to Conflict so concurrent order.paid is idempotent without aborting TX.
func (r *VoucherRepo) Insert(ctx context.Context, v *voucher.Voucher) error {
	if v == nil {
		return fmt.Errorf("voucher_repo: insert: voucher must not be nil")
	}
	if err := v.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	const q = `INSERT INTO vouchers
		(id, code, order_id, order_item_id, product_id, variant_id, customer_id,
		 seq, max_redemptions, redeemed_count, voided_at, issued_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (order_id, variant_id, seq) DO NOTHING`
	var voided interface{}
	if v.VoidedAt != nil {
		voided = v.VoidedAt.UTC()
	}
	res, err := r.queryer().ExecContext(ctx, q,
		v.ID, voucher.NormalizeCode(v.Code), v.OrderID, v.OrderItemID, v.ProductID, v.VariantID, v.CustomerID,
		v.Seq, v.MaxRedemptions, v.RedeemedCount, voided, v.IssuedAt, v.CreatedAt,
	)
	if err != nil {
		return mapVoucherExecError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("voucher_repo: insert rows: %w", err)
	}
	if n == 0 {
		return apperror.Wrap(apperror.CodeConflict, "voucher already exists for order line sequence", voucher.ErrAlreadyIssued)
	}
	return nil
}

// UpdateCounts CAS-updates redeemed_count.
// Missing / void rows → NotFound; lost CAS → Conflict.
func (r *VoucherRepo) UpdateCounts(ctx context.Context, id string, redeemedCount, expectedCount int) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return apperror.Validation("voucher id must not be empty")
	}
	const q = `UPDATE vouchers
		SET redeemed_count = $1
		WHERE id = $2 AND redeemed_count = $3 AND voided_at IS NULL`
	res, err := r.queryer().ExecContext(ctx, q, redeemedCount, id, expectedCount)
	if err != nil {
		return mapVoucherExecError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("voucher_repo: update counts rows: %w", err)
	}
	if n == 1 {
		return nil
	}
	cur, findErr := r.FindByID(ctx, id)
	if findErr != nil {
		return findErr
	}
	if cur == nil || cur.VoidedAt != nil {
		return apperror.NotFound("voucher not found or void")
	}
	return apperror.Conflict("voucher redeem count conflict")
}

// Void stamps voided_at when not already void.
func (r *VoucherRepo) Void(ctx context.Context, id string, at time.Time) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return apperror.Validation("voucher id must not be empty")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	const q = `UPDATE vouchers SET voided_at = $1 WHERE id = $2 AND voided_at IS NULL`
	res, err := r.queryer().ExecContext(ctx, q, at.UTC(), id)
	if err != nil {
		return mapVoucherExecError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("voucher_repo: void rows: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("voucher not found or already void")
	}
	return nil
}

// VoidForOrderItem voids every live voucher on the opaque order item key.
func (r *VoucherRepo) VoidForOrderItem(ctx context.Context, orderItemID string, at time.Time) ([]voucher.Voucher, error) {
	orderItemID = strings.TrimSpace(orderItemID)
	if orderItemID == "" {
		return nil, nil
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	qSel := `SELECT ` + voucherSelectColumns + `
		FROM vouchers WHERE order_item_id = $1 AND voided_at IS NULL
		ORDER BY seq`
	rows, err := r.queryer().QueryContext(ctx, qSel, orderItemID)
	if err != nil {
		return nil, fmt.Errorf("voucher_repo: void for order item list: %w", err)
	}
	defer rows.Close()
	live := make([]voucher.Voucher, 0)
	for rows.Next() {
		v, err := scanVoucher(rows)
		if err != nil {
			return nil, fmt.Errorf("voucher_repo: void for order item scan: %w", err)
		}
		live = append(live, *v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(live) == 0 {
		return nil, nil
	}
	const q = `UPDATE vouchers SET voided_at = $1
		WHERE order_item_id = $2 AND voided_at IS NULL`
	if _, err := r.queryer().ExecContext(ctx, q, at.UTC(), orderItemID); err != nil {
		return nil, mapVoucherExecError(err)
	}
	for i := range live {
		t := at.UTC()
		live[i].VoidedAt = &t
	}
	return live, nil
}

// LinkCustomer sets customer_id on guest vouchers for the order.
func (r *VoucherRepo) LinkCustomer(ctx context.Context, orderID, customerID string) error {
	orderID = strings.TrimSpace(orderID)
	customerID = strings.TrimSpace(customerID)
	if orderID == "" || customerID == "" {
		return apperror.Validation("order id and customer id are required")
	}
	const q = `UPDATE vouchers SET customer_id = $1
		WHERE order_id = $2 AND customer_id = ''`
	_, err := r.queryer().ExecContext(ctx, q, customerID, orderID)
	if err != nil {
		return mapVoucherExecError(err)
	}
	return nil
}

// FindByID returns the voucher or (nil, nil).
func (r *VoucherRepo) FindByID(ctx context.Context, id string) (*voucher.Voucher, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	q := `SELECT ` + voucherSelectColumns + ` FROM vouchers WHERE id = $1`
	v, err := scanVoucher(r.queryer().QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		if mapped := mapVoucherExecError(err); mapped != err {
			return nil, mapped
		}
		return nil, fmt.Errorf("voucher_repo: find by id: %w", err)
	}
	return v, nil
}

// FindByCode returns the voucher for a normalized code, or (nil, nil).
func (r *VoucherRepo) FindByCode(ctx context.Context, code string) (*voucher.Voucher, error) {
	code = voucher.NormalizeCode(code)
	if code == "" {
		return nil, nil
	}
	q := `SELECT ` + voucherSelectColumns + ` FROM vouchers WHERE code = $1`
	v, err := scanVoucher(r.queryer().QueryRowContext(ctx, q, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		if mapped := mapVoucherExecError(err); mapped != err {
			return nil, mapped
		}
		return nil, fmt.Errorf("voucher_repo: find by code: %w", err)
	}
	return v, nil
}

// List returns vouchers matching the filter, newest first.
func (r *VoucherRepo) List(ctx context.Context, f voucher.ListFilter) ([]voucher.Voucher, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	var (
		conds []string
		args  []interface{}
	)
	add := func(cond string, v interface{}) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if id := strings.TrimSpace(f.OrderID); id != "" {
		add("order_id = $%d", id)
	}
	if id := strings.TrimSpace(f.CustomerID); id != "" {
		add("customer_id = $%d", id)
	}
	if code := voucher.NormalizeCode(f.Code); code != "" {
		add("code = $%d", code)
	}
	switch strings.TrimSpace(f.Status) {
	case voucher.StatusVoid:
		conds = append(conds, "voided_at IS NOT NULL")
	case voucher.StatusIssued:
		conds = append(conds, "voided_at IS NULL AND redeemed_count = 0")
	case voucher.StatusPartiallyRedeemed:
		conds = append(conds, "voided_at IS NULL AND redeemed_count > 0 AND redeemed_count < max_redemptions")
	case voucher.StatusRedeemed:
		conds = append(conds, "voided_at IS NULL AND redeemed_count >= max_redemptions")
	case "":
		// no status filter
	default:
		return nil, apperror.Validation("unknown voucher status filter")
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, limit, offset)
	limIdx := len(args) - 1
	offIdx := len(args)
	q := fmt.Sprintf(`SELECT %s FROM vouchers %s ORDER BY issued_at DESC, id DESC LIMIT $%d OFFSET $%d`,
		voucherSelectColumns, where, limIdx, offIdx)

	rows, err := r.queryer().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("voucher_repo: list: %w", err)
	}
	defer rows.Close()

	out := make([]voucher.Voucher, 0)
	for rows.Next() {
		v, err := scanVoucher(rows)
		if err != nil {
			return nil, fmt.Errorf("voucher_repo: list scan: %w", err)
		}
		out = append(out, *v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("voucher_repo: list rows: %w", err)
	}
	return out, nil
}

// ListByOrderID returns all vouchers for an order, seq ascending.
func (r *VoucherRepo) ListByOrderID(ctx context.Context, orderID string) ([]voucher.Voucher, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return []voucher.Voucher{}, nil
	}
	q := `SELECT ` + voucherSelectColumns + `
		FROM vouchers WHERE order_id = $1
		ORDER BY variant_id, seq`
	rows, err := r.queryer().QueryContext(ctx, q, orderID)
	if err != nil {
		return nil, fmt.Errorf("voucher_repo: list by order: %w", err)
	}
	defer rows.Close()
	out := make([]voucher.Voucher, 0)
	for rows.Next() {
		v, err := scanVoucher(rows)
		if err != nil {
			return nil, fmt.Errorf("voucher_repo: list by order scan: %w", err)
		}
		out = append(out, *v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("voucher_repo: list by order rows: %w", err)
	}
	return out, nil
}
