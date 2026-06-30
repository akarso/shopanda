package sqsqueue_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/plugins/core/sqsqueue"
)

func TestResolveQueueURL_EnvFallback(t *testing.T) {
	t.Setenv("SHOPANDA_QUEUE_SQS_QUEUE_URL", "")
	t.Setenv("SQS_QUEUE_URL", "https://sqs.us-east-1.amazonaws.com/123/test")

	got := sqsqueue.ResolveQueueURL(config.SQSQueueConfig{})
	if got != "https://sqs.us-east-1.amazonaws.com/123/test" {
		t.Fatalf("ResolveQueueURL() = %q", got)
	}
}

func TestResolveQueueURL_NamespacedEnvWins(t *testing.T) {
	t.Setenv("SHOPANDA_QUEUE_SQS_QUEUE_URL", "https://sqs.eu-west-1.amazonaws.com/123/namespaced")
	t.Setenv("SQS_QUEUE_URL", "https://sqs.us-east-1.amazonaws.com/123/test")

	got := sqsqueue.ResolveQueueURL(config.SQSQueueConfig{})
	if got != "https://sqs.eu-west-1.amazonaws.com/123/namespaced" {
		t.Fatalf("ResolveQueueURL() = %q", got)
	}
}

func TestResolveQueueURL_ConfigFirst(t *testing.T) {
	t.Setenv("SQS_QUEUE_URL", "https://sqs.us-east-1.amazonaws.com/123/env")

	got := sqsqueue.ResolveQueueURL(config.SQSQueueConfig{
		QueueURL: "https://sqs.us-east-1.amazonaws.com/123/config",
	})
	if got != "https://sqs.us-east-1.amazonaws.com/123/config" {
		t.Fatalf("ResolveQueueURL() = %q", got)
	}
}
