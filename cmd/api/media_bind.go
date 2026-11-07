package main

import (
	"fmt"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/infrastructure/localfs"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// resolveAndBindMediaStorage resolves the active media.Storage (plugin-
// registered when present) and binds it for BoundMediaStorage readers.
func resolveAndBindMediaStorage(app *plugin.App, cfg *config.Config) (media.Storage, error) {
	st, err := resolveMediaStorage(app, cfg)
	if err != nil {
		return nil, fmt.Errorf("media storage: %w", err)
	}
	if err := app.BindMediaStorage(st); err != nil {
		return nil, err
	}
	return st, nil
}

func resolveMediaStorage(app *plugin.App, cfg *config.Config) (media.Storage, error) {
	if st, ok := app.MediaStorage(); ok {
		return st, nil
	}

	switch cfg.Media.Storage {
	case "s3":
		return nil, fmt.Errorf("media: s3 storage configured but no storage plugin registered (core plugin init failed?)")
	case "local":
		var secret []byte
		if cfg.Media.Local.SignSecret != "" {
			secret = []byte(cfg.Media.Local.SignSecret)
		}
		return localfs.NewWithSecret(cfg.Media.Local.BasePath, cfg.Media.Local.BaseURL, secret), nil
	default:
		return nil, fmt.Errorf("unsupported media.storage: %s", cfg.Media.Storage)
	}
}
