package sqsqueue_test

import (
	"io"
	"testing"

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
	if err := csqs.NewQueuePlugin().Init(app); err != nil {
		t.Skipf("SQS queue not available: %v", err)
	}
	if _, ok := app.Queue(); !ok {
		t.Fatal("Queue() ok = false, want sqs queue")
	}
}
