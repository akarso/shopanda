package storagelocal_test

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	cstoragelocal "github.com/akarso/shopanda/plugins/core/storagelocal"
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
	if got := cstoragelocal.NewStoragePlugin().Name(); got != "core/storage-local" {
		t.Fatalf("Name() = %q, want core/storage-local", got)
	}
}

func TestStoragePlugin_Init_RegistersLocalStorage(t *testing.T) {
	cfg := &config.Config{
		Media: config.MediaConfig{
			Storage: "local",
			Local: config.LocalStorageConfig{
				BasePath: "./public/media",
				BaseURL:  "/media",
			},
		},
	}
	app := testApp(cfg)
	if err := cstoragelocal.NewStoragePlugin().Init(app); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	v, ok := app.MediaStorage()
	if !ok {
		t.Fatal("MediaStorage() ok = false, want local storage")
	}
	if _, ok := v.(media.Storage); !ok {
		t.Fatalf("MediaStorage() type = %T, want media.Storage", v)
	}
}

func TestStoragePlugin_Init_WrongStorageDriver(t *testing.T) {
	cfg := &config.Config{Media: config.MediaConfig{Storage: "s3"}}
	if err := cstoragelocal.NewStoragePlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when media.storage is s3")
	}
}

func TestStoragePlugin_Init_MissingLocalConfig(t *testing.T) {
	cfg := &config.Config{
		Media: config.MediaConfig{Storage: "local"},
	}
	if err := cstoragelocal.NewStoragePlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when base_path and base_url are empty")
	}
}
