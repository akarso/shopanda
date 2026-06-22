package main

import (
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/core"
	"github.com/akarso/shopanda/plugins/example"
)

func registerPlugins(registry *plugin.Registry, cfg *config.Config) {
	core.Register(registry, cfg)
	if cfg.Plugins.Example.Enabled {
		registry.Register(example.New())
	}
}
