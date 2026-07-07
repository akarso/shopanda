package plugin

import (
	"fmt"

	assetsapp "github.com/akarso/shopanda/internal/application/assets"
)

// Assets exposes asset manifest registration to plugins during Init.
type Assets struct {
	registry   *assetsapp.Registry
	registrant string
}

// Register adds a static asset manifest entry for the current plugin.
func (a *Assets) Register(manifest assetsapp.Manifest) error {
	if a == nil || a.registry == nil {
		return fmt.Errorf("plugin: asset registry not configured")
	}
	return a.registry.Register(a.registrant, manifest)
}

// SetAssetRegistry wires the shared asset registry before plugin Init.
func (app *App) SetAssetRegistry(registry *assetsapp.Registry) {
	if registry == nil {
		panic("plugin: asset registry must not be nil")
	}
	app.assetRegistryMu.Lock()
	defer app.assetRegistryMu.Unlock()
	app.assetRegistry = registry
}

// AssetRegistry returns the shared asset registry.
func (app *App) AssetRegistry() *assetsapp.Registry {
	app.assetRegistryMu.Lock()
	defer app.assetRegistryMu.Unlock()
	return app.assetRegistry
}

// Assets returns plugin-facing asset registration scoped to registrant.
func (app *App) Assets(registrant string) *Assets {
	app.assetRegistryMu.Lock()
	defer app.assetRegistryMu.Unlock()
	if app.assetRegistry == nil {
		app.assetRegistry = assetsapp.NewRegistry()
	}
	return &Assets{registry: app.assetRegistry, registrant: registrant}
}
