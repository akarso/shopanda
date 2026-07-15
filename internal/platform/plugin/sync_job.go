package plugin

import (
	"fmt"
	"strings"

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

// SyncJobs returns a copy of registered sync jobs.
func (a *App) SyncJobs() []SyncJobRegistration {
	if len(a.syncJobs) == 0 {
		return nil
	}
	out := make([]SyncJobRegistration, len(a.syncJobs))
	copy(out, a.syncJobs)
	return out
}
