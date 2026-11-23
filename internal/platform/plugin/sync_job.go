package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/requestctx"
	"github.com/akarso/shopanda/pkg/extapi"
)

// SyncEnqueueOption configures Integration.Enqueue / enqueueSyncJob.
type SyncEnqueueOption func(*syncEnqueueConfig)

type syncEnqueueConfig struct {
	jobID    string
	jobIDSet bool
}

// WithJobID sets the queue job id for an idempotent enqueue (one job per entity).
// Empty / whitespace-only / non-UUID ids fail closed at Enqueue time (Postgres
// jobs.id is UUID; JobsPort.Get treats non-UUID as missing).
func WithJobID(jobID string) SyncEnqueueOption {
	return func(c *syncEnqueueConfig) {
		c.jobIDSet = true
		c.jobID = strings.TrimSpace(jobID)
	}
}

func applySyncEnqueueOptions(opts []SyncEnqueueOption) (syncEnqueueConfig, error) {
	var cfg syncEnqueueConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if !cfg.jobIDSet {
		return cfg, nil
	}
	if cfg.jobID == "" {
		return cfg, fmt.Errorf("plugin: enqueue sync job: WithJobID requires a non-empty UUID")
	}
	if !id.IsUUID(cfg.jobID) {
		return cfg, fmt.Errorf("plugin: enqueue sync job: WithJobID %q is not a UUID", cfg.jobID)
	}
	return cfg, nil
}

// syncJobConflictPort is the JobsPort surface needed to resolve idempotent
// WithJobID conflicts (type check + status-aware resume). Satisfied by
// pluginsdk.JobsPort / Postgres Reader queues.
type syncJobConflictPort interface {
	Get(ctx context.Context, id string) (*jobs.Detail, error)
	Requeue(ctx context.Context, id string, attempt int, opts ...jobs.RequeueOption) error
}

type SyncJobRegistration struct {
	PluginSlug string
	Job        extapi.SyncJob
	JobType    string
}

// RegisterSyncJob registers an outbound integration sync job for this plugin slug.
func (i *Integration) RegisterSyncJob(job extapi.SyncJob) error {
	if i == nil || i.app == nil {
		return fmt.Errorf("plugin: integration app not configured")
	}
	return i.app.registerSyncJob(i.slug, job)
}

// Enqueue queues a previously registered sync job for this plugin slug.
// Any registered job may be enqueued (manual, cron, or event) — this is the
// on-demand / "run now" path. Handlers must be idempotent: run-now can overlap
// a cron/event run of the same job type. The composition root must BindQueue
// the resolved jobs.Queue after InitAll; Enqueue during Init fails with
// ErrSyncJobQueueUnbound. Payload may be nil (stored as an empty object). The
// job envelope trigger is always "manual" so handlers can distinguish on-demand
// runs from cron/event.
//
// WithJobID: idempotent one-job-per-entity enqueue (UUID required). On
// Conflict, Enqueue resolves via bound JobsPort (Compose BindJobsPort /
// CapJobs): same sync Type + pending/processing/reclaiming → success
// (no-op; second payload is discarded); terminal → Requeue(WithRequeueTerminal)
// with one-shot race handling (NotTerminal = success; WrongAttempt → Get once
// + retry, second WrongAttempt hard-fails); wrong Type → hard error. JobsPort
// is required to resolve Conflict (Postgres Reader); without it Conflict is
// returned wrapped so IsConflict remains true but resume is not guessed.
// Cross-plugin enqueue is allowed, matching RegisterSyncJob.
func (i *Integration) Enqueue(ctx context.Context, name string, payload map[string]interface{}, opts ...SyncEnqueueOption) error {
	if i == nil || i.app == nil {
		return fmt.Errorf("plugin: integration app not configured")
	}
	return i.app.enqueueSyncJob(ctx, i.slug, name, payload, opts...)
}

