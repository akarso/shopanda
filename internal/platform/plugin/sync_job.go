package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/id"
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
// The composition root must BindQueue the resolved jobs.Queue after InitAll.
// Payload may be nil (stored as an empty object). The job payload trigger is
// always "manual" so handlers can distinguish on-demand runs from cron/event.
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
	jobType, err := extapi.SyncJobType(pluginSlug, name)
	if err != nil {
		return fmt.Errorf("plugin: sync job %q: %w", name, err)
	}
	reg := SyncJobRegistration{
		PluginSlug: pluginSlug,
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
	var reg *SyncJobRegistration
	for i := range a.syncJobs {
		if a.syncJobs[i].JobType == jobType {
			reg = &a.syncJobs[i]
			break
		}
	}
	if reg == nil {
		return fmt.Errorf("plugin: enqueue sync job %q: %w", name, extapi.ErrSyncJobNotFound)
	}
	queue, ok := a.Queue()
	if !ok || queue == nil {
		return fmt.Errorf("plugin: enqueue sync job %q: %w", name, extapi.ErrSyncJobQueueUnbound)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	job, err := jobs.NewJob(id.New(), reg.JobType, map[string]interface{}{
		"plugin":  reg.PluginSlug,
		"name":    reg.Job.Name,
		"trigger": extapi.SyncTriggerManual,
		"payload": payload,
	})
	if err != nil {
		return err
	}
	if reg.Job.MaxRetries > 0 {
		job.MaxRetries = reg.Job.MaxRetries
	}
	if err := queue.Enqueue(ctx, job); err != nil {
		return err
	}
	if a.Logger != nil {
		a.Logger.Info("integration.sync_job.enqueued", map[string]interface{}{
			"job_id":   job.ID,
			"job_type": job.Type,
			"trigger":  extapi.SyncTriggerManual,
		})
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
