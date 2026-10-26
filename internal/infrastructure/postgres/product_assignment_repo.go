package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/jackc/pgx/v5/pgconn"
)

// Compile-time check that ProductAssignmentRepo implements assignment.Repository.
var _ assignment.Repository = (*ProductAssignmentRepo)(nil)

const (
	productAssignmentsChildCurrentIndex = "product_assignments_child_current"
	productAssignmentsPK                = "product_assignments_pkey"
	productAssignmentSelectColumns      = `id, parent_order_item_id, child_order_item_id, status, created_at, revoked_at`
)

// ProductAssignmentRepo implements assignment.Repository using PostgreSQL.
// Rows are instance bindings only; they do not change Purchasable or
// visibility, and they do not resolve order lines.
//
// WithTx is adapter-only. The domain Repository port does not declare it.
type ProductAssignmentRepo struct {
	db *sql.DB
	tx *sql.Tx
}

type productAssignmentQueryer interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// NewProductAssignmentRepo returns a new ProductAssignmentRepo backed by db.
func NewProductAssignmentRepo(db *sql.DB) (*ProductAssignmentRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewProductAssignmentRepo: nil *sql.DB")
	}
	return &ProductAssignmentRepo{db: db}, nil
}

// WithTx returns a repo bound to the given transaction.
// This method is postgres-only; callers type-assert the concrete adapter.
func (r *ProductAssignmentRepo) WithTx(tx *sql.Tx) assignment.Repository {
	return &ProductAssignmentRepo{db: r.db, tx: tx}
}

func (r *ProductAssignmentRepo) queryer() productAssignmentQueryer {
	if r.tx != nil {
		return r.tx
	}
	return r.db
}

func mapProductAssignmentExecError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23505":
		if pgErr.ConstraintName == productAssignmentsChildCurrentIndex {
			return apperror.Conflict("child already has an active assignment")
		}
		if pgErr.ConstraintName == productAssignmentsPK {
			return apperror.Conflict("assignment already exists")
		}
		return apperror.Conflict("product assignment conflict")
	case "23514":
		return apperror.Validation("product assignment constraint violation")
	case "22P02":
		return apperror.Validation("invalid assignment id")
	default:
		return err
	}
}

