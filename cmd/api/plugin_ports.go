package main

import (
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// bindProductPorts binds catalog/order/customer read ports, catalog seed write
// port, and CapDB before plugin Init. Cart is late-bound after cart.Service
// exists in serve via BindCartPort.
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
	if boot != nil && boot.Products != nil && boot.Variants != nil &&
		boot.Prices != nil && boot.PriceHistory != nil && boot.Stock != nil {
		if err := app.BindCatalogSeedPort(pluginsdk.NewCatalogSeedPort(
			boot.Products, boot.Variants, boot.Prices, boot.PriceHistory, boot.Stock, boot.DB,
		)); err != nil {
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
