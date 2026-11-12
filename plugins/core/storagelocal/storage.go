package storagelocal

import (
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/infrastructure/localfs"
	"github.com/akarso/shopanda/internal/platform/config"
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
	secret := []byte(strings.TrimSpace(app.Config.Media.Local.SignSecret))
	var st *localfs.Storage
	var err error
	if len(secret) == 0 {
		if !config.DevModeEnabled() {
			return fmt.Errorf("local storage: media.local.sign_secret required when media.storage=local")
		}
		st, err = localfs.NewEphemeral(app.Config.Media.Local.BasePath, app.Config.Media.Local.BaseURL)
	} else {
		st, err = localfs.NewWithSecret(app.Config.Media.Local.BasePath, app.Config.Media.Local.BaseURL, secret)
	}
	if err != nil {
		return fmt.Errorf("local storage: %w", err)
	}
	app.RegisterMediaStorage(st)
	return nil
}