// Insert writes a new assignment.
func (r *ProductAssignmentRepo) Insert(ctx context.Context, a *assignment.ProductAssignment) error {
	if a == nil {
		return fmt.Errorf("product_assignment_repo: insert: assignment must not be nil")
	}
	if err := a.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	const q = `INSERT INTO product_assignments
		(id, parent_order_item_id, child_order_item_id, status, created_at, revoked_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := r.queryer().ExecContext(ctx, q,
		a.ID,
		nullIfEmpty(a.ParentOrderItemID),
		a.ChildOrderItemID,
		string(a.Status()),
		a.CreatedAt,
		nullTime(a.RevokedAt),
	)
	if err != nil {
		return mapProductAssignmentExecError(err)
	}
	a.AcceptStoredStatus()
	return nil
}

// Update persists a status transition only while the row is still in StoredStatus.
func (r *ProductAssignmentRepo) Update(ctx context.Context, a *assignment.ProductAssignment) error {
	if a == nil {
		return fmt.Errorf("product_assignment_repo: update: assignment must not be nil")
	}
	if err := a.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	if !a.AssigningParent() && a.ParentChanged() {
		return apperror.Validation("product assignment: parent cannot be rewritten")
	}
	var (
		q    string
		args []interface{}
	)
	if a.AssigningParent() {
		q = `UPDATE product_assignments
			SET status = $1,
				parent_order_item_id = $2,
				revoked_at = COALESCE(revoked_at, $3)
			WHERE id = $4 AND status = $5`
		args = []interface{}{
			string(a.Status()),
			nullIfEmpty(a.ParentOrderItemID),
			nullTime(a.RevokedAt),
			a.ID,
			string(a.StoredStatus()),
		}
	} else {
		q = `UPDATE product_assignments
			SET status = $1,
				revoked_at = COALESCE(revoked_at, $2)
			WHERE id = $3 AND status = $4`
		args = []interface{}{
			string(a.Status()),
			nullTime(a.RevokedAt),
			a.ID,
			string(a.StoredStatus()),
		}
	}
	result, err := r.queryer().ExecContext(ctx, q, args...)
	if err != nil {
		return mapProductAssignmentExecError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("product_assignment_repo: update rows: %w", err)
	}
	if n == 0 {
		var one int
		err := r.queryer().QueryRowContext(ctx, `SELECT 1 FROM product_assignments WHERE id = $1`, a.ID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return apperror.NotFound("product assignment not found")
		}
		if err != nil {
			return fmt.Errorf("product_assignment_repo: update status: %w", err)
		}
		return assignment.ErrStatusConflict
	}
	a.AcceptStoredStatus()
	return nil
}

// FindByID returns the assignment, or (nil, nil) when missing.
func (r *ProductAssignmentRepo) FindByID(ctx context.Context, assignmentID string) (*assignment.ProductAssignment, error) {
	if assignmentID == "" {
		return nil, fmt.Errorf("product_assignment_repo: find: empty id")
	}
	if !id.IsUUID(assignmentID) {
		return nil, apperror.Validation("invalid assignment id")
	}
	const q = `SELECT ` + productAssignmentSelectColumns + ` FROM product_assignments WHERE id = $1`
	a, err := scanProductAssignment(r.queryer().QueryRowContext(ctx, q, assignmentID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		if mapped := mapProductAssignmentExecError(err); mapped != err {
			return nil, mapped
		}
		return nil, fmt.Errorf("product_assignment_repo: find by id: %w", err)
	}
	return a, nil
}

// FindCurrentByChildID returns the live row for the child, or (nil, nil).
func (r *ProductAssignmentRepo) FindCurrentByChildID(ctx context.Context, childOrderItemID string) (*assignment.ProductAssignment, error) {
	if childOrderItemID == "" {
		return nil, fmt.Errorf("product_assignment_repo: find current: empty child order item id")
	}
	const q = `SELECT ` + productAssignmentSelectColumns + `
		FROM product_assignments
		WHERE child_order_item_id = $1 AND status IN ('unassigned', 'assigned')`
	a, err := scanProductAssignment(r.queryer().QueryRowContext(ctx, q, childOrderItemID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("product_assignment_repo: find current: %w", err)
	}
	return a, nil
}

// ListByChildID returns history for the child, newest first. The slice is
// non-nil when the query succeeds.
func (r *ProductAssignmentRepo) ListByChildID(ctx context.Context, childOrderItemID string) ([]assignment.ProductAssignment, error) {
	if childOrderItemID == "" {
		return nil, fmt.Errorf("product_assignment_repo: list: empty child order item id")
	}
	const q = `SELECT ` + productAssignmentSelectColumns + `
		FROM product_assignments
		WHERE child_order_item_id = $1
		ORDER BY created_at DESC, id`
	rows, err := r.queryer().QueryContext(ctx, q, childOrderItemID)
	if err != nil {
		return nil, fmt.Errorf("product_assignment_repo: list: %w", err)
	}
	defer rows.Close()
	out := make([]assignment.ProductAssignment, 0)
	for rows.Next() {
		a, err := scanProductAssignment(rows)
		if err != nil {
			return nil, fmt.Errorf("product_assignment_repo: list scan: %w", err)
		}
		out = append(out, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("product_assignment_repo: list rows: %w", err)
	}
	return out, nil
}

type productAssignmentRow interface {
	Scan(dest ...interface{}) error
}

func scanProductAssignment(row productAssignmentRow) (*assignment.ProductAssignment, error) {
	var (
		id, child, status string
		parent            sql.NullString
		created           time.Time
		revoked           sql.NullTime
	)
	if err := row.Scan(&id, &parent, &child, &status, &created, &revoked); err != nil {
		return nil, err
	}
	a := &assignment.ProductAssignment{
		ID:               id,
		ChildOrderItemID: child,
		CreatedAt:        created.UTC(),
	}
	if parent.Valid {
		a.ParentOrderItemID = parent.String
	}
	if revoked.Valid {
		t := revoked.Time.UTC()
		a.RevokedAt = &t
	}
	if err := a.SetStatusFromDB(status); err != nil {
		return nil, err
	}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return a, nil
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
