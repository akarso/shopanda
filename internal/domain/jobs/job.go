package jobs

import (
	"context"
	"errors"
	"time"
)

// Status represents the lifecycle state of a job.
type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusDone       Status = "done"
	StatusFailed     Status = "failed"
	// StatusCancelled is a terminal state reached only via an admin-triggered
	// Cancel (see jobs.Admin) on a still-pending job — never set by the
	// worker itself. A processing job cannot be cancelled (no in-flight
	// cancellation), so this status never interrupts a handler already
	// running.
	StatusCancelled Status = "cancelled"
)

// DefaultMaxRetries is the default number of retry attempts for a job.
const DefaultMaxRetries = 3

// ErrSkipOutcome tells the worker to skip platform Complete/Fail for this
// attempt. Handlers (or queue wrappers) return it when they already recorded
// the delivery outcome themselves — e.g. a product claim fence that called
// Complete/Fail on the underlying queue. Use errors.Is to detect it.
var ErrSkipOutcome = errors.New("jobs: skip outcome")

// Job represents a unit of background work.
type Job struct {
	ID         string
	Type       string
	Payload    map[string]interface{}
	Status     Status
	Attempts   int
	MaxRetries int
	RunAt      time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Handler processes a specific job type.
type Handler interface {
	// Type returns the job type this handler processes.
	Type() string

	// Handle executes the job. Implementations must be idempotent.
	Handle(ctx context.Context, job Job) error
}

// Queue is the port for job queue backends.
type Queue interface {
	// Enqueue adds a job to the queue.
	Enqueue(ctx context.Context, job Job) error

	// Dequeue atomically claims the next pending job.
	// Returns nil, nil when no jobs are available.
	Dequeue(ctx context.Context) (*Job, error)

	// Complete marks a job as done for the given claim attempt (the
	// Attempts value on the Job returned by Dequeue). Backends that can
	// compare attempt (Postgres, Redis) no-op with a nil error when the
	// row/claim no longer matches — a stale worker must not finish a
	// newer claim. Broker backends accept attempt and compare when the
	// inflight entry is available; otherwise they keep id-based behavior.
	Complete(ctx context.Context, id string, attempt int) error

	// Fail marks a job as failed or re-queues it for retry for the given
	// claim attempt. If attempts < maxRetries, the job is re-queued with
	// a delay. Otherwise, the job is marked as permanently failed.
	// Same stale-attempt semantics as Complete: mismatch / already moved
	// on returns nil so the worker does not treat it as a hard failure.
	Fail(ctx context.Context, id string, attempt int, jobErr error) error
}

// NewJob creates a Job with sensible defaults.
func NewJob(id, jobType string, payload map[string]interface{}) (Job, error) {
	if id == "" {
		return Job{}, errors.New("job id is required")
	}
	if jobType == "" {
		return Job{}, errors.New("job type is required")
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	now := time.Now().UTC()
	return Job{
		ID:         id,
		Type:       jobType,
		Payload:    payload,
		Status:     StatusPending,
		Attempts:   0,
		MaxRetries: DefaultMaxRetries,
		RunAt:      now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}
