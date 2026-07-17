package pluginsdk

import (
	"github.com/akarso/shopanda/pkg/extapi"
)

// SyncJobOption configures optional sync job fields.
type SyncJobOption func(*extapi.SyncJob)

// MaxRetries sets the queue retry limit for a sync job.
func MaxRetries(n int) SyncJobOption {
	return func(job *extapi.SyncJob) {
		if n > 0 {
			job.MaxRetries = n
		}
	}
}

// SyncJobs registers outbound integration sync jobs for one integration slug.
type SyncJobs struct {
	sdk  *SDK
	slug string
}

// Integration returns sync job helpers for slug (/api/v1/integrations/{slug}/… namespace).
func (s *SDK) Integration(slug string) *SyncJobs {
	return &SyncJobs{sdk: s, slug: slug}
}

// RegisterCron registers a cron-triggered sync job.
func (sj *SyncJobs) RegisterCron(name, cronSpec string, handler extapi.SyncJobHandler, opts ...SyncJobOption) error {
	job := extapi.SyncJob{
		Name:    name,
		Trigger: extapi.Cron(cronSpec),
		Handler: handler,
	}
	for _, opt := range opts {
		opt(&job)
	}
	return sj.register(job)
}

// RegisterOnEvent registers an event-triggered sync job.
func (sj *SyncJobs) RegisterOnEvent(name, eventName string, handler extapi.SyncJobHandler, opts ...SyncJobOption) error {
	job := extapi.SyncJob{
		Name:    name,
		Trigger: extapi.OnEvent(eventName),
		Handler: handler,
	}
	for _, opt := range opts {
		opt(&job)
	}
	return sj.register(job)
}

func (sj *SyncJobs) register(job extapi.SyncJob) error {
	return sj.sdk.app.Integration(sj.slug).RegisterSyncJob(job)
}
