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

// ErrRequeueNotFound means no jobs row matched the id (FOR UPDATE miss).
var ErrRequeueNotFound = errors.New("jobs: requeue job not found")

// ErrRequeueNotProcessing means the row exists but is not status=processing
// (default reclaim mode). Missing rows use ErrRequeueNotFound.
var ErrRequeueNotProcessing = errors.New("jobs: requeue not processing")

// ErrRequeueNotTerminal means WithRequeueTerminal was set but the row is not
// failed/done/cancelled. Missing rows use ErrRequeueNotFound.
var ErrRequeueNotTerminal = errors.New("jobs: requeue not terminal")

// ErrRequeueInProgress means another Requeue already holds reclaiming and
// its reclaim is younger than the fixed DefaultRequeueMinAge lease (not the
// caller's min-age) — do not abort that reclaim.
var ErrRequeueInProgress = errors.New("jobs: requeue already in progress")

// DefaultRequeueMinAge is the default minimum claim age when the caller
// does not pass WithRequeueMinAge (aligned with jobs:requeue). Age alone
// does not prove the worker is dead — prefer waiting for Fail then Retry.
// Ignored when WithRequeueTerminal is set.
const DefaultRequeueMinAge = 5 * time.Minute

// MinRequeueMinAge is the lowest WithRequeueMinAge value backends honor
// (matches jobs:requeue CLI floor). Smaller positive values fall back to
// DefaultRequeueMinAge.
const MinRequeueMinAge = time.Second

// RequeueOption configures Queue.Requeue.
type RequeueOption func(*requeueConfig)

type requeueConfig struct {
	MinAge   time.Duration
	Terminal bool
}

// RequeueSettings is the resolved option set for backends.
type RequeueSettings struct {
	MinAge   time.Duration
	Terminal bool
}

// WithRequeueMinAge sets how old a processing claim must be before reclaim.
// Values <= 0 fall back to DefaultRequeueMinAge. When WithRequeueTerminal is
// also set, MinAge is ignored (no age check on terminal rows).
func WithRequeueMinAge(d time.Duration) RequeueOption {
	return func(c *requeueConfig) { c.MinAge = d }
}

// WithRequeueTerminal re-pends a failed/done/cancelled job while keeping
// attempts, running OnRequeued / RequeueTx in the same transaction, setting
// run_at=NOW(), and leaving last_error unchanged. Does not accept
// pending/processing (use default reclaim for stale processing claims).
//
// API is ungated: any caller may pass this option for done/cancelled.
// The jobs:requeue CLI separately requires --force for done/cancelled
// and for exhausted attempts>=max_retries (including max_retries=0);
// --force is a no-op on non-exhausted failed. Do not assume Queue.Requeue
// enforces that. Admin.Retry remains failed-only with attempts=0 and no hooks.
func WithRequeueTerminal() RequeueOption {
	return func(c *requeueConfig) { c.Terminal = true }
}

// ResolveRequeueOptions resolves min-age and terminal mode for backends.
// Terminal=true ignores MinAge (returned MinAge is still normalized for
// callers that log it; backends must skip the age predicate).
func ResolveRequeueOptions(opts []RequeueOption) RequeueSettings {
	cfg := requeueConfig{MinAge: DefaultRequeueMinAge}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	minAge := cfg.MinAge
	if minAge < MinRequeueMinAge {
		minAge = DefaultRequeueMinAge
	}
	return RequeueSettings{MinAge: minAge, Terminal: cfg.Terminal}
}

// ApplyRequeueOptions resolves min-age for backends. Values <= 0 or below
// MinRequeueMinAge fall back to DefaultRequeueMinAge.
func ApplyRequeueOptions(opts []RequeueOption) time.Duration {
	return ResolveRequeueOptions(opts).MinAge
}

// IsTerminalRequeueStatus reports whether status may be re-pended with
// WithRequeueTerminal (API). CLI force rules use TerminalRequeueNeedsForce.
func IsTerminalRequeueStatus(s Status) bool {
	switch s {
	case StatusFailed, StatusDone, StatusCancelled:
		return true
	default:
		return false
	}
}

// TerminalRequeueNeedsForce reports statuses the jobs:requeue CLI treats as
// high-risk (done/cancelled resurrection). Failed never needs force for
// status alone; exhausted attempts are a separate CLI check.
func TerminalRequeueNeedsForce(s Status) bool {
	return s == StatusDone || s == StatusCancelled
}

// RequeueBeforeCommit runs after reclaim predicates pass and before the
// job becomes dequeueable (postgres: row locked under FOR UPDATE in its
// prior status — processing for default reclaim, or failed/done/cancelled
// for WithRequeueTerminal; redis: status=reclaiming, off ready).
// OnRequeued fence release sees attempt N. Return non-nil to abort — row
// must not land on ready/pending.
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

	// Requeue moves a job back to pending without resetting attempts
	// (unlike Admin.Retry, which is failed→pending with attempts=0 and
	// no OnRequeued). Default: processing only; attempt must match; row
	// older than min-age (DefaultRequeueMinAge). WithRequeueTerminal:
	// failed/done/cancelled only (API ungated — CLI --force is separate);
	// no min-age; same attempt CAS; run_at=NOW(); last_error unchanged;
	// OnRequeued runs in the reclaim TX.
	// Returns ErrRequeueNotFound / TooFresh / WrongAttempt / NotProcessing /
	// NotTerminal / Conflict, or ErrRequeueUnsupported on brokers.
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
