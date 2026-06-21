package storages3

import (
	"fmt"

	"github.com/akarso/shopanda/internal/infrastructure/s3store"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// StoragePlugin registers the S3-compatible media storage backend.
type StoragePlugin struct{}

func NewStoragePlugin() *StoragePlugin { return &StoragePlugin{} }

func (p *StoragePlugin) Name() string { return "core/storage-s3" }

func (p *StoragePlugin) Init(app *plugin.App) error {
	if app.Config == nil {
		return fmt.Errorf("s3 storage: config not configured")
	}
	if app.Config.Media.Storage != "s3" {
		return fmt.Errorf("s3 storage: disabled (media.storage=%q)", app.Config.Media.Storage)
	}

	cfg := app.Config.Media.S3
	if cfg.Bucket == "" {
		return fmt.Errorf("s3 storage: empty bucket")
	}
	if cfg.Region == "" {
		return fmt.Errorf("s3 storage: empty region")
	}

	s3s, err := s3store.New(s3store.Config{
		Endpoint:  cfg.Endpoint,
		Bucket:    cfg.Bucket,
		Region:    cfg.Region,
		AccessKey: cfg.AccessKey,
		SecretKey: cfg.SecretKey,
		BaseURL:   cfg.BaseURL,
		PublicACL: cfg.PublicACL,
	})
	if err != nil {
		return fmt.Errorf("s3 storage: init client: %w", err)
	}
	app.RegisterMediaStorage(s3s)
	return nil
}
