package rabbitmqqueue_test

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	crabbitmq "github.com/akarso/shopanda/plugins/core/rabbitmqqueue"
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
	if got := crabbitmq.NewQueuePlugin().Name(); got != "core/rabbitmq-queue" {
		t.Fatalf("Name() = %q, want core/rabbitmq-queue", got)
	}
}

func TestQueuePlugin_Init_WrongQueueDriver(t *testing.T) {
	cfg := &config.Config{Queue: config.QueueConfig{Driver: "postgres"}}
	if err := crabbitmq.NewQueuePlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when queue.driver is postgres")
	}
}

func TestQueuePlugin_Init_MissingRabbitMQURL(t *testing.T) {
	cfg := &config.Config{Queue: config.QueueConfig{Driver: "rabbitmq"}}
	if err := crabbitmq.NewQueuePlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when rabbitmq url is empty")
	}
}

func TestQueuePlugin_Init_NilApp(t *testing.T) {
	if err := crabbitmq.NewQueuePlugin().Init(nil); err == nil {
		t.Fatal("Init() expected error when app is nil")
	}
}

func TestQueuePlugin_Init_UsesRabbitMQURLEnvFallback(t *testing.T) {
	t.Setenv("RABBITMQ_URL", "amqp://guest:guest@127.0.0.1:5672/")

	cfg := &config.Config{
		Queue: config.QueueConfig{Driver: "rabbitmq"},
	}
	app := testApp(cfg)
	if err := crabbitmq.NewQueuePlugin().Init(app); err != nil {
		t.Skipf("RabbitMQ broker not available: %v", err)
	}
	v, ok := app.Queue()
	if !ok {
		t.Fatal("Queue() ok = false, want rabbitmq queue")
	}
	if _, ok := v.(jobs.Queue); !ok {
		t.Fatalf("Queue() type = %T, want jobs.Queue", v)
	}
}
