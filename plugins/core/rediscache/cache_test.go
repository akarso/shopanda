package rediscache_test

import (
	"io"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	crediscache "github.com/akarso/shopanda/plugins/core/rediscache"
)

func testApp(cfg *config.Config) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	return &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
}

func TestCachePlugin_Name(t *testing.T) {
	if got := crediscache.NewCachePlugin().Name(); got != "core/redis-cache" {
		t.Fatalf("Name() = %q, want core/redis-cache", got)
	}
}

func TestCachePlugin_Init_RegistersRedisCache(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()

	cfg := &config.Config{
		Cache: config.CacheConfig{
			Driver: "redis",
			Redis: config.RedisCacheConfig{
				URL:       "redis://" + mr.Addr(),
				KeyPrefix: "test",
			},
		},
	}
	app := testApp(cfg)
	if err := crediscache.NewCachePlugin().Init(app); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	v, ok := app.Cache()
	if !ok {
		t.Fatal("Cache() ok = false, want redis cache")
	}
	if _, ok := v.(cache.Cache); !ok {
		t.Fatalf("Cache() type = %T, want cache.Cache", v)
	}
}

func TestCachePlugin_Init_WrongCacheDriver(t *testing.T) {
	cfg := &config.Config{Cache: config.CacheConfig{Driver: "postgres"}}
	if err := crediscache.NewCachePlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when cache.driver is postgres")
	}
}

func TestCachePlugin_Init_MissingRedisURL(t *testing.T) {
	cfg := &config.Config{Cache: config.CacheConfig{Driver: "redis"}}
	if err := crediscache.NewCachePlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when redis url is empty")
	}
}
