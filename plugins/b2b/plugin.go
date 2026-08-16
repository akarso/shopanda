package b2b

import (
	"fmt"
	"net/http"

	"github.com/akarso/shopanda/internal/domain/customergroup"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/b2b/groups"
	b2bpricing "github.com/akarso/shopanda/plugins/b2b/pricing"
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
	if app.Bootstrap.Customers == nil {
		return fmt.Errorf("b2b plugin: customer repository not configured")
	}
	if app.Bootstrap.Variants == nil {
		return fmt.Errorf("b2b plugin: variant repository not configured")
	}

	if err := runPluginMigrations(app.Bootstrap.DB); err != nil {
		return fmt.Errorf("b2b plugin: %w", err)
	}

	groupRepo, err := groups.NewPostgresRepo(app.Bootstrap.DB)
	if err != nil {
		return fmt.Errorf("b2b plugin: groups repo: %w", err)
	}
	groupPriceRepo, err := b2bpricing.NewPostgresGroupPriceRepo(app.Bootstrap.DB)
	if err != nil {
		return fmt.Errorf("b2b plugin: group price repo: %w", err)
	}

	if err := app.RegisterPermission(PermissionGroupsRead, identity.RoleAdmin, identity.RoleManager); err != nil {
		return fmt.Errorf("b2b plugin: register groups read permission: %w", err)
	}
	if err := app.RegisterPermission(PermissionGroupsWrite, identity.RoleAdmin, identity.RoleManager); err != nil {
		return fmt.Errorf("b2b plugin: register groups write permission: %w", err)
	}
	if err := app.RegisterPermission(PermissionPricesRead, identity.RoleAdmin, identity.RoleManager); err != nil {
		return fmt.Errorf("b2b plugin: register prices read permission: %w", err)
	}
	if err := app.RegisterPermission(PermissionPricesWrite, identity.RoleAdmin, identity.RoleManager); err != nil {
		return fmt.Errorf("b2b plugin: register prices write permission: %w", err)
	}

	app.RegisterPricingStep(b2bpricing.NewGroupPriceStep(groupRepo, groupPriceRepo))

	groupAdmin, priceAdmin := newAdminHandlers(app.Bootstrap, groupRepo, groupPriceRepo)

	routes := []struct {
		pattern string
		perm    rbac.Permission
		handler http.HandlerFunc
	}{
		{"GET /api/v1/admin/customer-groups", PermissionGroupsRead, groupAdmin.List()},
		{"GET /api/v1/admin/customer-groups/{groupId}", PermissionGroupsRead, groupAdmin.Get()},
		{"POST /api/v1/admin/customer-groups", PermissionGroupsWrite, groupAdmin.Create()},
		{"PUT /api/v1/admin/customer-groups/{groupId}", PermissionGroupsWrite, groupAdmin.Update()},
		{"DELETE /api/v1/admin/customer-groups/{groupId}", PermissionGroupsWrite, groupAdmin.Delete()},
		{"GET /api/v1/admin/customers/{customerId}/customer-group", PermissionGroupsRead, groupAdmin.GetCustomerGroup()},
		{"PUT /api/v1/admin/customers/{customerId}/customer-group", PermissionGroupsWrite, groupAdmin.AssignCustomer()},
		{"DELETE /api/v1/admin/customers/{customerId}/customer-group", PermissionGroupsWrite, groupAdmin.RemoveCustomer()},
		{"GET /api/v1/admin/customer-groups/{groupId}/variants/{variantId}/price", PermissionPricesRead, priceAdmin.Get()},
		{"PUT /api/v1/admin/customer-groups/{groupId}/variants/{variantId}/price", PermissionPricesWrite, priceAdmin.Update()},
		{"DELETE /api/v1/admin/customer-groups/{groupId}/variants/{variantId}/price", PermissionPricesWrite, priceAdmin.Delete()},
	}
	for _, route := range routes {
		if err := app.RegisterAdminRoute(route.pattern, route.perm, route.handler); err != nil {
			return fmt.Errorf("b2b plugin: register %s: %w", route.pattern, err)
		}
	}

	if app.Logger != nil {
		app.Logger.Info("b2b plugin: customer groups and group pricing registered", nil)
	}
	return nil
}

func newAdminHandlers(
	boot *plugin.Bootstrap,
	groupRepo customergroup.Repository,
	groupPriceRepo customergroup.GroupPriceRepository,
) (*groups.AdminHandler, *b2bpricing.AdminHandler) {
	return groups.NewAdminHandler(groupRepo, boot.Customers),
		b2bpricing.NewAdminHandler(groupRepo, groupPriceRepo, boot.Variants)
}
