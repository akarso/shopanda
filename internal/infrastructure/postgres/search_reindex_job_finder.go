package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
)

// Compile-time check that ReindexJobFinder implements domainsearch.ReindexJobLookup.
var _ domainsearch.ReindexJobLookup = (*ReindexJobFinder)(nil)

// reindexJobType is the job.Type value ReindexService.Trigger enqueues
// (application/search.JobType). Duplicated here as a literal rather than
// imported — this package (postgres) sits below the application layer and
// must not import it; this is the same reason SearchProductSource reads
// the products table directly instead of routing through a higher-layer
// port. Must stay in sync with application/search.JobType.
const reindexJobType = "search.reindex"

// ReindexJobFinder implements domainsearch.ReindexJobLookup by querying the
// jobs table (the same table JobQueue manages) for the search.reindex job
// carrying a given run_id in its payload. Kept as its own narrow type
// rather than a method on JobQueue: JobQueue is deliberately generic
// (jobs.Queue/Reader/Admin have no job-type-specific knowledge), the same
// reason SearchProductSource is its own type instead of routing through
// catalog.ProductRepository — see that type's doc comment.
type ReindexJobFinder struct {
	db *sql.DB
}

// NewReindexJobFinder returns a ReindexJobFinder backed by db.
func NewReindexJobFinder(db *sql.DB) (*ReindexJobFinder, error) {
	if db == nil {
		return nil, fmt.Errorf("NewReindexJobFinder: nil *sql.DB")
	}
	return &ReindexJobFinder{db: db}, nil
}

// FindReindexJobByRunID implements domainsearch.ReindexJobLookup. When more
// than one search.reindex job somehow carries the same run_id (should
// never happen — ReindexService.Trigger enqueues exactly one job per run
// it creates), the most recently created one wins.
func (f *ReindexJobFinder) FindReindexJobByRunID(ctx context.Context, runID string) (domainsearch.ReindexJobStatus, error) {
	const q = `SELECT id, status FROM jobs
		WHERE type = $1 AND payload->>'run_id' = $2
		ORDER BY created_at DESC
		LIMIT 1`

	var jobID, status string
	err := f.db.QueryRowContext(ctx, q, reindexJobType, runID).Scan(&jobID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return domainsearch.ReindexJobStatus{Found: false}, nil
	}
	if err != nil {
		return domainsearch.ReindexJobStatus{}, fmt.Errorf("reindex_job_finder: find by run id: %w", err)
	}

	st := domainjobs.Status(status)
	terminal := st == domainjobs.StatusDone || st == domainjobs.StatusFailed || st == domainjobs.StatusCancelled
	return domainsearch.ReindexJobStatus{
		Found:    true,
		JobID:    jobID,
		Status:   status,
		Terminal: terminal,
	}, nil
}
