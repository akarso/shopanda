package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
)

// Compile-time check that JobQueue implements jobs.Queue and jobs.Reader —
// the same Postgres-backed type satisfies both the write-oriented queue
// port and the read-only introspection port, since it's one table either
// way. A future broker-backed Queue implementation is not expected to
// implement jobs.Reader; see that interface's doc comment.
var (
	_ jobs.Queue  = (*JobQueue)(nil)
	_ jobs.Reader = (*JobQueue)(nil)
)

// Backoff parameters for retry delay calculation.
const (
	backoffBase = 5 * time.Second // initial delay
	backoffMax  = 5 * time.Minute // cap
	jitterRatio = 0.25            // ±25% randomization
)

// JobQueue implements jobs.Queue using PostgreSQL with FOR UPDATE SKIP LOCKED.
type JobQueue struct {
	db *sql.DB
}

// NewJobQueue returns a new JobQueue backed by db.
func NewJobQueue(db *sql.DB) (*JobQueue, error) {
	if db == nil {
		return nil, fmt.Errorf("NewJobQueue: nil *sql.DB")
	}
	return &JobQueue{db: db}, nil
}

// Enqueue inserts a new job into the queue.
func (q *JobQueue) Enqueue(ctx context.Context, job jobs.Job) error {
	payload, err := json.Marshal(job.Payload)
	if err != nil {
		return fmt.Errorf("job_queue: marshal payload: %w", err)
	}

	const query = `INSERT INTO jobs (id, type, payload, status, attempts, max_retries, run_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	_, err = q.db.ExecContext(ctx, query,
		job.ID, job.Type, payload, string(job.Status),
		job.Attempts, job.MaxRetries, job.RunAt, job.CreatedAt, job.UpdatedAt)
	if err != nil {
		return fmt.Errorf("job_queue: enqueue: %w", err)
	}
	return nil
}

// Dequeue atomically claims the next pending job using FOR UPDATE SKIP LOCKED.
// Returns nil, nil when no jobs are available.
func (q *JobQueue) Dequeue(ctx context.Context) (*jobs.Job, error) {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("job_queue: begin tx: %w", err)
	}
	defer tx.Rollback()

	const selectQ = `SELECT id, type, payload, status, attempts, max_retries, run_at, created_at, updated_at
		FROM jobs
		WHERE status = 'pending' AND run_at <= NOW()
		ORDER BY created_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1`

	var j jobs.Job
	var payloadJSON []byte
	var status string

	err = tx.QueryRowContext(ctx, selectQ).Scan(
		&j.ID, &j.Type, &payloadJSON, &status,
		&j.Attempts, &j.MaxRetries, &j.RunAt, &j.CreatedAt, &j.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("job_queue: dequeue select: %w", err)
	}

	j.Status = jobs.Status(status)

	if len(payloadJSON) > 0 {
		if err := json.Unmarshal(payloadJSON, &j.Payload); err != nil {
			return nil, fmt.Errorf("job_queue: unmarshal payload: %w", err)
		}
	}
	if j.Payload == nil {
		j.Payload = map[string]interface{}{}
	}

	const updateQ = `UPDATE jobs SET status = 'processing', attempts = attempts + 1, updated_at = NOW() WHERE id = $1`
	if _, err := tx.ExecContext(ctx, updateQ, j.ID); err != nil {
		return nil, fmt.Errorf("job_queue: dequeue update: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("job_queue: dequeue commit: %w", err)
	}

	j.Status = jobs.StatusProcessing
	j.Attempts++
	return &j, nil
}

// Complete marks a job as done.
func (q *JobQueue) Complete(ctx context.Context, id string) error {
	const query = `UPDATE jobs SET status = 'done', updated_at = NOW() WHERE id = $1 AND status = 'processing'`
	result, err := q.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("job_queue: complete: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("job_queue: complete rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("job_queue: job %s not found or not processing", id)
	}
	return nil
}

// retryDelay calculates the backoff duration for a given attempt number.
// Uses exponential backoff (base * 2^attempt) capped at backoffMax, with ±25% jitter.
func retryDelay(attempt int) time.Duration {
	delay := float64(backoffBase) * math.Pow(2, float64(attempt))
	if delay > float64(backoffMax) {
		delay = float64(backoffMax)
	}
	jitter := delay * jitterRatio * (2*rand.Float64() - 1) // range: -jitterRatio..+jitterRatio
	return time.Duration(delay + jitter)
}

// Fail re-queues a job for retry or marks it as permanently failed. jobErr,
// when non-nil, is recorded as the job's last_error (admin introspection,
// PR-1028) — a nil jobErr (the worker's "no handler registered" path)
// leaves last_error unchanged rather than inventing a message on this
// method's behalf.
// Uses atomic conditional UPDATEs to avoid read-then-write races.
func (q *JobQueue) Fail(ctx context.Context, id string, jobErr error) error {
	var lastErr interface{}
	if jobErr != nil {
		lastErr = jobErr.Error()
	}

	// First, try to permanently fail jobs that have exhausted retries.
	const failQ = `UPDATE jobs SET status = 'failed', updated_at = NOW(),
			last_error = COALESCE($2, last_error)
		WHERE id = $1 AND status = 'processing' AND attempts >= max_retries`
	result, err := q.db.ExecContext(ctx, failQ, id, lastErr)
	if err != nil {
		return fmt.Errorf("job_queue: fail permanent: %w", err)
	}
	failRows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("job_queue: fail rows: %w", err)
	}
	if failRows > 0 {
		return nil
	}

	// Otherwise, re-queue with exponential backoff delay.
	// We read attempts to compute the delay in Go (with jitter), then update atomically.
	var attempts int
	const attemptsQ = `SELECT attempts FROM jobs WHERE id = $1 AND status = 'processing' AND attempts < max_retries`
	if err := q.db.QueryRowContext(ctx, attemptsQ, id).Scan(&attempts); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("job_queue: job %s not found or not processing", id)
		}
		return fmt.Errorf("job_queue: fail lookup: %w", err)
	}

	delay := retryDelay(attempts - 1)
	const retryQ = `UPDATE jobs SET status = 'pending', run_at = NOW() + $2::interval, updated_at = NOW(),
			last_error = COALESCE($3, last_error)
		WHERE id = $1 AND status = 'processing' AND attempts < max_retries`
	result, err = q.db.ExecContext(ctx, retryQ, id, fmt.Sprintf("%d milliseconds", delay.Milliseconds()), lastErr)
	if err != nil {
		return fmt.Errorf("job_queue: fail retry: %w", err)
	}
	retryRows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("job_queue: fail retry rows: %w", err)
	}
	if retryRows == 0 {
		return fmt.Errorf("job_queue: job %s not found or not processing", id)
	}
	return nil
}

// List returns a page of jobs matching filter, most recently created
// first. Callers are expected to have already applied any limit/offset
// bounds — this method trusts filter.Limit/Offset as given (see
// internal/application/jobs.Service, which owns that policy).
func (q *JobQueue) List(ctx context.Context, filter jobs.ListFilter) ([]jobs.Summary, error) {
	const query = `SELECT id, type, status, attempts, max_retries, run_at, created_at, updated_at
		FROM jobs
		WHERE ($1 = '' OR type = $1) AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4`

	rows, err := q.db.QueryContext(ctx, query, filter.Type, string(filter.Status), filter.Limit, filter.Offset)
	if err != nil {
		return nil, fmt.Errorf("job_queue: list: %w", err)
	}
	defer rows.Close()

	out := make([]jobs.Summary, 0)
	for rows.Next() {
		var s jobs.Summary
		var status string
		if err := rows.Scan(&s.ID, &s.Type, &status, &s.Attempts, &s.MaxRetries, &s.RunAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("job_queue: list scan: %w", err)
		}
		s.Status = jobs.Status(status)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("job_queue: list rows: %w", err)
	}
	return out, nil
}

// Get returns a single job's full detail. Returns (nil, nil) when no job
// with that ID exists.
func (q *JobQueue) Get(ctx context.Context, id string) (*jobs.Detail, error) {
	const query = `SELECT id, type, payload, status, attempts, max_retries, run_at, created_at, updated_at, last_error
		FROM jobs WHERE id = $1`

	var d jobs.Detail
	var payloadJSON []byte
	var status string
	var lastError sql.NullString

	err := q.db.QueryRowContext(ctx, query, id).Scan(
		&d.ID, &d.Type, &payloadJSON, &status, &d.Attempts, &d.MaxRetries, &d.RunAt, &d.CreatedAt, &d.UpdatedAt, &lastError)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("job_queue: get: %w", err)
	}
	d.Status = jobs.Status(status)
	d.LastError = lastError.String

	// No nil-Payload fallback needed: jobs.payload is NOT NULL DEFAULT
	// '{}', so payloadJSON is always non-empty and Unmarshal always
	// produces a non-nil map.
	if err := json.Unmarshal(payloadJSON, &d.Payload); err != nil {
		return nil, fmt.Errorf("job_queue: get unmarshal payload: %w", err)
	}
	return &d, nil
}

// CountsByStatus returns the number of jobs currently in each status. A
// status with zero jobs is absent from the map, not present with 0.
func (q *JobQueue) CountsByStatus(ctx context.Context) (map[jobs.Status]int, error) {
	const query = `SELECT status, count(*) FROM jobs GROUP BY status`

	rows, err := q.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("job_queue: counts by status: %w", err)
	}
	defer rows.Close()

	out := make(map[jobs.Status]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("job_queue: counts by status scan: %w", err)
		}
		out[jobs.Status(status)] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("job_queue: counts by status rows: %w", err)
	}
	return out, nil
}
