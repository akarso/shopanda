package postgres

import (
	"fmt"

	inpostgres "github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// QueuePlugin registers the Postgres job queue backend.
type QueuePlugin struct{}

func NewQueuePlugin() *QueuePlugin { return &QueuePlugin{} }

func (p *QueuePlugin) Name() string { return "core/postgres-queue" }

func (p *QueuePlugin) Init(app *plugin.App) error {
	if app.Bootstrap == nil || app.Bootstrap.DB == nil {
		return fmt.Errorf("postgres queue: database not configured")
	}
	q, err := inpostgres.NewJobQueue(app.Bootstrap.DB)
	if err != nil {
		return err
	}
	app.RegisterQueue(q)
	return nil
}
