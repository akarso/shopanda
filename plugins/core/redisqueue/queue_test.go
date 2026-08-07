package redisqueue_test

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	credisqueue "github.com/akarso/shopanda/plugins/core/redisqueue"
	"github.com/alicebob/miniredis/v2"
)

func testApp(cfg *config.Config) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	return &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
}

func TestQueuePlugin_Name(t *testing.T) {
	if got := credisqueue.NewQueuePlugin().Name(); got != "core/redis-queue" {
		t.Fatalf("Name() = %q, want core/redis-queue", got)
	}
}

func TestQueuePlugin_Init_RegistersRedisQueue(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()

	cfg := &config.Config{
		Queue: config.QueueConfig{
			Driver: "redis",
			Redis: config.RedisQueueConfig{
				URL:       "redis://" + mr.Addr(),
				KeyPrefix: "test",
			},
		},
	}
	app := testApp(cfg)
	if err := credisqueue.NewQueuePlugin().Init(app); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	v, ok := app.Queue()
	if !ok {
		t.Fatal("Queue() ok = false, want redis queue")
	}
	if _, ok := v.(jobs.Queue); !ok {
		t.Fatalf("Queue() type = %T, want jobs.Queue", v)
	}
}

func TestQueuePlugin_Init_WrongQueueDriver(t *testing.T) {
	cfg := &config.Config{Queue: config.QueueConfig{Driver: "postgres"}}
	if err := credisqueue.NewQueuePlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when queue.driver is postgres")
	}
}

func TestQueuePlugin_Init_MissingRedisURL(t *testing.T) {
	cfg := &config.Config{Queue: config.QueueConfig{Driver: "redis"}}
	if err := credisqueue.NewQueuePlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when redis url is empty")
	}
}

func TestQueuePlugin_Init_UsesRedisURLEnvFallback(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()

	t.Setenv("REDIS_URL", "redis://"+mr.Addr())

	cfg := &config.Config{
		Queue: config.QueueConfig{Driver: "redis"},
	}
	app := testApp(cfg)
	if err := credisqueue.NewQueuePlugin().Init(app); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	v, ok := app.Queue()
	if !ok {
		t.Fatal("Queue() ok = false, want redis queue")
	}
	if _, ok := v.(jobs.Queue); !ok {
		t.Fatalf("Queue() type = %T, want jobs.Queue", v)
	}
}
