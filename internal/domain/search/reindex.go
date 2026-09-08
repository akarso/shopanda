package search

import (
	"context"
	"time"
)

// RunStatus is the lifecycle state of a search_index_runs row.
type RunStatus string

const (
	RunStatusProcessing RunStatus = "processing"
	RunStatusCompleted  RunStatus = "completed"
	RunStatusFailed     RunStatus = "failed"
)

// ReindexScope describes what a reindex run should cover. This PR ships
// full-scope reindex only (Name is always "all"); Params exists so
// PR-1034's partial/scoped values (explicit product/category IDs, or a
// "changed since" timestamp) don't need a schema or port change here.
type ReindexScope struct {
	Name   string
	Params map[string]interface{}
}

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
	// finished_at and, for a failed run, lastErr.
	Finish(ctx context.Context, id string, status RunStatus, lastErr string) error
}

// ProductSource is the read-only product source a reindex handler scans to
// rebuild the search index. Kept narrow and decoupled from
// catalog.ProductRepository — like Product itself, this intentionally
// avoids importing the catalog package (see Product's doc comment) so the
// reindex handler doesn't need to know about the wider catalog domain.
type ProductSource interface {
	// CountAll returns the total number of products to index, for the
	// run's total_count.
	CountAll(ctx context.Context) (int, error)

	// ListAll returns a page of products ordered consistently across
	// calls (so paging by offset doesn't skip or repeat rows).
	ListAll(ctx context.Context, offset, limit int) ([]Product, error)
}
