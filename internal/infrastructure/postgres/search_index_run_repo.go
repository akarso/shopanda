package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

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

// Finish implements domainsearch.RunStore.
func (r *SearchIndexRunRepo) Finish(ctx context.Context, id string, status domainsearch.RunStatus, lastErr string) error {
	const q = `UPDATE search_index_runs
		SET status = $2, finished_at = now(), last_error = NULLIF($3, '')
		WHERE id = $1`
	res, err := r.db.ExecContext(ctx, q, id, status, lastErr)
	if err != nil {
		return fmt.Errorf("search_index_run_repo: finish: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("search_index_run_repo: finish: rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("search_index_run_repo: finish: run %s not found", id)
	}
	return nil
}
