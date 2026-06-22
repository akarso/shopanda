package redis_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/akarso/shopanda/internal/domain/jobs"
	inredis "github.com/akarso/shopanda/internal/infrastructure/redis"
	"github.com/akarso/shopanda/internal/platform/id"
)

func setupJobQueue(t *testing.T) (*miniredis.Miniredis, jobs.Queue) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	q, err := inredis.NewJobQueue(inredis.QueueConfig{
		URL:       "redis://" + mr.Addr(),
		KeyPrefix: "test-queue",
	})
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	return mr, q
}

func loadStoredJob(t *testing.T, mr *miniredis.Miniredis, jobID string) map[string]interface{} {
	t.Helper()
	raw, err := mr.Get("test-queue:job:" + jobID)
	if err != nil {
		t.Fatalf("get job %q: %v", jobID, err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("unmarshal job: %v", err)
	}
	return doc
}

func TestJobQueue_Enqueue(t *testing.T) {
	_, q := setupJobQueue(t)
	ctx := context.Background()

	job, err := jobs.NewJob(id.New(), "send_email", map[string]interface{}{"to": "a@b.com"})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
}

func TestJobQueue_Dequeue(t *testing.T) {
	_, q := setupJobQueue(t)
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
	if got == nil {
		t.Fatal("Dequeue returned nil, expected a job")
	}
	if got.ID != job.ID {
		t.Errorf("ID = %q, want %q", got.ID, job.ID)
	}
	if got.Status != jobs.StatusProcessing {
		t.Errorf("Status = %q, want %q", got.Status, jobs.StatusProcessing)
	}
	if got.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", got.Attempts)
	}
}

func TestJobQueue_Dequeue_Empty(t *testing.T) {
	_, q := setupJobQueue(t)
	ctx := context.Background()

	got, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil job, got %+v", got)
	}
}

func TestJobQueue_Complete(t *testing.T) {
	_, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "test", nil)
	_ = q.Enqueue(ctx, job)

	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected a dequeued job")
	}
	if err := q.Complete(ctx, got.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	next, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue after complete: %v", err)
	}
	if next != nil {
		t.Fatal("expected nil after complete, got a job")
	}
}

func TestJobQueue_Fail_Retry(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "test", nil)
	_ = q.Enqueue(ctx, job)

	got, _ := q.Dequeue(ctx)
	now := time.Now()

	if err := q.Fail(ctx, got.ID, nil); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	doc := loadStoredJob(t, mr, got.ID)
	if doc["status"] != string(jobs.StatusPending) {
		t.Errorf("status = %v, want pending", doc["status"])
	}
	runAt, err := time.Parse(time.RFC3339Nano, doc["run_at"].(string))
	if err != nil {
		t.Fatalf("parse run_at: %v", err)
	}
	minDelay := 3750 * time.Millisecond
	maxDelay := 8 * time.Second
	if !runAt.After(now.Add(minDelay)) {
		t.Errorf("run_at %v should be after now+minDelay %v", runAt, now.Add(minDelay))
	}
	if !runAt.Before(now.Add(maxDelay)) {
		t.Errorf("run_at %v should be before now+maxDelay %v", runAt, now.Add(maxDelay))
	}
}

func TestJobQueue_Fail_Permanent(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "test", nil)
	job.MaxRetries = 1
	_ = q.Enqueue(ctx, job)

	got, _ := q.Dequeue(ctx)
	if err := q.Fail(ctx, got.ID, nil); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	doc := loadStoredJob(t, mr, got.ID)
	if doc["status"] != string(jobs.StatusFailed) {
		t.Errorf("status = %v, want failed", doc["status"])
	}
}

func TestJobQueue_Complete_NotFound(t *testing.T) {
	_, q := setupJobQueue(t)
	ctx := context.Background()

	if err := q.Complete(ctx, id.New()); err == nil {
		t.Fatal("expected error for non-existent job")
	}
}

func TestJobQueue_Dequeue_RespectsRunAt(t *testing.T) {
	_, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "delayed", nil)
	job.RunAt = time.Now().UTC().Add(100 * time.Millisecond)
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	got, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil before run_at, got %+v", got)
	}

	time.Sleep(150 * time.Millisecond)

	got, err = q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue after delay: %v", err)
	}
	if got == nil || got.ID != job.ID {
		t.Fatalf("Dequeue after delay = %+v, want job %q", got, job.ID)
	}
}

func TestNewJobQueue_EmptyURL(t *testing.T) {
	if _, err := inredis.NewJobQueue(inredis.QueueConfig{}); err == nil {
		t.Fatal("NewJobQueue() expected error for empty url")
	}
}
