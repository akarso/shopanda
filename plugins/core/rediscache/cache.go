package rediscache

import (
	"fmt"
	"os"

	inredis "github.com/akarso/shopanda/internal/infrastructure/redis"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// CachePlugin registers the Redis cache backend.
type CachePlugin struct{}

func NewCachePlugin() *CachePlugin { return &CachePlugin{} }

func (p *CachePlugin) Name() string { return "core/redis-cache" }

func (p *CachePlugin) Init(app *plugin.App) error {
	if app.Config == nil {
		return fmt.Errorf("redis cache: config not configured")
	}
	if app.Config.Cache.Driver != "redis" {
		return fmt.Errorf("redis cache: disabled (cache.driver=%q)", app.Config.Cache.Driver)
	}

	url := app.Config.Cache.Redis.URL
	if url == "" {
		url = os.Getenv("REDIS_URL")
	}
	if url == "" {
		return fmt.Errorf("redis cache: empty url (set cache.redis.url or REDIS_URL)")
	}

	cs, err := inredis.New(inredis.Config{
		URL:       url,
		KeyPrefix: app.Config.Cache.Redis.KeyPrefix,
	})
	if err != nil {
		return fmt.Errorf("redis cache: init client: %w", err)
	}
	app.RegisterCache(cs)
	return nil
}
