package kafkaqueue

import (
	"fmt"

	inkafka "github.com/akarso/shopanda/internal/infrastructure/kafka"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// QueuePlugin registers the Kafka job queue backend.
type QueuePlugin struct {
	NewQueue func(inkafka.QueueConfig) (*inkafka.JobQueue, error)
}

// NewQueuePlugin creates a Kafka queue plugin.
func NewQueuePlugin() *QueuePlugin {
	return &QueuePlugin{NewQueue: inkafka.NewJobQueue}
}

func (p *QueuePlugin) Name() string { return "core/kafka-queue" }

func (p *QueuePlugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("kafka queue: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("kafka queue: config not configured")
	}
	if app.Config.Queue.Driver != "kafka" {
		return fmt.Errorf("kafka queue: disabled (queue.driver=%q)", app.Config.Queue.Driver)
	}

	brokers := ResolveBrokers(app.Config.Queue.Kafka)
	if len(brokers) == 0 {
		return fmt.Errorf("kafka queue: empty brokers (set queue.kafka.brokers, KAFKA_BROKERS, or SHOPANDA_QUEUE_KAFKA_BROKERS)")
	}

	newQueue := p.NewQueue
	if newQueue == nil {
		newQueue = inkafka.NewJobQueue
	}
	q, err := newQueue(inkafka.QueueConfig{
		Brokers:     brokers,
		TopicPrefix: app.Config.Queue.Kafka.TopicPrefix,
	})
	if err != nil {
		return fmt.Errorf("kafka queue: init client: %w", err)
	}
	app.RegisterQueue(q)
	return nil
}
