package postgres

import (
	"fmt"

	inpostgres "github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// SearchPlugin registers the Postgres full-text search backend.
type SearchPlugin struct{}

func NewSearchPlugin() *SearchPlugin { return &SearchPlugin{} }

func (p *SearchPlugin) Name() string { return "core/postgres-search" }

func (p *SearchPlugin) Init(app *plugin.App) error {
	if app.Bootstrap == nil || app.Bootstrap.DB == nil {
		return fmt.Errorf("postgres search: database not configured")
	}
	se, err := inpostgres.NewSearchEngine(app.Bootstrap.DB)
	if err != nil {
		return err
	}
	app.RegisterSearchProvider(se)
	return nil
}
