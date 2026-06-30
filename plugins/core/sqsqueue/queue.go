package sqsqueue

import (
	"context"
	"fmt"
	"os"

	insqs "github.com/akarso/shopanda/internal/infrastructure/sqs"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// QueuePlugin registers the SQS job queue backend.
type QueuePlugin struct{}

func NewQueuePlugin() *QueuePlugin { return &QueuePlugin{} }

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

	queueURL := app.Config.Queue.SQS.QueueURL
	if queueURL == "" {
		queueURL = os.Getenv("SQS_QUEUE_URL")
	}
	if queueURL == "" {
		return fmt.Errorf("sqs queue: empty queue_url (set queue.sqs.queue_url or SQS_QUEUE_URL)")
	}

	failedURL := app.Config.Queue.SQS.FailedQueueURL
	if failedURL == "" {
		failedURL = os.Getenv("SQS_FAILED_QUEUE_URL")
	}

	region := app.Config.Queue.SQS.Region
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		region = os.Getenv("AWS_DEFAULT_REGION")
	}

	q, err := insqs.NewJobQueue(context.Background(), insqs.QueueConfig{
		QueueURL:       queueURL,
		FailedQueueURL: failedURL,
		Region:         region,
	})
	if err != nil {
		return fmt.Errorf("sqs queue: init client: %w", err)
	}
	app.RegisterQueue(q)
	return nil
}
