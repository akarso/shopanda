package core

import (
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	coremeili "github.com/akarso/shopanda/plugins/core/meilisearch"
	corepostgres "github.com/akarso/shopanda/plugins/core/postgres"
)

// Register adds core infrastructure plugins implied by the active driver switches.
func Register(registry *plugin.Registry, cfg *config.Config) {
	if cfg.CorePostgresSearchEnabled() {
		registry.Register(corepostgres.NewSearchPlugin())
	} else if cfg.CoreMeilisearchSearchEnabled() {
		registry.Register(coremeili.NewSearchPlugin())
	}
	if cfg.CorePostgresCacheEnabled() {
		registry.Register(corepostgres.NewCachePlugin())
	}
	if cfg.CorePostgresQueueEnabled() {
		registry.Register(corepostgres.NewQueuePlugin())
	}
}
