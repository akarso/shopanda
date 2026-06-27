package b2b

import (
	"fmt"
	"net/http"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/b2b/groups"
)

// Plugin is the commercial B2B extension module.
type Plugin struct{}

// New returns the B2B plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "shopanda/b2b" }

func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("b2b plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("b2b plugin: config not configured")
	}
	if !app.Config.Plugins.B2B.Enabled {
		return fmt.Errorf("b2b plugin: disabled (plugins.b2b.enabled=false)")
	}

	ok, err := Validate(app.Config.Plugins.B2B.LicenseKey)
	if err != nil {
		return fmt.Errorf("b2b plugin: %w", err)
	}
	if !ok {
		return fmt.Errorf("b2b plugin: invalid or missing license (set plugins.b2b.license_key)")
	}
	if app.Bootstrap == nil || app.Bootstrap.DB == nil {
		return fmt.Errorf("b2b plugin: database bootstrap not configured")
	}

	if err := runPluginMigrations(app.Bootstrap.DB); err != nil {
		return fmt.Errorf("b2b plugin: %w", err)
	}

	groupRepo, err := groups.NewPostgresRepo(app.Bootstrap.DB)
	if err != nil {
		return fmt.Errorf("b2b plugin: groups repo: %w", err)
	}
	customerRepo, err := postgres.NewCustomerRepo(app.Bootstrap.DB)
	if err != nil {
		return fmt.Errorf("b2b plugin: customer repo: %w", err)
	}

	if err := app.RegisterPermission(PermissionGroupsRead, identity.RoleAdmin, identity.RoleManager); err != nil {
		return fmt.Errorf("b2b plugin: register read permission: %w", err)
	}
	if err := app.RegisterPermission(PermissionGroupsWrite, identity.RoleAdmin, identity.RoleManager); err != nil {
		return fmt.Errorf("b2b plugin: register write permission: %w", err)
	}

	groupAdmin := groups.NewAdminHandler(groupRepo, customerRepo)

	app.RegisterAdminRoute("GET /api/v1/admin/customer-groups", PermissionGroupsRead, http.HandlerFunc(groupAdmin.List()))
	app.RegisterAdminRoute("GET /api/v1/admin/customer-groups/{groupId}", PermissionGroupsRead, http.HandlerFunc(groupAdmin.Get()))
	app.RegisterAdminRoute("POST /api/v1/admin/customer-groups", PermissionGroupsWrite, http.HandlerFunc(groupAdmin.Create()))
	app.RegisterAdminRoute("PUT /api/v1/admin/customer-groups/{groupId}", PermissionGroupsWrite, http.HandlerFunc(groupAdmin.Update()))
	app.RegisterAdminRoute("GET /api/v1/admin/customers/{customerId}/customer-group", PermissionGroupsRead, http.HandlerFunc(groupAdmin.GetCustomerGroup()))
	app.RegisterAdminRoute("PUT /api/v1/admin/customers/{customerId}/customer-group", PermissionGroupsWrite, http.HandlerFunc(groupAdmin.AssignCustomer()))
	app.RegisterAdminRoute("DELETE /api/v1/admin/customers/{customerId}/customer-group", PermissionGroupsWrite, http.HandlerFunc(groupAdmin.RemoveCustomer()))

	if app.Logger != nil {
		app.Logger.Info("b2b plugin: customer groups registered", nil)
	}
	return nil
}
