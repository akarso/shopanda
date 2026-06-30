package kafkaqueue_test

import (
	"io"
	"testing"

	inkafka "github.com/akarso/shopanda/internal/infrastructure/kafka"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	ckafka "github.com/akarso/shopanda/plugins/core/kafkaqueue"
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
	if got := ckafka.NewQueuePlugin().Name(); got != "core/kafka-queue" {
		t.Fatalf("Name() = %q, want core/kafka-queue", got)
	}
}

func TestQueuePlugin_Init_WrongQueueDriver(t *testing.T) {
	cfg := &config.Config{Queue: config.QueueConfig{Driver: "postgres"}}
	if err := ckafka.NewQueuePlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when queue.driver is postgres")
	}
}

func TestQueuePlugin_Init_MissingBrokers(t *testing.T) {
	cfg := &config.Config{Queue: config.QueueConfig{Driver: "kafka"}}
	if err := ckafka.NewQueuePlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when kafka brokers are empty")
	}
}

func TestQueuePlugin_Init_NilApp(t *testing.T) {
	if err := ckafka.NewQueuePlugin().Init(nil); err == nil {
		t.Fatal("Init() expected error when app is nil")
	}
}

func TestQueuePlugin_Init_UsesKafkaBrokersEnvFallback(t *testing.T) {
	t.Setenv("KAFKA_BROKERS", "127.0.0.1:9092")

	cfg := &config.Config{Queue: config.QueueConfig{Driver: "kafka"}}
	app := testApp(cfg)
	plugin := ckafka.NewQueuePlugin()
	plugin.NewQueue = func(cfg inkafka.QueueConfig) (*inkafka.JobQueue, error) {
		if len(cfg.Brokers) != 1 || cfg.Brokers[0] != "127.0.0.1:9092" {
			t.Fatalf("brokers = %v, want [127.0.0.1:9092]", cfg.Brokers)
		}
		return &inkafka.JobQueue{}, nil
	}
	if err := plugin.Init(app); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if _, ok := app.Queue(); !ok {
		t.Fatal("Queue() ok = false, want kafka queue")
	}
}
