package sqsqueue_test

import (
	"context"
	"io"
	"testing"

	insqs "github.com/akarso/shopanda/internal/infrastructure/sqs"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	csqs "github.com/akarso/shopanda/plugins/core/sqsqueue"
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
	if got := csqs.NewQueuePlugin().Name(); got != "core/sqs-queue" {
		t.Fatalf("Name() = %q, want core/sqs-queue", got)
	}
}

func TestQueuePlugin_Init_WrongQueueDriver(t *testing.T) {
	cfg := &config.Config{Queue: config.QueueConfig{Driver: "postgres"}}
	if err := csqs.NewQueuePlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when queue.driver is postgres")
	}
}

func TestQueuePlugin_Init_MissingQueueURL(t *testing.T) {
	cfg := &config.Config{Queue: config.QueueConfig{Driver: "sqs"}}
	if err := csqs.NewQueuePlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when sqs queue_url is empty")
	}
}

func TestQueuePlugin_Init_NilApp(t *testing.T) {
	if err := csqs.NewQueuePlugin().Init(nil); err == nil {
		t.Fatal("Init() expected error when app is nil")
	}
}

func TestQueuePlugin_Init_UsesSQSQueueURLEnvFallback(t *testing.T) {
	t.Setenv("SQS_QUEUE_URL", "https://sqs.us-east-1.amazonaws.com/123456789012/test-jobs")

	cfg := &config.Config{Queue: config.QueueConfig{Driver: "sqs"}}
	app := testApp(cfg)
	plugin := csqs.NewQueuePlugin()
	plugin.NewQueue = func(_ context.Context, cfg insqs.QueueConfig) (*insqs.JobQueue, error) {
		if cfg.QueueURL != "https://sqs.us-east-1.amazonaws.com/123456789012/test-jobs" {
			t.Fatalf("queue url = %q", cfg.QueueURL)
		}
		return &insqs.JobQueue{}, nil
	}
	if err := plugin.Init(app); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if _, ok := app.Queue(); !ok {
		t.Fatal("Queue() ok = false, want sqs queue")
	}
}
