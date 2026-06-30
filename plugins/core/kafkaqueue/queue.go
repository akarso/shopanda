package kafkaqueue

import (
	"fmt"
	"os"
	"strings"

	inkafka "github.com/akarso/shopanda/internal/infrastructure/kafka"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// QueuePlugin registers the Kafka job queue backend.
type QueuePlugin struct{}

func NewQueuePlugin() *QueuePlugin { return &QueuePlugin{} }

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

	brokers := append([]string(nil), app.Config.Queue.Kafka.Brokers...)
	if len(brokers) == 0 {
		if env := strings.TrimSpace(os.Getenv("KAFKA_BROKERS")); env != "" {
			for _, part := range strings.Split(env, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					brokers = append(brokers, part)
				}
			}
		}
	}
	if len(brokers) == 0 {
		return fmt.Errorf("kafka queue: empty brokers (set queue.kafka.brokers or KAFKA_BROKERS)")
	}

	q, err := inkafka.NewJobQueue(inkafka.QueueConfig{
		Brokers:     brokers,
		TopicPrefix: app.Config.Queue.Kafka.TopicPrefix,
	})
	if err != nil {
		return fmt.Errorf("kafka queue: init client: %w", err)
	}
	app.RegisterQueue(q)
	return nil
}
