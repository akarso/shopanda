package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
)

// Compile-time check that JobQueue implements jobs.Queue.
var _ jobs.Queue = (*JobQueue)(nil)

// retryDelay is the fixed delay before a failed job becomes eligible again.
const retryDelay = 10 * time.Second

// JobQueue implements jobs.Queue using PostgreSQL with FOR UPDATE SKIP LOCKED.
type JobQueue struct {
	db *sql.DB
}

// NewJobQueue returns a new JobQueue backed by db.
func NewJobQueue(db *sql.DB) *JobQueue {
	return &JobQueue{db: db}
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

// Fail re-queues a job for retry or marks it as permanently failed.
// Uses atomic conditional UPDATEs to avoid read-then-write races.
func (q *JobQueue) Fail(ctx context.Context, id string, jobErr error) error {
	// First, try to permanently fail jobs that have exhausted retries.
	const failQ = `UPDATE jobs SET status = 'failed', updated_at = NOW()
		WHERE id = $1 AND attempts >= max_retries`
	result, err := q.db.ExecContext(ctx, failQ, id)
	if err != nil {
		return fmt.Errorf("job_queue: fail permanent: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("job_queue: fail rows: %w", err)
	}
	if rows > 0 {
		return nil
	}

	// Otherwise, re-queue with a delay.
	const retryQ = `UPDATE jobs SET status = 'pending', run_at = NOW() + $2::interval, updated_at = NOW()
		WHERE id = $1 AND attempts < max_retries`
	_, err = q.db.ExecContext(ctx, retryQ, id, fmt.Sprintf("%d seconds", int(retryDelay.Seconds())))
	if err != nil {
		return fmt.Errorf("job_queue: fail retry: %w", err)
	}
	return nil
}
