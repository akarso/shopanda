package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domainsearch "github.com/akarso/shopanda/internal/domain/search"
)

// Compile-time check that SearchIndexRunRepo implements domainsearch.RunStore.
var _ domainsearch.RunStore = (*SearchIndexRunRepo)(nil)

// SearchIndexRunRepo implements domainsearch.RunStore using the
// search_index_runs table (migration 069).
type SearchIndexRunRepo struct {
	db *sql.DB
}

// NewSearchIndexRunRepo returns a SearchIndexRunRepo backed by db.
func NewSearchIndexRunRepo(db *sql.DB) (*SearchIndexRunRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewSearchIndexRunRepo: nil *sql.DB")
	}
	return &SearchIndexRunRepo{db: db}, nil
}

// Create implements domainsearch.RunStore.
func (r *SearchIndexRunRepo) Create(ctx context.Context, run domainsearch.Run) error {
	params := run.ScopeParams
	if params == nil {
		params = map[string]interface{}{}
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("search_index_run_repo: marshal scope_params: %w", err)
	}

	const q = `INSERT INTO search_index_runs (
		id, scope, scope_params, status, total_count, processed_count, error_count, started_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err = r.db.ExecContext(ctx, q,
		run.ID, run.Scope, paramsJSON, run.Status, run.TotalCount, run.ProcessedCount, run.ErrorCount, run.StartedAt,
	)
	if err != nil {
		return fmt.Errorf("search_index_run_repo: create: %w", err)
	}
	return nil
}

// Get implements domainsearch.RunStore. Returns (nil, nil) if no run with
// that ID exists.
func (r *SearchIndexRunRepo) Get(ctx context.Context, id string) (*domainsearch.Run, error) {
	const q = `SELECT id, scope, scope_params, status, total_count, processed_count, error_count,
		started_at, finished_at, last_error
		FROM search_index_runs WHERE id = $1`

	var run domainsearch.Run
	var paramsJSON []byte
	var finishedAt sql.NullTime
	var lastError sql.NullString

	err := r.db.QueryRowContext(ctx, q, id).Scan(
		&run.ID, &run.Scope, &paramsJSON, &run.Status, &run.TotalCount, &run.ProcessedCount, &run.ErrorCount,
		&run.StartedAt, &finishedAt, &lastError,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("search_index_run_repo: get: %w", err)
	}
	if len(paramsJSON) > 0 {
		if err := json.Unmarshal(paramsJSON, &run.ScopeParams); err != nil {
			return nil, fmt.Errorf("search_index_run_repo: unmarshal scope_params: %w", err)
		}
	}
	if finishedAt.Valid {
		run.FinishedAt = finishedAt.Time
	}
	if lastError.Valid {
		run.LastError = lastError.String
	}
	return &run, nil
}

// UpdateProgress implements domainsearch.RunStore.
func (r *SearchIndexRunRepo) UpdateProgress(ctx context.Context, id string, totalCount, processedCount, errorCount int) error {
	const q = `UPDATE search_index_runs
		SET total_count = $2, processed_count = $3, error_count = $4
		WHERE id = $1`
	res, err := r.db.ExecContext(ctx, q, id, totalCount, processedCount, errorCount)
	if err != nil {
		return fmt.Errorf("search_index_run_repo: update progress: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("search_index_run_repo: update progress: rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("search_index_run_repo: update progress: run %s not found", id)
	}
	return nil
}

// FindStaleProcessing implements domainsearch.RunStore.
func (r *SearchIndexRunRepo) FindStaleProcessing(ctx context.Context, olderThan time.Time, limit int) ([]domainsearch.Run, error) {
	const q = `SELECT id, scope, scope_params, status, total_count, processed_count, error_count,
		started_at, finished_at, last_error
		FROM search_index_runs
		WHERE status = 'processing' AND started_at < $1
		ORDER BY started_at ASC
		LIMIT $2`

	rows, err := r.db.QueryContext(ctx, q, olderThan, limit)
	if err != nil {
		return nil, fmt.Errorf("search_index_run_repo: find stale processing: %w", err)
	}
	defer rows.Close()

	out := make([]domainsearch.Run, 0)
	for rows.Next() {
		var run domainsearch.Run
		var paramsJSON []byte
		var finishedAt sql.NullTime
		var lastError sql.NullString

		if err := rows.Scan(
			&run.ID, &run.Scope, &paramsJSON, &run.Status, &run.TotalCount, &run.ProcessedCount, &run.ErrorCount,
			&run.StartedAt, &finishedAt, &lastError,
		); err != nil {
			return nil, fmt.Errorf("search_index_run_repo: find stale processing scan: %w", err)
		}
		if len(paramsJSON) > 0 {
			if err := json.Unmarshal(paramsJSON, &run.ScopeParams); err != nil {
				return nil, fmt.Errorf("search_index_run_repo: find stale processing unmarshal scope_params: %w", err)
			}
		}
		if finishedAt.Valid {
			run.FinishedAt = finishedAt.Time
		}
		if lastError.Valid {
			run.LastError = lastError.String
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search_index_run_repo: find stale processing rows: %w", err)
	}
	return out, nil
}

// List implements domainsearch.RunStore.
//
// Ordered by started_at DESC, id DESC — id is a tiebreaker, not a
// secondary sort key of interest: concurrent triggers can share the same
// microsecond-resolution started_at, and without a stable tiebreaker their
// relative order is undefined, letting an offset-based caller skip or
// duplicate rows between pages (matching JobQueue.List's own
// "created_at DESC, id DESC" precedent for the same reason).
func (r *SearchIndexRunRepo) List(ctx context.Context, limit, offset int) ([]domainsearch.Run, error) {
	const q = `SELECT id, scope, scope_params, status, total_count, processed_count, error_count,
		started_at, finished_at, last_error
		FROM search_index_runs
		ORDER BY started_at DESC, id DESC
		LIMIT $1 OFFSET $2`

	rows, err := r.db.QueryContext(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("search_index_run_repo: list: %w", err)
	}
	defer rows.Close()

	out := make([]domainsearch.Run, 0)
	for rows.Next() {
		var run domainsearch.Run
		var paramsJSON []byte
		var finishedAt sql.NullTime
		var lastError sql.NullString

		if err := rows.Scan(
			&run.ID, &run.Scope, &paramsJSON, &run.Status, &run.TotalCount, &run.ProcessedCount, &run.ErrorCount,
			&run.StartedAt, &finishedAt, &lastError,
		); err != nil {
			return nil, fmt.Errorf("search_index_run_repo: list scan: %w", err)
		}
		if len(paramsJSON) > 0 {
			if err := json.Unmarshal(paramsJSON, &run.ScopeParams); err != nil {
				return nil, fmt.Errorf("search_index_run_repo: list unmarshal scope_params: %w", err)
			}
		}
		if finishedAt.Valid {
			run.FinishedAt = finishedAt.Time
		}
		if lastError.Valid {
			run.LastError = lastError.String
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search_index_run_repo: list rows: %w", err)
	}
	return out, nil
}

// Finish implements domainsearch.RunStore. It is a conditional update
// (`WHERE status = 'processing'`) rather than an unconditional write by
// id: two independent callers can each decide, from an earlier read, that
// a run should be finished (ReindexHandler completing normally,
// ReconcileHandler correcting a stuck run, a retried job completing
// again) — without this guard, whichever write lands second silently
// overwrites a run that already reached its real terminal status with a
// stale, possibly misleading status/last_error. When no row matches
// because the run is no longer "processing" (already finished by someone
// else), Finish returns domainsearch.ErrRunNotProcessing instead of
// succeeding or reporting a generic "not found" — a follow-up existence
// check distinguishes that from a genuinely unknown id.
func (r *SearchIndexRunRepo) Finish(ctx context.Context, id string, status domainsearch.RunStatus, lastErr string) error {
	const q = `UPDATE search_index_runs
		SET status = $2, finished_at = now(), last_error = NULLIF($3, '')
		WHERE id = $1 AND status = 'processing'`
	res, err := r.db.ExecContext(ctx, q, id, status, lastErr)
	if err != nil {
		return fmt.Errorf("search_index_run_repo: finish: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("search_index_run_repo: finish: rows affected: %w", err)
	}
	if rows > 0 {
		return nil
	}

	exists, err := r.exists(ctx, id)
	if err != nil {
		return fmt.Errorf("search_index_run_repo: finish: check existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("search_index_run_repo: finish: run %s not found", id)
	}
	return domainsearch.ErrRunNotProcessing
}

// exists reports whether a run with the given id is present, regardless
// of status — used by Finish to distinguish "not found" from "found but
// no longer processing" after a conditional update affects zero rows.
func (r *SearchIndexRunRepo) exists(ctx context.Context, id string) (bool, error) {
	var found int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM search_index_runs WHERE id = $1`, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
