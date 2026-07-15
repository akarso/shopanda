package plugin

import (
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/integrationhttp"
)

// SetIntegrationIdempotencyStore wires the shared idempotency store before plugin Init.
func (a *App) SetIntegrationIdempotencyStore(store integrationhttp.IdempotencyStore) {
	a.integrationIdempotencyMu.Lock()
	defer a.integrationIdempotencyMu.Unlock()
	a.integrationIdempotency = store
}

// IntegrationIdempotencyStore returns the shared idempotency store.
func (a *App) IntegrationIdempotencyStore() integrationhttp.IdempotencyStore {
	a.integrationIdempotencyMu.Lock()
	defer a.integrationIdempotencyMu.Unlock()
	return a.integrationIdempotency
}

func idempotencyConfig(app *App, pluginSlug string) integrationhttp.IdempotencyConfig {
	cfg := integrationhttp.IdempotencyConfig{
		Store: app.IntegrationIdempotencyStore(),
	}
	slug, err := extapi.NormalizePluginSlug(pluginSlug)
	if err == nil {
		cfg.PluginSlug = slug
	}
	return cfg
}
