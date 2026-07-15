package plugin

import (
	"fmt"
	"net/http"

	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/integrationhttp"
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

// RegisterSecureRoute registers a public integration route wrapped with auth middleware.
// When an idempotency store is configured on the app, mutating requests with Idempotency-Key are deduplicated.
func (i *Integration) RegisterSecureRoute(method, path string, auth integrationhttp.AuthConfig, handler http.Handler) error {
	if auth.PluginSlug == "" {
		slug, err := extapi.NormalizePluginSlug(i.slug)
		if err != nil {
			return fmt.Errorf("plugin: integration secure route: %w", err)
		}
		auth.PluginSlug = slug
	}
	secured := handler
	if store := i.app.IntegrationIdempotencyStore(); store != nil {
		secured = integrationhttp.IdempotencyHandler(idempotencyConfig(i.app, i.slug), secured)
	}
	return i.RegisterRoute(method, path, integrationhttp.SecureHandler(auth, secured))
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
