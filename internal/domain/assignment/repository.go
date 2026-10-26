package assignment

import (
	"context"
	"errors"
)

// ErrStatusConflict is returned when Update matches no row because the
// row's status is no longer StoredStatus. The in-memory transition must
// not be treated as saved.
var ErrStatusConflict = errors.New("product assignment: status conflict")

// Repository persists ProductAssignment rows. Transaction binding is not
// part of this port. WithTx is implemented on the postgres adapter only
// (postgres.ProductAssignmentRepo) so PR-1070 can insert in the same
// checkout transaction.
type Repository interface {
	// Insert writes a new assignment. A second current row for the same
	// child (status unassigned or assigned) is a conflict. Field
	// validation only; unit ids are not resolved here.
	Insert(ctx context.Context, a *ProductAssignment) error

	// Update persists a status transition only while the row is still in
	// StoredStatus. parent_order_item_id is written only on
	// unassigned → assigned. Any other in-memory parent change is
	// validation and is not written. revoked_at keeps a value that is
	// already stored, so a stale write cannot clear it. A row whose
	// status no longer matches is ErrStatusConflict.
	Update(ctx context.Context, a *ProductAssignment) error

	// FindByID returns the assignment, or (nil, nil) when missing.
	FindByID(ctx context.Context, id string) (*ProductAssignment, error)

	// FindCurrentByChildID returns the unassigned or assigned row for
	// childOrderItemID, or (nil, nil) when the child has none (including
	// when only revoked history exists).
	FindCurrentByChildID(ctx context.Context, childOrderItemID string) (*ProductAssignment, error)

	// ListByChildID returns every row for childOrderItemID, newest first
	// (revoked history included). Always returns a non-nil slice.
	ListByChildID(ctx context.Context, childOrderItemID string) ([]ProductAssignment, error)
}
