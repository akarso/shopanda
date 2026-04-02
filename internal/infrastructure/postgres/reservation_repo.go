package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// Compile-time check that ReservationRepo implements inventory.ReservationRepository.
var _ inventory.ReservationRepository = (*ReservationRepo)(nil)

// ReservationRepo implements inventory.ReservationRepository using PostgreSQL.
type ReservationRepo struct {
	db *sql.DB
}

// NewReservationRepo returns a new ReservationRepo backed by db.
func NewReservationRepo(db *sql.DB) *ReservationRepo {
	return &ReservationRepo{db: db}
}

// Reserve atomically decrements stock and creates a reservation within a transaction.
func (r *ReservationRepo) Reserve(ctx context.Context, res *inventory.Reservation) error {
	if res == nil {
		return fmt.Errorf("reservation_repo: reserve: reservation must not be nil")
	}
	if res.Status != inventory.ReservationActive {
		return fmt.Errorf("reservation_repo: reserve: status must be active, got %q", res.Status)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reservation_repo: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Decrement stock atomically, fail if insufficient.
	const decr = `UPDATE stock SET quantity = quantity - $1, updated_at = $2
		WHERE variant_id = $3 AND quantity >= $1`
	result, err := tx.ExecContext(ctx, decr, res.Quantity, time.Now().UTC(), res.VariantID)
	if err != nil {
		return fmt.Errorf("reservation_repo: decrement stock: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("reservation_repo: rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.New(apperror.CodeConflict, "insufficient stock")
	}

	// Insert reservation.
	const ins = `INSERT INTO reservations (id, variant_id, quantity, status, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err = tx.ExecContext(ctx, ins,
		res.ID, res.VariantID, res.Quantity, string(res.Status), res.ExpiresAt, res.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("reservation_repo: insert reservation: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reservation_repo: commit: %w", err)
	}
	return nil
}

// Release cancels an active reservation and restores stock.
func (r *ReservationRepo) Release(ctx context.Context, reservationID string) error {
	if reservationID == "" {
		return fmt.Errorf("reservation_repo: release: empty reservation id")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reservation_repo: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Mark reservation as released, returning its variant_id and quantity.
	const upd = `UPDATE reservations SET status = 'released'
		WHERE id = $1 AND status = 'active'
		RETURNING variant_id, quantity`
	var variantID string
	var qty int
	err = tx.QueryRowContext(ctx, upd, reservationID).Scan(&variantID, &qty)
	if errors.Is(err, sql.ErrNoRows) {
		return apperror.NotFound("reservation not found or not active")
	}
	if err != nil {
		return fmt.Errorf("reservation_repo: update reservation: %w", err)
	}

	// Restore stock.
	const incr = `UPDATE stock SET quantity = quantity + $1, updated_at = $2
		WHERE variant_id = $3`
	_, err = tx.ExecContext(ctx, incr, qty, time.Now().UTC(), variantID)
	if err != nil {
		return fmt.Errorf("reservation_repo: restore stock: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reservation_repo: commit: %w", err)
	}
	return nil
}

// Confirm marks a reservation as confirmed without restoring stock.
func (r *ReservationRepo) Confirm(ctx context.Context, reservationID string) error {
	if reservationID == "" {
		return fmt.Errorf("reservation_repo: confirm: empty reservation id")
	}

	const q = `UPDATE reservations SET status = 'confirmed'
		WHERE id = $1 AND status = 'active'`
	result, err := r.db.ExecContext(ctx, q, reservationID)
	if err != nil {
		return fmt.Errorf("reservation_repo: confirm: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("reservation_repo: rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("reservation not found or not active")
	}
	return nil
}

// FindByID returns a reservation by its ID.
func (r *ReservationRepo) FindByID(ctx context.Context, id string) (*inventory.Reservation, error) {
	if id == "" {
		return nil, fmt.Errorf("reservation_repo: find: empty id")
	}
	const q = `SELECT id, variant_id, quantity, status, expires_at, created_at
		FROM reservations WHERE id = $1`
	var res inventory.Reservation
	var status string
	err := r.db.QueryRowContext(ctx, q, id).Scan(
		&res.ID, &res.VariantID, &res.Quantity, &status, &res.ExpiresAt, &res.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reservation_repo: find: %w", err)
	}
	res.Status = inventory.ReservationStatus(status)
	return &res, nil
}

// ListActiveByVariantID returns all active reservations for a variant.
func (r *ReservationRepo) ListActiveByVariantID(ctx context.Context, variantID string) ([]inventory.Reservation, error) {
	if variantID == "" {
		return nil, fmt.Errorf("reservation_repo: list active: empty variant id")
	}
	const q = `SELECT id, variant_id, quantity, status, expires_at, created_at
		FROM reservations WHERE variant_id = $1 AND status = 'active'
		ORDER BY created_at`

	rows, err := r.db.QueryContext(ctx, q, variantID)
	if err != nil {
		return nil, fmt.Errorf("reservation_repo: list active: %w", err)
	}
	defer rows.Close()

	var reservations []inventory.Reservation
	for rows.Next() {
		var res inventory.Reservation
		var status string
		if err := rows.Scan(&res.ID, &res.VariantID, &res.Quantity, &status, &res.ExpiresAt, &res.CreatedAt); err != nil {
			return nil, fmt.Errorf("reservation_repo: list scan: %w", err)
		}
		res.Status = inventory.ReservationStatus(status)
		reservations = append(reservations, res)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reservation_repo: list rows: %w", err)
	}
	return reservations, nil
}

// ReleaseExpiredBefore atomically releases all active reservations that expired
// before cutoff and restores their quantities to stock.
func (r *ReservationRepo) ReleaseExpiredBefore(ctx context.Context, cutoff time.Time) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("reservation_repo: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Mark expired active reservations as released and collect their variant/qty.
	const upd = `UPDATE reservations SET status = 'released'
		WHERE status = 'active' AND expires_at < $1
		RETURNING variant_id, quantity`
	rows, err := tx.QueryContext(ctx, upd, cutoff)
	if err != nil {
		return 0, fmt.Errorf("reservation_repo: release expired: %w", err)
	}
	defer rows.Close()

	type restore struct {
		variantID string
		quantity  int
	}
	var restores []restore
	for rows.Next() {
		var r restore
		if err := rows.Scan(&r.variantID, &r.quantity); err != nil {
			return 0, fmt.Errorf("reservation_repo: scan expired: %w", err)
		}
		restores = append(restores, r)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("reservation_repo: rows expired: %w", err)
	}

	// Restore stock for each released reservation.
	const incr = `UPDATE stock SET quantity = quantity + $1, updated_at = $2
		WHERE variant_id = $3`
	now := time.Now().UTC()
	for _, rs := range restores {
		if _, err := tx.ExecContext(ctx, incr, rs.quantity, now, rs.variantID); err != nil {
			return 0, fmt.Errorf("reservation_repo: restore stock: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("reservation_repo: commit: %w", err)
	}
	return len(restores), nil
}
