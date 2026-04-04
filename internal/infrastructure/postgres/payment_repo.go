package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/lib/pq"
)

// Compile-time check that PaymentRepo implements payment.PaymentRepository.
var _ payment.PaymentRepository = (*PaymentRepo)(nil)

// PaymentRepo implements payment.PaymentRepository using PostgreSQL.
type PaymentRepo struct {
	db *sql.DB
}

// NewPaymentRepo returns a new PaymentRepo backed by db.
func NewPaymentRepo(db *sql.DB) *PaymentRepo {
	return &PaymentRepo{db: db}
}

// hydratePayment reads a payment row from a *sql.Row.
func hydratePayment(row *sql.Row) (*payment.Payment, error) {
	var id, orderID string
	var status, method string
	var amount int64
	var currency string
	var providerRef sql.NullString
	var createdAt, updatedAt time.Time
	err := row.Scan(&id, &orderID, &method, &status,
		&amount, &currency, &providerRef, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	money, err := shared.NewMoney(amount, currency)
	if err != nil {
		return nil, fmt.Errorf("payment_repo: amount money: %w", err)
	}
	var ref string
	if providerRef.Valid {
		ref = providerRef.String
	}
	return payment.NewPaymentFromDB(id, orderID, payment.PaymentMethod(method), status, money, ref, createdAt, updatedAt)
}

const paymentColumns = `id, order_id, method, status, amount, currency, provider_ref, created_at, updated_at`

// FindByID returns a payment by its ID.
// Returns (nil, nil) when not found.
func (r *PaymentRepo) FindByID(ctx context.Context, id string) (*payment.Payment, error) {
	if id == "" {
		return nil, fmt.Errorf("payment_repo: find: empty id")
	}
	q := `SELECT ` + paymentColumns + ` FROM payments WHERE id = $1`
	p, err := hydratePayment(r.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("payment_repo: find by id: %w", err)
	}
	return p, nil
}

// FindByOrderID returns the payment for a given order.
// Returns (nil, nil) when no payment exists for the order.
func (r *PaymentRepo) FindByOrderID(ctx context.Context, orderID string) (*payment.Payment, error) {
	if orderID == "" {
		return nil, fmt.Errorf("payment_repo: find by order: empty order id")
	}
	q := `SELECT ` + paymentColumns + ` FROM payments WHERE order_id = $1`
	p, err := hydratePayment(r.db.QueryRowContext(ctx, q, orderID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("payment_repo: find by order id: %w", err)
	}
	return p, nil
}

// Create persists a new payment.
func (r *PaymentRepo) Create(ctx context.Context, p *payment.Payment) error {
	if p == nil {
		return fmt.Errorf("payment_repo: create: payment must not be nil")
	}
	const q = `INSERT INTO payments (id, order_id, method, status, amount, currency, provider_ref, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	var ref sql.NullString
	if p.ProviderRef != "" {
		ref = sql.NullString{String: p.ProviderRef, Valid: true}
	}
	_, err := r.db.ExecContext(ctx, q,
		p.ID, p.OrderID, string(p.Method), string(p.Status()),
		p.Amount.Amount(), p.Amount.Currency(), ref,
		p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return apperror.Conflict("payment for this order already exists")
		}
		return fmt.Errorf("payment_repo: create: %w", err)
	}
	return nil
}

// UpdateStatus updates the status, provider_ref, and updated_at of a payment.
// Uses optimistic locking via updated_at to detect concurrent modifications.
func (r *PaymentRepo) UpdateStatus(ctx context.Context, p *payment.Payment, prevUpdatedAt time.Time) error {
	if p == nil {
		return fmt.Errorf("payment_repo: update status: payment must not be nil")
	}
	const q = `UPDATE payments SET status = $1, provider_ref = $2, updated_at = $3 WHERE id = $4 AND updated_at = $5`
	var ref sql.NullString
	if p.ProviderRef != "" {
		ref = sql.NullString{String: p.ProviderRef, Valid: true}
	}
	res, err := r.db.ExecContext(ctx, q, string(p.Status()), ref, p.UpdatedAt, p.ID, prevUpdatedAt)
	if err != nil {
		return fmt.Errorf("payment_repo: update status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("payment_repo: rows affected: %w", err)
	}
	if n == 0 {
		return apperror.Conflict("payment was modified concurrently")
	}
	return nil
}
