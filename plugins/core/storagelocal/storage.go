package storagelocal

import (
	"fmt"

	"github.com/akarso/shopanda/internal/infrastructure/localfs"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// StoragePlugin registers the local filesystem media storage backend.
type StoragePlugin struct{}

func NewStoragePlugin() *StoragePlugin { return &StoragePlugin{} }

func (p *StoragePlugin) Name() string { return "core/storage-local" }

func (p *StoragePlugin) Init(app *plugin.App) error {
	if app.Config == nil {
		return fmt.Errorf("local storage: config not configured")
	}
	storage := app.Config.Media.Storage
	if storage != "" && storage != "local" {
		return fmt.Errorf("local storage: disabled (media.storage=%q)", storage)
	}
	if app.Config.Media.Local.BasePath == "" {
		return fmt.Errorf("local storage: empty base_path")
	}
	if app.Config.Media.Local.BaseURL == "" {
		return fmt.Errorf("local storage: empty base_url")
	}
	app.RegisterMediaStorage(localfs.New(app.Config.Media.Local.BasePath, app.Config.Media.Local.BaseURL))
	return nil
}
