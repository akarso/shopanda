package plugin

import (
	"fmt"
	"net/http"

	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/pkg/extapi"
)

// Integration exposes inbound integration route registration scoped to a plugin slug.
type Integration struct {
	app  *App
	slug string
}

// Integration returns plugin-facing integration route registration for pluginSlug.
// pluginSlug becomes the {plugin} segment in /api/v1/integrations/{plugin}/….
func (a *App) Integration(pluginSlug string) *Integration {
	return &Integration{app: a, slug: pluginSlug}
}

// RegisterRoute registers a public integration route under /api/v1/integrations/{plugin}/….
func (i *Integration) RegisterRoute(method, path string, handler http.Handler) error {
	if i == nil || i.app == nil {
		return fmt.Errorf("plugin: integration app not configured")
	}
	if handler == nil {
		return fmt.Errorf("plugin: integration route handler must not be nil")
	}
	pattern, err := extapi.IntegrationRoutePattern(i.slug, method, path)
	if err != nil {
		return fmt.Errorf("plugin: integration route: %w", err)
	}
	return i.app.RegisterPublicRoute(pattern, handler)
}

// RegisterAdminRoute registers an admin integration route under /api/v1/admin/integrations/{plugin}/….
func (i *Integration) RegisterAdminRoute(method, path string, perm rbac.Permission, handler http.Handler) error {
	if i == nil || i.app == nil {
		return fmt.Errorf("plugin: integration app not configured")
	}
	if handler == nil {
		return fmt.Errorf("plugin: integration route handler must not be nil")
	}
	pattern, err := extapi.IntegrationAdminRoutePattern(i.slug, method, path)
	if err != nil {
		return fmt.Errorf("plugin: integration admin route: %w", err)
	}
	return i.app.RegisterAdminRoute(pattern, perm, handler)
}
