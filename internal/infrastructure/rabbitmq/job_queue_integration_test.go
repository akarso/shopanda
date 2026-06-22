//go:build integration

package rabbitmq_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	inrabbitmq "github.com/akarso/shopanda/internal/infrastructure/rabbitmq"
	"github.com/akarso/shopanda/internal/platform/id"
)

func setupRabbitQueue(t *testing.T) jobs.Queue {
	t.Helper()
	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		url = "amqp://guest:guest@127.0.0.1:5672/"
	}
	prefix := "shopanda-test-" + strings.ReplaceAll(t.Name(), "/", "_")
	q, err := inrabbitmq.NewJobQueue(inrabbitmq.QueueConfig{
		URL:         url,
		QueuePrefix: prefix,
	})
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	return q
}

func TestJobQueue_EnqueueDequeueComplete(t *testing.T) {
	q := setupRabbitQueue(t)
	ctx := context.Background()

	job, err := jobs.NewJob(id.New(), "send_email", map[string]interface{}{"to": "a@b.com"})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	got, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if got == nil || got.ID != job.ID {
		t.Fatalf("Dequeue = %+v, want job %q", got, job.ID)
	}
	if got.Status != jobs.StatusProcessing || got.Attempts != 1 {
		t.Fatalf("Dequeue status=%q attempts=%d", got.Status, got.Attempts)
	}

	if err := q.Complete(ctx, got.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	next, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue after complete: %v", err)
	}
	if next != nil {
		t.Fatal("expected empty queue after complete")
	}
}

func TestJobQueue_Fail_Permanent(t *testing.T) {
	q := setupRabbitQueue(t)
	ctx := context.Background()

	job, err := jobs.NewJob(id.New(), "test", nil)
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	job.MaxRetries = 1
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	got, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if got == nil {
		t.Fatal("Dequeue returned nil job")
	}
	if err := q.Fail(ctx, got.ID, nil); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	next, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue after fail: %v", err)
	}
	if next != nil {
		t.Fatal("permanently failed job should not return to main queue")
	}
}

func TestJobQueue_Fail_Retry(t *testing.T) {
	q := setupRabbitQueue(t)
	ctx := context.Background()

	job, err := jobs.NewJob(id.New(), "test", nil)
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	got, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if got == nil {
		t.Fatal("Dequeue returned nil job")
	}
	if err := q.Fail(ctx, got.ID, nil); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		retry, err := q.Dequeue(ctx)
		if err != nil {
			t.Fatalf("Dequeue retry: %v", err)
		}
		if retry != nil && retry.ID == job.ID {
			if retry.Attempts != 2 {
				t.Fatalf("retry attempts = %d, want 2", retry.Attempts)
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("timed out waiting for retry job")
}
