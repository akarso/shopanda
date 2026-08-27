package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// Compile-time check that ReservationRepo implements inventory.ReservationRepository.
var _ inventory.ReservationRepository = (*ReservationRepo)(nil)

// defaultReservationExpiryBatchSize is ReservationRepo's default
// reservationExpiryBatchSize (see that field's doc comment).
const defaultReservationExpiryBatchSize = 500

// ReservationRepo implements inventory.ReservationRepository using PostgreSQL.
type ReservationRepo struct {
	db *sql.DB

	// reservationExpiryBatchSize bounds how many reservations
	// ReleaseExpiredBefore processes per transaction. See its doc comment
	// for why an unbounded single transaction is unsafe here. An
	// instance field, not a package-level var: a package-level var mutated
	// by a test-only setter is shared, unsynchronized state that any test
	// touching it — including a future one added with t.Parallel(), a
	// common way to speed up this package's DB-integration suite — could
	// race on or silently stomp another test's expectations. Each test
	// already constructs its own *ReservationRepo, so scoping the override
	// to one instance makes cross-test interference structurally
	// impossible instead of merely unlikely under today's sequential
	// execution. See SetReservationExpiryBatchSizeForTest.
	reservationExpiryBatchSize int
}

// NewReservationRepo returns a new ReservationRepo backed by db.
func NewReservationRepo(db *sql.DB) (*ReservationRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewReservationRepo: nil *sql.DB")
	}
	return &ReservationRepo{db: db, reservationExpiryBatchSize: defaultReservationExpiryBatchSize}, nil
}

