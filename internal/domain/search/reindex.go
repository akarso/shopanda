package search

import (
	"context"
	"errors"
	"time"
)

// ErrRunNotProcessing is returned by RunStore.Finish when the run exists
// but is no longer "processing" — it was already finished by a concurrent
// caller (e.g. ReindexHandler's own completion racing the reconciliation
// sweep's earlier decision, or two reconciliation passes overlapping).
// Finish is a conditional update guarded on the current status precisely
// so this is detectable instead of silently overwriting a run that
// already reached its real terminal state with a stale, possibly
// misleading status/last_error. Callers should treat this as a no-op, not
// a failure.
var ErrRunNotProcessing = errors.New("search: run is not processing")

// RunStatus is the lifecycle state of a search_index_runs row.
type RunStatus string

const (
	RunStatusProcessing RunStatus = "processing"
	RunStatusCompleted  RunStatus = "completed"
	RunStatusFailed     RunStatus = "failed"
)

// Run is a single reindex run's persisted state.
type Run struct {
	ID             string
	Scope          string
	ScopeParams    map[string]interface{}
	Status         RunStatus
	TotalCount     int
	ProcessedCount int
	// ErrorCount is always 0 as of this PR: ReindexHandler fails the whole
	// run on the first indexing error rather than skipping a bad product
	// and counting it — there is no per-item failure to count yet. The
	// field exists now so a later PR that adds per-item error tolerance
	// (a real design decision, not scoped here) doesn't need a schema or
	// port change to report it.
	ErrorCount int
	StartedAt  time.Time
	FinishedAt time.Time
	LastError  string
}

// RunStore is the port for persisting search_index_runs rows.
type RunStore interface {
	// Create persists a new run, status "processing".
	Create(ctx context.Context, run Run) error

	// Get returns a run by ID, or (nil, nil) if none exists.
	Get(ctx context.Context, id string) (*Run, error)

	// UpdateProgress updates a still-processing run's counts.
	UpdateProgress(ctx context.Context, id string, totalCount, processedCount, errorCount int) error

	// Finish marks a run terminal (completed or failed), recording
	// finished_at and, for a failed run, lastErr. It is a conditional
	// update: only a run currently "processing" is changed. If the run
	// exists but is no longer "processing" (already finished by a
	// concurrent caller), Finish returns ErrRunNotProcessing instead of
	// overwriting it. If no run with that ID exists at all, Finish
	// returns a plain error distinguishable from ErrRunNotProcessing via
	// errors.Is.
	Finish(ctx context.Context, id string, status RunStatus, lastErr string) error

	// FindStaleProcessing returns up to limit runs still "processing"
	// whose started_at is older than olderThan, oldest first —
	// candidates for the reconciliation sweep
	// (application/search.ReconcileHandler) to check against their
	// underlying job's own outcome. See that handler's doc comment for
	// why a run can otherwise stay "processing" forever. limit bounds a
	// single sweep invocation's work (a mass-orphan event, e.g. many
	// workers crashing at once, must not make one tick process an
	// unbounded number of rows); any runs left over a full page are
	// picked up by the next tick, since a run this call doesn't reach
	// stays "processing" and so stays a match for the next call.
	FindStaleProcessing(ctx context.Context, olderThan time.Time, limit int) ([]Run, error)
}

// ReindexJobStatus is the minimal read model the reconciliation sweep
// needs from the job queue for one search.reindex job. Deliberately not
// domain/jobs.Detail — this keeps domain/search from depending on the
// jobs bounded context, the same decoupling principle ProductSource's doc
// comment applies to the catalog domain.
type ReindexJobStatus struct {
	// Found is false when no search.reindex job carries this run_id in
	// its payload at all — e.g. ReindexService.Trigger's Create succeeded
	// but Enqueue never did (Trigger already compensates for this
	// synchronously; Found=false lets the sweep catch it defensively too,
	// in case that compensating write itself failed).
	Found bool
	// JobID is the underlying job's ID, for the reconciled run's
	// LastError message. Empty when Found is false.
	JobID string
	// Status is the job's raw status string (pending/processing/done/
	// failed/cancelled). Empty when Found is false.
	Status string
	// Terminal is true when Status is done, failed, or cancelled — a
	// status nothing will ever move the job on from. A "processing" or
	// "pending" job might still legitimately finish later, so the sweep
	// must not act on it — see ReconcileHandler.
	Terminal bool
}

// ReindexJobLookup finds the search.reindex job carrying a given run_id in
// its payload, letting the reconciliation sweep learn whether a run stuck
// in "processing" corresponds to a job that has already reached a
// terminal status and will never call RunStore.Finish for it again.
type ReindexJobLookup interface {
	FindReindexJobByRunID(ctx context.Context, runID string) (ReindexJobStatus, error)
}

// ProductSource is the read-only product source a reindex handler scans to
// rebuild the search index. Kept narrow and decoupled from
// catalog.ProductRepository — like Product itself, this intentionally
// avoids importing the catalog package (see Product's doc comment) so the
// reindex handler doesn't need to know about the wider catalog domain.
type ProductSource interface {
	// CountAll returns the total number of products in the catalog — used
	// for a full-scan run's total_count, and by
	// application/search.ReindexService.Trigger's full-scan-threshold
	// comparison (PR-1034: a partial scope covering more of the catalog
	// than the threshold is run as a full scan instead).
	CountAll(ctx context.Context) (int, error)

	// ListAll returns a page of products ordered consistently across
	// calls (so paging by offset doesn't skip or repeat rows).
	ListAll(ctx context.Context, offset, limit int) ([]Product, error)

	// ListByIDs returns the products matching any of the given IDs — the
	// scoped-reindex (PR-1034) equivalent of ListAll's offset paging.
	// ReindexHandler chunks an already-resolved product ID list into
	// batches and calls this once per batch. Order is unspecified. An ID
	// with no matching product (deleted between
	// ReindexService.Trigger resolving the scope and the job actually
	// running) is simply absent from the result, not an error.
	ListByIDs(ctx context.Context, ids []string) ([]Product, error)

	// ProductIDsByCategory returns the distinct product IDs assigned to
	// any of the given category IDs — resolves a ScopeCategories request
	// (PR-1034) into the concrete ID list ReindexService.Trigger's
	// full-scan-threshold check and the job payload both need.
	ProductIDsByCategory(ctx context.Context, categoryIDs []string) ([]string, error)

	// ProductIDsUpdatedSince returns the IDs of products whose updated_at
	// is at or after since — resolves a ScopeSince request (PR-1034) the
	// same way ProductIDsByCategory resolves ScopeCategories.
	ProductIDsUpdatedSince(ctx context.Context, since time.Time) ([]string, error)
}
