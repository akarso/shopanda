package postgres

import (
	"fmt"

	inpostgres "github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// CachePlugin registers the Postgres UNLOGGED-table cache backend.
type CachePlugin struct{}

func NewCachePlugin() *CachePlugin { return &CachePlugin{} }

func (p *CachePlugin) Name() string { return "core/postgres-cache" }

func (p *CachePlugin) Init(app *plugin.App) error {
	if app.Bootstrap == nil || app.Bootstrap.DB == nil {
		return fmt.Errorf("postgres cache: database not configured")
	}
	cs, err := inpostgres.NewCacheStore(app.Bootstrap.DB)
	if err != nil {
		return err
	}
	app.RegisterCache(cs)
	return nil
}