// SetReservationExpiryBatchSizeForTest overrides the batch size this repo
// instance uses for ReleaseExpiredBefore, for tests only — lets a test
// exercise the multi-batch loop at a manageable scale instead of seeding
// 500+ rows, without any shared state across tests (see the field's doc
// comment).
func (r *ReservationRepo) SetReservationExpiryBatchSizeForTest(n int) {
	r.reservationExpiryBatchSize = n
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
	if !id.IsValid(reservationID) {
		return apperror.NotFound("reservation not found or not active")
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
	if !id.IsValid(reservationID) {
		return apperror.NotFound("reservation not found or not active")
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
// Returns (nil, nil) when not found, including when reservationID is not a
// well-formed UUID (it can never match a row on the uuid column).
func (r *ReservationRepo) FindByID(ctx context.Context, reservationID string) (*inventory.Reservation, error) {
	if reservationID == "" {
		return nil, fmt.Errorf("reservation_repo: find: empty id")
	}
	if !id.IsValid(reservationID) {
		return nil, nil
	}
	const q = `SELECT id, variant_id, quantity, status, expires_at, created_at
		FROM reservations WHERE id = $1`
	var res inventory.Reservation
	var status string
	err := r.db.QueryRowContext(ctx, q, reservationID).Scan(
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

// reservationExpiryStatementTimeout is a per-batch-transaction backstop
// (via SET LOCAL, scoped to just that transaction) against a single batch
// running away — e.g. a missing index turning the bounded LIMIT query into
// a full scan — independent of whatever deadline the caller's ctx carries.
const reservationExpiryStatementTimeout = "30s"

// ReleaseExpiredBefore atomically releases active reservations that expired
// before cutoff, restoring their quantities to stock. Processes in bounded
// batches — one transaction per up-to-reservationExpiryBatchSize rows,
// SELECT ... FOR UPDATE SKIP LOCKED — rather than one single transaction
// over the entire expired set:
//
//   - Bounded lock duration: an unbounded single transaction would hold
//     locks on every touched stock row for however long the full sweep
//     takes, blocking concurrent checkout Reserve calls on those same
//     variants for that entire duration. This matters most on a first run
//     after deploy, which is expected to sweep a potentially large
//     historical backlog (see RUNBOOK.md) — with batching, no checkout is
//     ever blocked longer than one batch takes to commit.
//   - Bounded retry cost: a failure partway (timeout, connection drop,
//     process restart) only loses the batch in flight, not the whole
//     backlog — every already-committed batch stays committed.
//   - SKIP LOCKED also makes concurrent invocations of this method safe
//     (e.g. two worker processes both processing the reservation-expiry
//     job while a large sweep is still in progress on one of them): each
//     invocation's SELECT skips rows already locked by the other's
//     in-flight batch instead of blocking on or reprocessing them, so
//     overlapping runs partition the work instead of contending for it.
//
// Stops early — returning what it released so far, with a nil error — if
// ctx is cancelled or its deadline expires before every expired-as-of-
// cutoff row is drained. This is expected for a large backlog, not a
// failure: cutoff is recomputed fresh on the caller's next invocation, so
// the remaining backlog is picked up there. Callers that want to detect
// "stopped early vs. fully drained" can check ctx.Err() themselves after
// the call returns — same context object, so a deadline that fired is
// still observable there.
//
// If any released reservation's stock row no longer existed to receive the
// restored quantity back (the variant was deleted after the reservation
// was created), the count returned still includes it — the release itself
// is not in doubt — but the error return is a non-nil
// *inventory.OrphanedStockRestoreError describing which ones. Callers
// should log it, not treat it as cause to retry.
func (r *ReservationRepo) ReleaseExpiredBefore(ctx context.Context, cutoff time.Time) (int, error) {
	total := 0
	var orphaned []string
	for {
		if ctx.Err() != nil {
			break
		}
		n, batchOrphaned, err := r.releaseExpiredBatch(ctx, cutoff, r.reservationExpiryBatchSize)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				break
			}
			return total, err
		}
		total += n
		orphaned = append(orphaned, batchOrphaned...)
		if n < r.reservationExpiryBatchSize {
			break
		}
	}
	if len(orphaned) > 0 {
		const maxReportedIDs = 20
		ids := orphaned
		if len(ids) > maxReportedIDs {
			ids = ids[:maxReportedIDs]
		}
		return total, &inventory.OrphanedStockRestoreError{Count: len(orphaned), ReservationIDs: ids}
	}
	return total, nil
}

// releaseExpiredBatch releases at most limit expired reservations in one
// transaction, returning how many it released and the IDs of any whose
// stock restore no-opped (variant no longer exists).
func (r *ReservationRepo) releaseExpiredBatch(ctx context.Context, cutoff time.Time, limit int) (int, []string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("reservation_repo: begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "SET LOCAL statement_timeout = '"+reservationExpiryStatementTimeout+"'"); err != nil {
		return 0, nil, fmt.Errorf("reservation_repo: set statement_timeout: %w", err)
	}

	// FOR UPDATE SKIP LOCKED: skip rows a concurrent transaction (another
	// overlapping sweep) already holds, instead of blocking on them — see
	// the doc comment above.
	const sel = `SELECT id, variant_id, quantity FROM reservations
		WHERE status = 'active' AND expires_at < $1
		ORDER BY expires_at
		LIMIT $2
		FOR UPDATE SKIP LOCKED`
	rows, err := tx.QueryContext(ctx, sel, cutoff, limit)
	if err != nil {
		return 0, nil, fmt.Errorf("reservation_repo: select expired batch: %w", err)
	}

	type restore struct {
		id        string
		variantID string
		quantity  int
	}
	var restores []restore
	for rows.Next() {
		var rs restore
		if err := rows.Scan(&rs.id, &rs.variantID, &rs.quantity); err != nil {
			rows.Close()
			return 0, nil, fmt.Errorf("reservation_repo: scan expired batch: %w", err)
		}
		restores = append(restores, rs)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, nil, fmt.Errorf("reservation_repo: rows expired batch: %w", err)
	}
	rows.Close()

	if len(restores) == 0 {
		return 0, nil, nil
	}

	ids := make([]string, len(restores))
	for i, rs := range restores {
		ids[i] = rs.id
	}
	const upd = `UPDATE reservations SET status = 'released' WHERE id = ANY($1)`
	if _, err := tx.ExecContext(ctx, upd, ids); err != nil {
		return 0, nil, fmt.Errorf("reservation_repo: release expired batch: %w", err)
	}

	const incr = `UPDATE stock SET quantity = quantity + $1, updated_at = $2
		WHERE variant_id = $3`
	now := time.Now().UTC()
	var orphaned []string
	for _, rs := range restores {
		result, err := tx.ExecContext(ctx, incr, rs.quantity, now, rs.variantID)
		if err != nil {
			return 0, nil, fmt.Errorf("reservation_repo: restore stock: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return 0, nil, fmt.Errorf("reservation_repo: restore stock rows affected: %w", err)
		}
		if affected == 0 {
			orphaned = append(orphaned, rs.id)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, nil, fmt.Errorf("reservation_repo: commit: %w", err)
	}
	return len(restores), orphaned, nil
}
