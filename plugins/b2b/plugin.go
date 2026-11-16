package b2b

import (
	"fmt"
	"net/http"

	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/b2b/groups"
	b2bpricing "github.com/akarso/shopanda/plugins/b2b/pricing"
)

// RoleAdmin and RoleManager name the built-in roles entitled to B2B admin
// endpoints. Untyped string constants convert implicitly to
// internal/domain/identity.Role at the Host.RegisterPermission boundary.
const (
	RoleAdmin   = "admin"
	RoleManager = "manager"
)

// Plugin is the commercial B2B extension module.
type Plugin struct{}

// New returns the B2B plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return pluginName }

func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("b2b plugin: app not configured")
	}
	if !h.ConfigBool("plugins.b2b.enabled") {
		return fmt.Errorf("b2b plugin: disabled (plugins.b2b.enabled=false)")
	}

	ok, err := Validate(h.ConfigValue("plugins.b2b.license_key"))
	if err != nil {
		return fmt.Errorf("b2b plugin: %w", err)
	}
	if !ok {
		return fmt.Errorf("b2b plugin: invalid or missing license (set plugins.b2b.license_key)")
	}

	h.Require(pluginsdk.CapDB)
	h.Require(pluginsdk.CapCustomers)
	h.Require(pluginsdk.CapCatalog)

	db, hasDB := h.BootstrapDB()
	if !hasDB {
		return fmt.Errorf("b2b plugin: database bootstrap not configured")
	}
	customers, hasCustomers := h.Customers()
	if !hasCustomers {
		return fmt.Errorf("b2b plugin: customer repository not configured")
	}
	variants, hasVariants := h.Catalog()
	if !hasVariants {
		return fmt.Errorf("b2b plugin: variant repository not configured")
	}

	groupRepo, err := groups.NewPostgresRepo(db)
	if err != nil {
		return fmt.Errorf("b2b plugin: groups repo: %w", err)
	}
	groupPriceRepo, err := b2bpricing.NewPostgresGroupPriceRepo(db)
	if err != nil {
		return fmt.Errorf("b2b plugin: group price repo: %w", err)
	}

	if err := h.RegisterPermission(PermissionGroupsRead, RoleAdmin, RoleManager); err != nil {
		return fmt.Errorf("b2b plugin: register groups read permission: %w", err)
	}
	if err := h.RegisterPermission(PermissionGroupsWrite, RoleAdmin, RoleManager); err != nil {
		return fmt.Errorf("b2b plugin: register groups write permission: %w", err)
	}
	if err := h.RegisterPermission(PermissionPricesRead, RoleAdmin, RoleManager); err != nil {
		return fmt.Errorf("b2b plugin: register prices read permission: %w", err)
	}
	if err := h.RegisterPermission(PermissionPricesWrite, RoleAdmin, RoleManager); err != nil {
		return fmt.Errorf("b2b plugin: register prices write permission: %w", err)
	}

	h.RegisterPricingStep(b2bpricing.NewGroupPriceStep(groupRepo, groupPriceRepo))

	groupAdmin, priceAdmin := newAdminHandlers(customers, variants, groupRepo, groupPriceRepo)

	routes := []struct {
		pattern string
		perm    string
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
		if err := h.RegisterAdminRoute(route.pattern, route.perm, route.handler); err != nil {
			return fmt.Errorf("b2b plugin: register %s: %w", route.pattern, err)
		}
	}

	h.Logger().Info("b2b plugin: customer groups and group pricing registered", nil)
	return nil
}

func newAdminHandlers(
	customers pluginsdk.CustomerPort,
	variants pluginsdk.CatalogPort,
	groupRepo groups.Repository,
	groupPriceRepo b2bpricing.GroupPriceRepository,
) (*groups.AdminHandler, *b2bpricing.AdminHandler) {
	return groups.NewAdminHandler(groupRepo, customers),
		b2bpricing.NewAdminHandler(groupRepo, groupPriceRepo, variants)
}
