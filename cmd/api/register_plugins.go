package main

import (
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/b2b"
	"github.com/akarso/shopanda/plugins/cartdemo"
	"github.com/akarso/shopanda/plugins/importdemo"
	"github.com/akarso/shopanda/plugins/integrationdemo"
	"github.com/akarso/shopanda/plugins/pimdemo"
	"github.com/akarso/shopanda/plugins/warehousedemo"
	"github.com/akarso/shopanda/plugins/core"
	"github.com/akarso/shopanda/plugins/example"
	"github.com/akarso/shopanda/plugins/slotsdemo"
)

func registerPlugins(registry *plugin.Registry, cfg *config.Config) {
	core.Register(registry, cfg)
	if cfg.Plugins.Example.Enabled {
		registry.Register(example.New())
	}
	if cfg.Plugins.SlotsDemo.Enabled {
		registry.Register(slotsdemo.New())
	}
	if cfg.Plugins.CartDemo.Enabled {
		registry.Register(cartdemo.New())
	}
	if cfg.Plugins.ImportDemo.Enabled {
		registry.Register(importdemo.New())
	}
	if cfg.Plugins.IntegrationDemo.Enabled {
		registry.Register(integrationdemo.New())
	}
	if cfg.Plugins.WarehouseDemo.Enabled {
		registry.Register(warehousedemo.New())
	}
	if cfg.Plugins.PimDemo.Enabled {
		registry.Register(pimdemo.New())
	}
	if cfg.Plugins.B2B.Enabled {
		registry.Register(b2b.New())
	}
}
