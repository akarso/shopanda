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

// ErrSkipOutcome tells the worker to skip platform Complete/Fail for this
// delivery. Handlers (or product fence wrappers) use it when they already
// wrote the outcome or intentionally leave the claim for reclaim/Requeue.
var ErrSkipOutcome = errors.New("jobs: skip outcome")

// Queue is the port for job queue backends.
type Queue interface {
	// Enqueue adds a job to the queue.
	Enqueue(ctx context.Context, job Job) error

	// Dequeue atomically claims the next pending job.
	// Returns nil, nil when no jobs are available.
	Dequeue(ctx context.Context) (*Job, error)

	// Complete marks a job as done for the given dequeue attempt.
	// attempt must match the job.Attempts value returned by Dequeue so a
	// stale delivery cannot finish a newer claim (postgres CAS; brokers
	// best-effort).
	Complete(ctx context.Context, id string, attempt int) error

	// Fail marks a job as failed or re-queues it for retry for the given
	// dequeue attempt. If attempts < maxRetries, the job is re-queued with
	// a delay. Otherwise, the job is marked as permanently failed.
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
