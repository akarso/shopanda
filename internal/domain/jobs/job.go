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
	// StatusReclaiming is a Redis (and similar) intermediate during
	// RequeueWithBeforeCommit: claim is held off ready/processing lists
	// while OnRequeued runs. Dequeue ignores it; Complete/Fail will not
	// treat it as a live processing claim. Postgres keeps the row
	// processing under FOR UPDATE instead.
	StatusReclaiming Status = "reclaiming"
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
// Return this sentinel alone (or wrapping only context cancellation) —
// joining/wrapping a real handler failure with ErrSkipOutcome still Fail's
// so last_error is written. Skipping without a fence Complete/Fail/Requeue
// leaves status=processing.
var ErrSkipOutcome = errors.New("jobs: skip outcome")

// ErrOutcomeNotApplied means Complete/Fail matched zero rows (missing id,
// wrong attempt, or row no longer processing). The worker must not treat
// this as a committed success — the handler may already have run and the
// job may still be processing or claimed by a newer attempt.
var ErrOutcomeNotApplied = errors.New("jobs: outcome not applied")

// ErrRequeueUnsupported is returned by broker-backed queues that have no
// queryable processing row to reclaim.
var ErrRequeueUnsupported = errors.New("jobs: requeue unsupported for this queue driver")

// ErrRequeueConflict is a generic reclaim miss. Prefer the more specific
// errors below when the backend can distinguish the predicate.
var ErrRequeueConflict = errors.New("jobs: requeue conflict")

// ErrRequeueTooFresh means the processing claim is younger than the caller
// min-age (still a plausible live handler).
var ErrRequeueTooFresh = errors.New("jobs: requeue claim too fresh")

// ErrRequeueWrongAttempt means attempts did not match the live claim.
var ErrRequeueWrongAttempt = errors.New("jobs: requeue wrong attempt")

// ErrRequeueNotProcessing means the row is missing or not status=processing.
var ErrRequeueNotProcessing = errors.New("jobs: requeue not processing")

// ErrRequeueInProgress means another Requeue already holds reclaiming and
// its reclaim is younger than the fixed DefaultRequeueMinAge lease (not the
// caller's min-age) — do not abort that reclaim.
var ErrRequeueInProgress = errors.New("jobs: requeue already in progress")

// DefaultRequeueMinAge is the default minimum claim age when the caller
// does not pass WithRequeueMinAge (aligned with jobs:requeue). Age alone
// does not prove the worker is dead — prefer waiting for Fail then Retry.
const DefaultRequeueMinAge = 5 * time.Minute

// MinRequeueMinAge is the lowest WithRequeueMinAge value backends honor
// (matches jobs:requeue CLI floor). Smaller positive values fall back to
// DefaultRequeueMinAge.
const MinRequeueMinAge = time.Second

// RequeueOption configures Queue.Requeue.
type RequeueOption func(*requeueConfig)

type requeueConfig struct {
	MinAge time.Duration
}

// WithRequeueMinAge sets how old a processing claim must be before reclaim.
// Values <= 0 fall back to DefaultRequeueMinAge.
func WithRequeueMinAge(d time.Duration) RequeueOption {
	return func(c *requeueConfig) { c.MinAge = d }
}

// ApplyRequeueOptions resolves min-age for backends. Values <= 0 or below
// MinRequeueMinAge fall back to DefaultRequeueMinAge.
func ApplyRequeueOptions(opts []RequeueOption) time.Duration {
	cfg := requeueConfig{MinAge: DefaultRequeueMinAge}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	if cfg.MinAge < MinRequeueMinAge {
		return DefaultRequeueMinAge
	}
	return cfg.MinAge
}

// RequeueBeforeCommit runs after reclaim predicates pass and before the
// job becomes dequeueable (postgres: still processing under FOR UPDATE;
// redis: status=reclaiming, off ready). OnRequeued fence release sees
// attempt N. Return non-nil to abort — row must not land on ready/pending.
//
// Hooks must not open a second connection and UPDATE the same jobs row
// (deadlocks the reclaim lock). On Postgres, use RequeueTx(ctx) /
// pluginsdk.RequeueTx so fence writes commit with the jobs row. External
// side effects that commit before the reclaim TX must be independently
// idempotent — COMMIT failure does not re-run the hook.
type RequeueBeforeCommit func(ctx context.Context, jobID string, attempt int) error

// RequeueHookable is implemented by backends that can run a before-commit
// callback under the same lock/transaction as the pending update.
type RequeueHookable interface {
	RequeueWithBeforeCommit(ctx context.Context, id string, attempt int, before RequeueBeforeCommit, opts ...RequeueOption) error
}

// Queue is the port for job queue backends.
type Queue interface {
	// Enqueue adds a job to the queue.
	Enqueue(ctx context.Context, job Job) error

	// Dequeue atomically claims the next pending job.
	// Returns nil, nil when no jobs are available.
	Dequeue(ctx context.Context) (*Job, error)

	// Complete marks a job as done for the given dequeue attempt.
	// attempt must match the job.Attempts value returned by Dequeue.
	// Returns ErrOutcomeNotApplied when the conditional update matches
	// zero rows (stale attempt / missing / already moved).
	Complete(ctx context.Context, id string, attempt int) error

	// Fail marks a job as failed or re-queues it for retry for the given
	// dequeue attempt. If attempts < maxRetries, the job is re-queued with
	// a delay. Otherwise, the job is marked as permanently failed.
	// Returns ErrOutcomeNotApplied when no row matches.
	Fail(ctx context.Context, id string, attempt int, jobErr error) error

	// Requeue moves a processing job back to pending without resetting
	// attempts (unlike Admin.Retry, which is failed→pending with
	// attempts=0). attempt must match the live claim; the row must be
	// older than the configured min-age (default DefaultRequeueMinAge).
	// Returns ErrRequeueTooFresh / WrongAttempt / NotProcessing (or
	// ErrRequeueConflict), or ErrRequeueUnsupported on brokers.
	Requeue(ctx context.Context, id string, attempt int, opts ...RequeueOption) error
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
