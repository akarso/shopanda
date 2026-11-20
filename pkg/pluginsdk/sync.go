package pluginsdk

import (
	"context"
	"net/http"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/integrationhttp"
)

// SyncJobOption configures optional sync job fields at Register* time.
type SyncJobOption func(*extapi.SyncJob)

// MaxRetries sets the queue retry limit for a sync job.
func MaxRetries(n int) SyncJobOption {
	return func(job *extapi.SyncJob) {
		if n > 0 {
			job.MaxRetries = n
		}
	}
}

// EnqueueOption configures SyncJobs.Enqueue (run-now).
type EnqueueOption func(*enqueueConfig)

type enqueueConfig struct {
	opts []plugin.SyncEnqueueOption
}

// WithJobID sets the queue job id for idempotent enqueue (one job per entity).
// A duplicate id returns an error for which IsConflict is true (Conflict is
// not wrapped away). Resume with Host.Jobs().Get / Requeue when CapJobs is bound.
// Empty / whitespace-only ids fail closed.
func WithJobID(jobID string) EnqueueOption {
	return func(c *enqueueConfig) {
		c.opts = append(c.opts, plugin.WithJobID(jobID))
	}
}

func applyEnqueueOptions(opts []EnqueueOption) []plugin.SyncEnqueueOption {
	var cfg enqueueConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg.opts
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

// RegisterManual registers an on-demand sync job (no cron/event wiring).
// For event-driven durable work prefer RegisterOnEvent (envelope trigger=event).
// Use RegisterManual + Enqueue for true run-now, or for custom/conditional
// payload shaping that RegisterOnEvent's eventPayload cannot express.
// Call Enqueue only after the composition root binds the resolved jobs.Queue.
func (sj *SyncJobs) RegisterManual(name string, handler extapi.SyncJobHandler, opts ...SyncJobOption) error {
	job := extapi.SyncJob{
		Name:    name,
		Trigger: extapi.Manual(),
		Handler: handler,
	}
	for _, opt := range opts {
		opt(&job)
	}
	return sj.register(job)
}

// Enqueue queues a previously registered sync job for this slug ("run now").
// Works for manual, cron, or event jobs; the envelope trigger is always "manual".
// Use WithJobID for idempotent one-job-per-entity enqueue; duplicates return
// IsConflict so callers can Get/Requeue the existing job via Host.Jobs().
func (sj *SyncJobs) Enqueue(ctx context.Context, name string, payload map[string]interface{}, opts ...EnqueueOption) error {
	return coreApp(sj.sdk.host).Integration(sj.slug).Enqueue(ctx, name, payload, applyEnqueueOptions(opts)...)
}

func (sj *SyncJobs) register(job extapi.SyncJob) error {
	return coreApp(sj.sdk.host).Integration(sj.slug).RegisterSyncJob(job)
}

// RegisterRoute registers a public inbound integration route under
// /api/v1/integrations/{slug}/….
func (sj *SyncJobs) RegisterRoute(method, path string, handler http.Handler) error {
	return coreApp(sj.sdk.host).Integration(sj.slug).RegisterRoute(method, path, handler)
}

// RegisterSecureRoute registers an inbound integration route wrapped with
// API key / HMAC auth middleware (and idempotency dedup when configured).
func (sj *SyncJobs) RegisterSecureRoute(method, path string, auth integrationhttp.AuthConfig, handler http.Handler) error {
	return coreApp(sj.sdk.host).Integration(sj.slug).RegisterSecureRoute(method, path, auth, handler)
}
