package storages3_test

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	cstorages3 "github.com/akarso/shopanda/plugins/core/storages3"
)

func testApp(cfg *config.Config) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	return &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
}

func TestStoragePlugin_Name(t *testing.T) {
	if got := cstorages3.NewStoragePlugin().Name(); got != "core/storage-s3" {
		t.Fatalf("Name() = %q, want core/storage-s3", got)
	}
}

func TestStoragePlugin_Init_MissingBucket(t *testing.T) {
	cfg := &config.Config{
		Media: config.MediaConfig{
			Storage: "s3",
			S3: config.S3StorageConfig{
				Region: "us-east-1",
			},
		},
	}
	if err := cstorages3.NewStoragePlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error for empty bucket")
	}
}

func TestStoragePlugin_Init_MissingRegion(t *testing.T) {
	cfg := &config.Config{
		Media: config.MediaConfig{
			Storage: "s3",
			S3: config.S3StorageConfig{
				Bucket: "media",
			},
		},
	}
	if err := cstorages3.NewStoragePlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error for empty region")
	}
}

func TestStoragePlugin_Init_RegistersS3Storage(t *testing.T) {
	cfg := &config.Config{
		Media: config.MediaConfig{
			Storage: "s3",
			S3: config.S3StorageConfig{
				Endpoint: "http://localhost:9000",
				Bucket:   "media",
				Region:   "us-east-1",
			},
		},
	}
	app := testApp(cfg)
	if err := cstorages3.NewStoragePlugin().Init(app); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	v, ok := app.MediaStorage()
	if !ok {
		t.Fatal("MediaStorage() ok = false, want s3 storage")
	}
	if _, ok := v.(media.Storage); !ok {
		t.Fatalf("MediaStorage() type = %T, want media.Storage", v)
	}
}

func TestStoragePlugin_Init_WrongStorageDriver(t *testing.T) {
	cfg := &config.Config{Media: config.MediaConfig{Storage: "local"}}
	if err := cstorages3.NewStoragePlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when media.storage is local")
	}
}
