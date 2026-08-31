package jobs

import "context"

// Admin is a write-only port for admin-triggered job lifecycle actions —
// separate from Queue (the worker's own dequeue/complete/fail lifecycle)
// and Reader (read-only introspection) because retry/cancel are operator
// corrections, not something the worker does on its own. Kept
// Postgres-queue-only for the same reason Reader is: a broker-backed Queue
// implementation has no queryable/updatable job table the way the
// Postgres implementation does.
type Admin interface {
	// Retry resets a failed job back to pending (attempts reset to 0,
	// run_at set to now) so the worker picks it up again. Returns a
	// *apperror.Error with CodeNotFound if no job with that ID exists, or
	// CodeConflict if the job exists but isn't currently failed.
	Retry(ctx context.Context, id string) error

	// Cancel marks a pending job cancelled — it will never be dequeued.
	// There is no in-flight cancellation: a processing job cannot be
	// cancelled. Returns a *apperror.Error with CodeNotFound if no job
	// with that ID exists, or CodeConflict if the job exists but isn't
	// currently pending.
	Cancel(ctx context.Context, id string) error
}
