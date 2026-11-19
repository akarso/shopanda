package main

import (
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// resolveAndBindJobQueue resolves the active jobs.Queue (plugin-registered when
// present) and binds it for Integration.Enqueue. Shared by serve, worker,
// scheduler, and CLIs so resolve+bind cannot drift across processes.
// When the queue also implements jobs.Reader, binds CapJobs / Host.Jobs().
func resolveAndBindJobQueue(app *plugin.App, conn *sql.DB, cfg *config.Config) (jobs.Queue, error) {
	jobQueue, err := resolveJobQueue(app, conn, cfg)
	if err != nil {
		return nil, fmt.Errorf("job queue: %w", err)
	}
	if app == nil {
		return nil, fmt.Errorf("job queue: plugin app is nil")
	}
	jobQueue, err = app.ApplyQueueBoundHooks(jobQueue)
	if err != nil {
		return nil, err
	}
	jobQueue = app.WrapRequeueNotify(jobQueue)
	if err := app.BindQueue(jobQueue); err != nil {
		return nil, err
	}
	if err := bindJobsPortFromQueue(app, jobQueue); err != nil {
		return nil, err
	}
	return jobQueue, nil
}

// bindJobsPortFromQueue satisfies CapJobs when q implements jobs.Reader
// (Postgres). Broker queues without a Reader leave CapJobs unbound so
// Need(CapJobs) fails closed instead of handing a half-port.
func bindJobsPortFromQueue(app *plugin.App, q jobs.Queue) error {
	reader, ok := q.(jobs.Reader)
	if !ok {
		return nil
	}
	port, err := pluginsdk.NewJobsPort(reader, q)
	if err != nil {
		return fmt.Errorf("job queue: jobs port: %w", err)
	}
	if err := app.BindJobsPort(port); err != nil {
		return fmt.Errorf("job queue: BindJobsPort: %w", err)
	}
	return nil
}