func (a *App) registerSyncJob(pluginSlug string, job extapi.SyncJob) error {
	name := strings.TrimSpace(job.Name)
	if name == "" {
		return fmt.Errorf("plugin: sync job: %w", extapi.ErrSyncJobNameRequired)
	}
	if job.Handler == nil {
		return fmt.Errorf("plugin: sync job %q: handler must not be nil", name)
	}
	slug, err := extapi.NormalizePluginSlug(pluginSlug)
	if err != nil {
		return fmt.Errorf("plugin: sync job %q: %w", name, err)
	}
	jobType, err := extapi.SyncJobType(slug, name)
	if err != nil {
		return fmt.Errorf("plugin: sync job %q: %w", name, err)
	}
	job.Name = name
	reg := SyncJobRegistration{
		PluginSlug: slug,
		Job:        job,
		JobType:    jobType,
	}
	switch job.Trigger.Kind {
	case extapi.SyncTriggerCron:
		if strings.TrimSpace(job.Trigger.CronSpec) == "" {
			return fmt.Errorf("plugin: sync job %q: cron spec required", name)
		}
		if job.EventJobID != nil {
			return fmt.Errorf("plugin: sync job %q: EventJobID is only valid for event triggers", name)
		}
	case extapi.SyncTriggerEvent:
		eventName := strings.TrimSpace(job.Trigger.EventName)
		if eventName == "" {
			return fmt.Errorf("plugin: sync job %q: event name required", name)
		}
		reg.Job.Trigger.EventName = eventName
	case extapi.SyncTriggerManual:
		// On-demand only; no cron/event fields.
		if job.EventJobID != nil {
			return fmt.Errorf("plugin: sync job %q: EventJobID is only valid for event triggers", name)
		}
	default:
		return fmt.Errorf("plugin: sync job %q: unsupported trigger kind %q", name, job.Trigger.Kind)
	}
	for _, existing := range a.syncJobs {
		if existing.JobType == jobType {
			return fmt.Errorf("plugin: duplicate sync job type %q", jobType)
		}
	}
	a.syncJobs = append(a.syncJobs, reg)
	return nil
}

// NewSyncJobEnvelope builds the queue job for a sync registration with a
// generated id. trigger is stored in the envelope (cron / event / manual).
// payload is shallow-copied so later caller mutations are not visible to the queue.
func NewSyncJobEnvelope(reg SyncJobRegistration, trigger string, payload map[string]interface{}) (jobs.Job, error) {
	return newSyncJobEnvelope(reg, trigger, payload, id.New())
}

// NewSyncJobEnvelopeWithID builds the queue job with a caller-chosen id
// (UUID required). Used by event triggers with EventJobID and by WithJobID.
func NewSyncJobEnvelopeWithID(reg SyncJobRegistration, trigger string, payload map[string]interface{}, jobID string) (jobs.Job, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return jobs.Job{}, fmt.Errorf("plugin: sync job envelope: job id requires a non-empty UUID")
	}
	if !id.IsUUID(jobID) {
		return jobs.Job{}, fmt.Errorf("plugin: sync job envelope: job id %q is not a UUID", jobID)
	}
	return newSyncJobEnvelope(reg, trigger, payload, jobID)
}

// ResolveEnqueueConflict resumes an idempotent enqueue after a Conflict
// (same rules as WithJobID). Used by the event-trigger path when EventJobID
// is set. Without JobsPort, Conflict is returned wrapped.
func (a *App) ResolveEnqueueConflict(ctx context.Context, name, jobID, expectedType string, conflict error) error {
	return a.resolveIdempotentEnqueueConflict(ctx, name, jobID, expectedType, conflict)
}

func newSyncJobEnvelope(reg SyncJobRegistration, trigger string, payload map[string]interface{}, jobID string) (jobs.Job, error) {
	copied := shallowCopyStringMap(payload)
	if copied == nil {
		copied = map[string]interface{}{}
	}
	name := strings.TrimSpace(reg.Job.Name)
	job, err := jobs.NewJob(jobID, reg.JobType, map[string]interface{}{
		"plugin":  reg.PluginSlug,
		"name":    name,
		"trigger": trigger,
		"payload": copied,
	})
	if err != nil {
		return jobs.Job{}, err
	}
	if reg.Job.MaxRetries > 0 {
		job.MaxRetries = reg.Job.MaxRetries
	}
	return job, nil
}

func shallowCopyStringMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// InjectRequestID shallow-copies payload and, when ctx carries a request ID
// and the payload has no usable request_id, stores it as "request_id".
// Empty string, nil, or non-string existing values are treated as unset and
// overwritten. Callers that already set a non-empty string keep their value.
// Empty / missing ctx IDs are a no-op.
func InjectRequestID(ctx context.Context, payload map[string]interface{}) map[string]interface{} {
	out := shallowCopyStringMap(payload)
	if out == nil {
		out = map[string]interface{}{}
	}
	if ctx == nil {
		return out
	}
	rid := requestctx.RequestID(ctx)
	if rid == "" {
		return out
	}
	if existing, ok := out["request_id"]; ok {
		if s, isStr := existing.(string); isStr && s != "" {
			return out
		}
	}
	out["request_id"] = rid
	return out
}

func (a *App) enqueueSyncJob(ctx context.Context, pluginSlug, name string, payload map[string]interface{}, opts ...SyncEnqueueOption) error {
	if a == nil {
		return fmt.Errorf("plugin: app is nil")
	}
	cfg, err := applySyncEnqueueOptions(opts)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("plugin: enqueue sync job: %w", extapi.ErrSyncJobNameRequired)
	}
	jobType, err := extapi.SyncJobType(pluginSlug, name)
	if err != nil {
		return fmt.Errorf("plugin: enqueue sync job %q: %w", name, err)
	}
	var reg SyncJobRegistration
	found := false
	for i := range a.syncJobs {
		if a.syncJobs[i].JobType == jobType {
			reg = a.syncJobs[i]
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("plugin: enqueue sync job %q: %w", name, extapi.ErrSyncJobNotFound)
	}
	if a.boundQueue == nil {
		return fmt.Errorf("plugin: enqueue sync job %q: %w", name, extapi.ErrSyncJobQueueUnbound)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload = InjectRequestID(ctx, payload)
	var job jobs.Job
	if cfg.jobIDSet {
		job, err = newSyncJobEnvelope(reg, extapi.SyncTriggerManual, payload, cfg.jobID)
	} else {
		job, err = NewSyncJobEnvelope(reg, extapi.SyncTriggerManual, payload)
	}
	if err != nil {
		return fmt.Errorf("plugin: enqueue sync job %q: %w", name, err)
	}
	if err := a.boundQueue.Enqueue(ctx, job); err != nil {
		if cfg.jobIDSet && apperror.Is(err, apperror.CodeConflict) {
			return a.resolveIdempotentEnqueueConflict(ctx, name, job.ID, job.Type, err)
		}
		return fmt.Errorf("plugin: enqueue sync job %q: %w", name, err)
	}
	if a.Logger != nil {
		fields := map[string]interface{}{
			"job_id":   job.ID,
			"job_type": job.Type,
			"trigger":  extapi.SyncTriggerManual,
		}
		if rid, _ := payload["request_id"].(string); rid != "" {
			fields["request_id"] = rid
		}
		a.Logger.Info("integration.sync_job.enqueued", fields)
	}
	return nil
}

// resolveIdempotentEnqueueConflict verifies the existing row matches the
// intended sync job type and status-aware resumes. Second Enqueue payload is
// never applied (Requeue does not rewrite payload).
func (a *App) resolveIdempotentEnqueueConflict(ctx context.Context, name, jobID, expectedType string, conflict error) error {
	raw, ok := a.JobsPort()
	if !ok || raw == nil {
		return fmt.Errorf("plugin: enqueue sync job %q: conflict on job id %q (JobsPort required to verify type/status): %w", name, jobID, conflict)
	}
	port, ok := raw.(syncJobConflictPort)
	if !ok || port == nil {
		return fmt.Errorf("plugin: enqueue sync job %q: conflict on job id %q (JobsPort does not support Get/Requeue): %w", name, jobID, conflict)
	}
	detail, err := port.Get(ctx, jobID)
	if err != nil {
		return fmt.Errorf("plugin: enqueue sync job %q: conflict resolve Get %q: %w", name, jobID, err)
	}
	if detail == nil {
		return fmt.Errorf("plugin: enqueue sync job %q: conflict on job id %q but Get returned nil: %w", name, jobID, conflict)
	}
	if detail.Type != expectedType {
		return fmt.Errorf("plugin: enqueue sync job %q: job id %q has type %q, want %q (refusing to resume foreign job)",
			name, jobID, detail.Type, expectedType)
	}
	kind := classifySyncJobConflictStatus(detail.Status)
	switch kind {
	case syncJobConflictInFlight:
		// Already queued / in flight / reclaiming — idempotent success. Payload unchanged.
		return nil
	case syncJobConflictTerminal:
		return a.requeueTerminalSyncJob(ctx, port, name, jobID, expectedType, detail)
	default:
		return fmt.Errorf("plugin: enqueue sync job %q: job id %q has unsupported status %q", name, jobID, detail.Status)
	}
}

type syncJobConflictStatusKind int

const (
	syncJobConflictUnknown syncJobConflictStatusKind = iota
	syncJobConflictInFlight
	syncJobConflictTerminal
)

func classifySyncJobConflictStatus(status jobs.Status) syncJobConflictStatusKind {
	switch status {
	case jobs.StatusPending, jobs.StatusProcessing, jobs.StatusReclaiming:
		return syncJobConflictInFlight
	case jobs.StatusFailed, jobs.StatusDone, jobs.StatusCancelled:
		return syncJobConflictTerminal
	default:
		return syncJobConflictUnknown
	}
}

// requeueTerminalSyncJob resumes a terminal job. One-shot race handling (best-effort):
// ErrRequeueNotTerminal → success (peer already requeued; known drivers only);
// ErrRequeueWrongAttempt → Get once, re-check type, retry Requeue if still terminal,
// or success if now in-flight. A second WrongAttempt is returned as a hard error —
// callers under heavy N-way contention may still see "conflict requeue retry".
func (a *App) requeueTerminalSyncJob(ctx context.Context, port syncJobConflictPort, name, jobID, expectedType string, detail *jobs.Detail) error {
	err := port.Requeue(ctx, jobID, detail.Attempts, jobs.WithRequeueTerminal())
	if err == nil || errors.Is(err, jobs.ErrRequeueNotTerminal) {
		return nil
	}
	if !errors.Is(err, jobs.ErrRequeueWrongAttempt) {
		return fmt.Errorf("plugin: enqueue sync job %q: conflict requeue %q: %w", name, jobID, err)
	}
	live, getErr := port.Get(ctx, jobID)
	if getErr != nil {
		return fmt.Errorf("plugin: enqueue sync job %q: conflict requeue reload %q: %w", name, jobID, getErr)
	}
	if live == nil {
		return fmt.Errorf("plugin: enqueue sync job %q: conflict requeue %q: job missing after WrongAttempt", name, jobID)
	}
	if live.Type != expectedType {
		return fmt.Errorf("plugin: enqueue sync job %q: job id %q has type %q, want %q (refusing to resume foreign job)",
			name, jobID, live.Type, expectedType)
	}
	kind := classifySyncJobConflictStatus(live.Status)
	switch kind {
	case syncJobConflictInFlight:
		return nil
	case syncJobConflictTerminal:
		err = port.Requeue(ctx, jobID, live.Attempts, jobs.WithRequeueTerminal())
		if err == nil || errors.Is(err, jobs.ErrRequeueNotTerminal) {
			return nil
		}
		return fmt.Errorf("plugin: enqueue sync job %q: conflict requeue retry %q: %w", name, jobID, err)
	default:
		return fmt.Errorf("plugin: enqueue sync job %q: job id %q has unsupported status %q after WrongAttempt", name, jobID, live.Status)
	}
}

// SyncJobs returns a copy of registered sync jobs.
func (a *App) SyncJobs() []SyncJobRegistration {
	if len(a.syncJobs) == 0 {
		return nil
	}
	out := make([]SyncJobRegistration, len(a.syncJobs))
	copy(out, a.syncJobs)
	return out
}
