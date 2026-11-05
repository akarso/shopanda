package main

import (
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// resolveAndBindJobQueue resolves the active jobs.Queue (plugin-registered when
// present) and binds it for Integration.Enqueue. Shared by serve, worker,
// scheduler, and CLIs so resolve+bind cannot drift across processes.
func resolveAndBindJobQueue(app *plugin.App, conn *sql.DB, cfg *config.Config) (jobs.Queue, error) {
	jobQueue, err := resolveJobQueue(app, conn, cfg)
	if err != nil {
		return nil, fmt.Errorf("job queue: %w", err)
	}
	if err := app.BindQueue(jobQueue); err != nil {
		return nil, err
	}
	return jobQueue, nil
}
