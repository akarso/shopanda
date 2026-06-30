package sqsqueue

import (
	"os"

	"github.com/akarso/shopanda/internal/platform/config"
)

func ResolveQueueURL(cfg config.SQSQueueConfig) string {
	if cfg.QueueURL != "" {
		return cfg.QueueURL
	}
	if v := os.Getenv("SHOPANDA_QUEUE_SQS_QUEUE_URL"); v != "" {
		return v
	}
	return os.Getenv("SQS_QUEUE_URL")
}

func ResolveFailedQueueURL(cfg config.SQSQueueConfig) string {
	if cfg.FailedQueueURL != "" {
		return cfg.FailedQueueURL
	}
	if v := os.Getenv("SHOPANDA_QUEUE_SQS_FAILED_QUEUE_URL"); v != "" {
		return v
	}
	return os.Getenv("SQS_FAILED_QUEUE_URL")
}

func ResolveRegion(cfg config.SQSQueueConfig) string {
	if cfg.Region != "" {
		return cfg.Region
	}
	if v := os.Getenv("SHOPANDA_QUEUE_SQS_REGION"); v != "" {
		return v
	}
	if v := os.Getenv("AWS_REGION"); v != "" {
		return v
	}
	return os.Getenv("AWS_DEFAULT_REGION")
}
