package main

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// bindProductPorts binds catalog/order/customer read ports and CapDB before
// plugin Init. Cart is late-bound after cart.Service exists in serve via
// BindCartPort. Catalog seed write (CapCatalogSeed) is CLI-only — see
// bindCatalogSeedPort; serve/worker skip it via assertServeNeedsMet /
// assertWorkerNeedsMet (plugin_boot.go).
func bindProductPorts(app *plugin.App, boot *plugin.Bootstrap) error {
	if app == nil {
		return nil
	}
	if boot != nil && boot.DB != nil {
		app.MarkCapabilitySatisfied(plugin.CapDB)
	}
	if boot != nil && (boot.Products != nil || boot.Variants != nil) {
		if err := app.BindCatalogPort(pluginsdk.NewCatalogPort(boot.Products, boot.Variants)); err != nil {
			return err
		}
	}
	if boot != nil && boot.Orders != nil {
		if err := app.BindOrdersPort(pluginsdk.NewOrdersPort(boot.Orders)); err != nil {
			return err
		}
	}
	if boot != nil && boot.Customers != nil {
		if err := app.BindCustomerPort(pluginsdk.NewCustomerPort(boot.Customers)); err != nil {
			return err
		}
	}
	return nil
}

// bindCatalogSeedPort wires CapCatalogSeed for CLI seed/import composition
// roots. Serve and worker must not call this — they skip CapCatalogSeed via
// assertServeNeedsMet / assertWorkerNeedsMet so plugins that Need it for
// CLI-only commands can still boot without catalog write access.
//
// Seed CLI pattern: bindProductPorts + bindCatalogSeedPort, then
// assertSeedCLINeedsMet (plugin_boot.go) — do not hand-copy skip lists.
func bindCatalogSeedPort(app *plugin.App, boot *plugin.Bootstrap) error {
	if app == nil {
		return nil
	}
	if boot == nil {
		return fmt.Errorf("bindCatalogSeedPort: bootstrap is required")
	}
	if boot.Products == nil || boot.Variants == nil ||
		boot.Prices == nil || boot.PriceHistory == nil || boot.Stock == nil {
		return fmt.Errorf("bindCatalogSeedPort: products, variants, prices, price history, and stock are required")
	}
	// Avoid boxing a typed-nil *sql.DB into any (would look non-nil to NewCatalogSeedPort).
	var db any
	if boot.DB != nil {
		db = boot.DB
	}
	seedPort, err := pluginsdk.NewCatalogSeedPort(
		boot.Products, boot.Variants, boot.Prices, boot.PriceHistory, boot.Stock, db,
	)
	if err != nil {
		return err
	}
	return app.BindCatalogSeedPort(seedPort)
}
