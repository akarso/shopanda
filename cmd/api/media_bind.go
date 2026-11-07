package main

import (
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/infrastructure/localfs"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// resolveAndBindMediaStorage resolves the active media.Storage (plugin-
// registered when present) and binds it for BoundMediaStorage readers.
// Call from every process that product plugins may use for media (serve,
// worker, scheduler, CLIs) — same pattern as resolveAndBindJobQueue.
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
		secret := []byte(strings.TrimSpace(cfg.Media.Local.SignSecret))
		if len(secret) == 0 {
			if !config.DevModeEnabled() {
				return nil, fmt.Errorf("media: media.local.sign_secret required when media.storage=local")
			}
			return localfs.NewEphemeral(cfg.Media.Local.BasePath, cfg.Media.Local.BaseURL)
		}
		return localfs.NewWithSecret(cfg.Media.Local.BasePath, cfg.Media.Local.BaseURL, secret)
	default:
		return nil, fmt.Errorf("unsupported media.storage: %s", cfg.Media.Storage)
	}
}
