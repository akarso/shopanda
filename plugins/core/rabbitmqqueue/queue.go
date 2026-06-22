package rabbitmqqueue

import (
	"fmt"
	"os"

	inrabbitmq "github.com/akarso/shopanda/internal/infrastructure/rabbitmq"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// QueuePlugin registers the RabbitMQ job queue backend.
type QueuePlugin struct{}

func NewQueuePlugin() *QueuePlugin { return &QueuePlugin{} }

func (p *QueuePlugin) Name() string { return "core/rabbitmq-queue" }

func (p *QueuePlugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("rabbitmq queue: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("rabbitmq queue: config not configured")
	}
	if app.Config.Queue.Driver != "rabbitmq" {
		return fmt.Errorf("rabbitmq queue: disabled (queue.driver=%q)", app.Config.Queue.Driver)
	}

	url := app.Config.Queue.RabbitMQ.URL
	if url == "" {
		url = os.Getenv("RABBITMQ_URL")
	}
	if url == "" {
		return fmt.Errorf("rabbitmq queue: empty url (set queue.rabbitmq.url or RABBITMQ_URL)")
	}

	q, err := inrabbitmq.NewJobQueue(inrabbitmq.QueueConfig{
		URL:         url,
		QueuePrefix: app.Config.Queue.RabbitMQ.QueuePrefix,
	})
	if err != nil {
		return fmt.Errorf("rabbitmq queue: init client: %w", err)
	}
	app.RegisterQueue(q)
	return nil
}
