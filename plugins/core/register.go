package core

import (
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	corepostgres "github.com/akarso/shopanda/plugins/core/postgres"
)

// Register adds core infrastructure plugins implied by the active driver switches.
func Register(registry *plugin.Registry, cfg *config.Config) {
	if cfg.CorePostgresSearchEnabled() {
		registry.Register(corepostgres.NewSearchPlugin())
	}
	if cfg.CorePostgresCacheEnabled() {
		registry.Register(corepostgres.NewCachePlugin())
	}
	if cfg.CorePostgresQueueEnabled() {
		registry.Register(corepostgres.NewQueuePlugin())
	}
}
