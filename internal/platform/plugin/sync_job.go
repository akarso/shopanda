package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/requestctx"
	"github.com/akarso/shopanda/pkg/extapi"
)

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
// runs from cron/event. Cross-plugin enqueue (calling Integration with another
// plugin's slug) is allowed, matching RegisterSyncJob; ownership checks are
// out of scope.
func (i *Integration) Enqueue(ctx context.Context, name string, payload map[string]interface{}) error {
	if i == nil || i.app == nil {
		return fmt.Errorf("plugin: integration app not configured")
	}
	return i.app.enqueueSyncJob(ctx, i.slug, name, payload)
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
	case extapi.SyncTriggerEvent:
		eventName := strings.TrimSpace(job.Trigger.EventName)
		if eventName == "" {
			return fmt.Errorf("plugin: sync job %q: event name required", name)
		}
		reg.Job.Trigger.EventName = eventName
	case extapi.SyncTriggerManual:
		// On-demand only; no cron/event fields.
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

// NewSyncJobEnvelope builds the queue job for a sync registration.
// trigger is stored in the envelope (cron / event / manual). payload is
// shallow-copied so later caller mutations are not visible to the queue.
func NewSyncJobEnvelope(reg SyncJobRegistration, trigger string, payload map[string]interface{}) (jobs.Job, error) {
	copied := shallowCopyStringMap(payload)
	if copied == nil {
		copied = map[string]interface{}{}
	}
	name := strings.TrimSpace(reg.Job.Name)
	job, err := jobs.NewJob(id.New(), reg.JobType, map[string]interface{}{
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
// and the key is not already set, stores it as "request_id". Callers that
// already set request_id keep their value. Empty / missing ctx IDs are a no-op.
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
	if _, exists := out["request_id"]; exists {
		return out
	}
	out["request_id"] = rid
	return out
}

func (a *App) enqueueSyncJob(ctx context.Context, pluginSlug, name string, payload map[string]interface{}) error {
	if a == nil {
		return fmt.Errorf("plugin: app is nil")
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
	job, err := NewSyncJobEnvelope(reg, extapi.SyncTriggerManual, payload)
	if err != nil {
		return fmt.Errorf("plugin: enqueue sync job %q: %w", name, err)
	}
	if err := a.boundQueue.Enqueue(ctx, job); err != nil {
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

// SyncJobs returns a copy of registered sync jobs.
func (a *App) SyncJobs() []SyncJobRegistration {
	if len(a.syncJobs) == 0 {
		return nil
	}
	out := make([]SyncJobRegistration, len(a.syncJobs))
	copy(out, a.syncJobs)
	return out
}
