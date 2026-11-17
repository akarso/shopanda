package pluginsdk

import (
	"context"
	"database/sql"

	"github.com/akarso/shopanda/internal/domain/jobs"
)

// ErrSkipOutcome tells the worker to skip platform Complete/Fail. Prefer
// returning this alone from a sync/job handler (or fence wrapper).
// Joining/wrapping a real failure with this sentinel still Fail's.
// Skipping without a fence Complete/Fail/Requeue leaves the row processing.
// Same sentinel as jobs.ErrSkipOutcome.
var ErrSkipOutcome = jobs.ErrSkipOutcome

// RequeueTx returns the Postgres reclaim *sql.Tx when OnRequeued / before-commit
// runs under RequeueWithBeforeCommit. Fence writes must use this TX (or be
// idempotent) so a failed jobs COMMIT does not leave an external release while
// the row stays processing. Same as jobs.RequeueTx.
func RequeueTx(ctx context.Context) (*sql.Tx, bool) {
	return jobs.RequeueTx(ctx)
}
