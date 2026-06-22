package redisqueue

import (
	"fmt"
	"os"

	inredis "github.com/akarso/shopanda/internal/infrastructure/redis"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// QueuePlugin registers the Redis job queue backend.
type QueuePlugin struct{}

func NewQueuePlugin() *QueuePlugin { return &QueuePlugin{} }

func (p *QueuePlugin) Name() string { return "core/redis-queue" }

func (p *QueuePlugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("redis queue: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("redis queue: config not configured")
	}
	if app.Config.Queue.Driver != "redis" {
		return fmt.Errorf("redis queue: disabled (queue.driver=%q)", app.Config.Queue.Driver)
	}

	url := app.Config.Queue.Redis.URL
	if url == "" {
		url = os.Getenv("REDIS_URL")
	}
	if url == "" {
		return fmt.Errorf("redis queue: empty url (set queue.redis.url or REDIS_URL)")
	}

	q, err := inredis.NewJobQueue(inredis.QueueConfig{
		URL:       url,
		KeyPrefix: app.Config.Queue.Redis.KeyPrefix,
	})
	if err != nil {
		return fmt.Errorf("redis queue: init client: %w", err)
	}
	app.RegisterQueue(q)
	return nil
}
