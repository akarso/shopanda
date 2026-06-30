package sqsqueue

import (
	"context"
	"fmt"
	"time"

	insqs "github.com/akarso/shopanda/internal/infrastructure/sqs"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

const initTimeout = 15 * time.Second

// QueuePlugin registers the SQS job queue backend.
type QueuePlugin struct {
	NewQueue func(context.Context, insqs.QueueConfig) (*insqs.JobQueue, error)
}

// NewQueuePlugin creates an SQS queue plugin.
func NewQueuePlugin() *QueuePlugin {
	return &QueuePlugin{NewQueue: insqs.NewJobQueue}
}

func (p *QueuePlugin) Name() string { return "core/sqs-queue" }

func (p *QueuePlugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("sqs queue: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("sqs queue: config not configured")
	}
	if app.Config.Queue.Driver != "sqs" {
		return fmt.Errorf("sqs queue: disabled (queue.driver=%q)", app.Config.Queue.Driver)
	}

	queueURL := ResolveQueueURL(app.Config.Queue.SQS)
	if queueURL == "" {
		return fmt.Errorf("sqs queue: empty queue_url (set queue.sqs.queue_url, SQS_QUEUE_URL, or SHOPANDA_QUEUE_SQS_QUEUE_URL)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()

	newQueue := p.NewQueue
	if newQueue == nil {
		newQueue = insqs.NewJobQueue
	}
	q, err := newQueue(ctx, insqs.QueueConfig{
		QueueURL:       queueURL,
		FailedQueueURL: ResolveFailedQueueURL(app.Config.Queue.SQS),
		Region:         ResolveRegion(app.Config.Queue.SQS),
	})
	if err != nil {
		return fmt.Errorf("sqs queue: init client: %w", err)
	}
	app.RegisterQueue(q)
	return nil
}
