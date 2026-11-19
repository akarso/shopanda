package pluginsdk

import (
	"context"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
)

// JobStatus is a job lifecycle status (pending, processing, …).
type JobStatus = jobs.Status

const (
	JobStatusPending    = jobs.StatusPending
	JobStatusProcessing = jobs.StatusProcessing
	JobStatusDone       = jobs.StatusDone
	JobStatusFailed     = jobs.StatusFailed
	JobStatusReclaiming = jobs.StatusReclaiming
	JobStatusCancelled  = jobs.StatusCancelled
)

// JobDetail is the full read model for a single job (Get).
type JobDetail = jobs.Detail

// RequeueOption configures JobsPort.Requeue / Queue.Requeue.
type RequeueOption = jobs.RequeueOption

// Requeue errors (same sentinels as internal/domain/jobs).
var (
	ErrRequeueUnsupported   = jobs.ErrRequeueUnsupported
	ErrRequeueConflict      = jobs.ErrRequeueConflict
	ErrRequeueTooFresh      = jobs.ErrRequeueTooFresh
	ErrRequeueWrongAttempt  = jobs.ErrRequeueWrongAttempt
	ErrRequeueNotFound      = jobs.ErrRequeueNotFound
	ErrRequeueNotProcessing = jobs.ErrRequeueNotProcessing
	ErrRequeueNotTerminal   = jobs.ErrRequeueNotTerminal
	ErrRequeueInProgress    = jobs.ErrRequeueInProgress
)

// WithRequeueMinAge sets how old a processing claim must be before reclaim.
func WithRequeueMinAge(d time.Duration) RequeueOption {
	return jobs.WithRequeueMinAge(d)
}

// WithRequeueTerminal re-pends a failed/done/cancelled job (see jobs docs).
func WithRequeueTerminal() RequeueOption {
	return jobs.WithRequeueTerminal()
}

// JobsPort is the Host-visible jobs surface for plugins that declare CapJobs.
// Get and CountsByStatus require a Reader-backed queue (Postgres). Requeue uses
// the bound Queue. Does not expose Enqueue/Dequeue/Complete/Fail or List.
//
// Get returns (nil, nil) when the id is missing or not a usable job id
// (including non-UUID strings on Postgres). Callers must nil-check before
// dereferencing. Requeue is not scoped to the plugin — any CapJobs holder may
// requeue any job id (ops-oriented; do not declare CapJobs casually).
type JobsPort interface {
	Get(ctx context.Context, id string) (*JobDetail, error)
	CountsByStatus(ctx context.Context) (map[JobStatus]int, error)
	Requeue(ctx context.Context, id string, attempt int, opts ...RequeueOption) error
}

type jobsPort struct {
	reader jobs.Reader
	queue  jobs.Queue
}

// NewJobsPort builds a JobsPort from a Reader + Queue (usually the same
// Postgres job queue instance). Returns an error when either is nil.
func NewJobsPort(reader jobs.Reader, queue jobs.Queue) (JobsPort, error) {
	if reader == nil {
		return nil, fmt.Errorf("pluginsdk: NewJobsPort: reader is nil")
	}
	if queue == nil {
		return nil, fmt.Errorf("pluginsdk: NewJobsPort: queue is nil")
	}
	return &jobsPort{reader: reader, queue: queue}, nil
}

func (p *jobsPort) Get(ctx context.Context, id string) (*JobDetail, error) {
	return p.reader.Get(ctx, id)
}

func (p *jobsPort) CountsByStatus(ctx context.Context) (map[JobStatus]int, error) {
	return p.reader.CountsByStatus(ctx)
}

func (p *jobsPort) Requeue(ctx context.Context, id string, attempt int, opts ...RequeueOption) error {
	return p.queue.Requeue(ctx, id, attempt, opts...)
}
