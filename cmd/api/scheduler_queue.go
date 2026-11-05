package main

import (
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// bindSchedulerQueue resolves the active jobs.Queue (plugin-registered when
// present) and BindQueue's it for Integration.Enqueue. Shared by runScheduler
// so a mistaken second NewJobQueue cannot diverge cron/Enqueue from the worker.
func bindSchedulerQueue(app *plugin.App, conn *sql.DB, cfg *config.Config) (jobs.Queue, error) {
	jobQueue, err := resolveJobQueue(app, conn, cfg)
	if err != nil {
		return nil, fmt.Errorf("job queue: %w", err)
	}
	if err := app.BindQueue(jobQueue); err != nil {
		return nil, err
	}
	return jobQueue, nil
}
