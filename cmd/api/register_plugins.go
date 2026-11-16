package main

import (
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/b2b"
	"github.com/akarso/shopanda/plugins/cartdemo"
	"github.com/akarso/shopanda/plugins/checkoutdemo"
	"github.com/akarso/shopanda/plugins/core"
	"github.com/akarso/shopanda/plugins/example"
	"github.com/akarso/shopanda/plugins/exportdemo"
	"github.com/akarso/shopanda/plugins/extref"
	"github.com/akarso/shopanda/plugins/importdemo"
	"github.com/akarso/shopanda/plugins/integrationdemo"
	"github.com/akarso/shopanda/plugins/maildemo"
	"github.com/akarso/shopanda/plugins/pimdemo"
	"github.com/akarso/shopanda/plugins/promodemo"
	"github.com/akarso/shopanda/plugins/slotsdemo"
	"github.com/akarso/shopanda/plugins/taxdemo"
	"github.com/akarso/shopanda/plugins/warehousedemo"
)

func registerPlugins(registry *plugin.Registry, cfg *config.Config) {
	core.Register(registry, cfg)
	if cfg.Plugins.Example.Enabled {
		registry.Register(pluginsdk.Adapt(example.New()))
	}
	if cfg.Plugins.SlotsDemo.Enabled {
		registry.Register(pluginsdk.Adapt(slotsdemo.New()))
	}
	if cfg.Plugins.CartDemo.Enabled {
		registry.Register(pluginsdk.Adapt(cartdemo.New()))
	}
	if cfg.Plugins.TaxDemo.Enabled {
		registry.Register(pluginsdk.Adapt(taxdemo.New()))
	}
	if cfg.Plugins.MailDemo.Enabled {
		registry.Register(pluginsdk.Adapt(maildemo.New()))
	}
	if cfg.Plugins.PromoDemo.Enabled {
		registry.Register(pluginsdk.Adapt(promodemo.New()))
	}
	if cfg.Plugins.ImportDemo.Enabled {
		registry.Register(pluginsdk.Adapt(importdemo.New()))
	}
	if cfg.Plugins.ExportDemo.Enabled {
		registry.Register(pluginsdk.Adapt(exportdemo.New()))
	}
	if cfg.Plugins.CheckoutDemo.Enabled {
		registry.Register(pluginsdk.Adapt(checkoutdemo.New()))
	}
	if cfg.Plugins.IntegrationDemo.Enabled {
		registry.Register(pluginsdk.Adapt(integrationdemo.New()))
	}
	if cfg.Plugins.WarehouseDemo.Enabled {
		registry.Register(pluginsdk.Adapt(warehousedemo.New()))
	}
	if cfg.Plugins.PimDemo.Enabled {
		registry.Register(pluginsdk.Adapt(pimdemo.New()))
	}
	if cfg.Plugins.B2B.Enabled {
		registry.Register(pluginsdk.Adapt(b2b.New()))
	}
	if cfg.Plugins.ExtRef.Enabled {
		registry.Register(pluginsdk.Adapt(extref.New()))
	}
}
