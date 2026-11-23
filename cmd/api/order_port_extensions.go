package main

import (
	"context"
	"database/sql"
	"fmt"

	extensionApp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// attachOrderLineExtensions loads persisted extension field definitions into
// app's registry, builds a ValueService, and attaches it to the bound
// OrdersPort. Call after InitAll in every process that runs plugin code with
// CapOrders (serve and dedicated worker). No-op when OrdersPort is unbound.
func attachOrderLineExtensions(ctx context.Context, conn *sql.DB, app *plugin.App, log logger.Logger) error {
	if app == nil || conn == nil {
		return nil
	}
	ordersPort, ok := app.OrdersPort()
	if !ok {
		return nil
	}
	reg := app.ExtensionRegistry()
	if reg == nil {
		return fmt.Errorf("attach order line extensions: extension registry not set")
	}
	fieldRepo, err := postgres.NewExtensionFieldRepo(conn)
	if err != nil {
		return fmt.Errorf("attach order line extensions: field repo: %w", err)
	}
	valueRepo, err := postgres.NewExtensionValueRepo(conn)
	if err != nil {
		return fmt.Errorf("attach order line extensions: value repo: %w", err)
	}
	if err := reg.LoadPersisted(ctx, fieldRepo, log); err != nil {
		return fmt.Errorf("attach order line extensions: load fields: %w", err)
	}
	app.WarnUnownedPluginOnlyFields()
	pluginsdk.AttachOrderLineExtensions(ordersPort, extensionApp.NewValueService(reg, valueRepo))
	return nil
}
