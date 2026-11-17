package redis_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	inredis "github.com/akarso/shopanda/internal/infrastructure/redis"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func listContains(t *testing.T, mr *miniredis.Miniredis, key, id string) bool {
	t.Helper()
	items, err := mr.List(key)
	if err != nil {
		// Empty / deleted list key — not present.
		return false
	}
	for _, item := range items {
		if item == id {
			return true
		}
	}
	return false
}

func delayedContains(t *testing.T, mr *miniredis.Miniredis, id string) bool {
	t.Helper()
	members, err := mr.ZMembers("test-queue:delayed")
	if err != nil {
		// Missing delayed key means nothing is scheduled.
		return false
	}
	for _, m := range members {
		if m == id {
			return true
		}
	}
	return false
}

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
	if err := q.Complete(ctx, got.ID, got.Attempts); err != nil {
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

	if err := q.Fail(ctx, got.ID, got.Attempts, nil); err != nil {
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
	if err := q.Fail(ctx, got.ID, got.Attempts, nil); err != nil {
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

	err := q.Complete(ctx, id.New(), 1)
	if err == nil || !errors.Is(err, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("Complete missing: %v, want ErrOutcomeNotApplied", err)
	}
}

func TestJobQueue_Fail_NotFound(t *testing.T) {
	_, q := setupJobQueue(t)
	ctx := context.Background()

	err := q.Fail(ctx, id.New(), 1, errors.New("boom"))
	if err == nil || !errors.Is(err, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("Fail missing: %v, want ErrOutcomeNotApplied", err)
	}
}

// TestJobQueue_Fail_PreservesEmptyPayloadArray pins the Redis cjson trap:
// decode+encode turns JSON [] into {}. Fail must SET a Go-marshaled blob so
// search.reindex product_ids:[] survives retry (scopedProductIDs requires an array).
func TestJobQueue_Fail_PreservesEmptyPayloadArray(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	payload := map[string]interface{}{"scope": "products", "product_ids": []string{}}
	job, _ := jobs.NewJob(id.New(), "search.reindex", payload)
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	got, err := q.Dequeue(ctx)
	if err != nil || got == nil {
		t.Fatalf("Dequeue: %v %#v", err, got)
	}
	if err := q.Fail(ctx, got.ID, got.Attempts, errors.New("search down")); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	doc := loadStoredJob(t, mr, got.ID)
	storedPayload, _ := doc["payload"].(map[string]interface{})
	rawIDs, ok := storedPayload["product_ids"]
	if !ok {
		t.Fatalf("payload missing product_ids: %#v", doc["payload"])
	}
	switch rawIDs.(type) {
	case []interface{}:
		// ok — empty array survived Go marshal (Redis cjson.encode would write {})
	default:
		t.Fatalf("product_ids = %T (%v), want []interface{} (empty array, not object)", rawIDs, rawIDs)
	}
	// Round-trip through loadJob the way a later Dequeue would.
	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	raw, getErr := c.Get(ctx, "test-queue:job:"+got.ID).Bytes()
	if getErr != nil {
		t.Fatalf("GET job: %v", getErr)
	}
	var again jobs.Job
	// storedJob shape matches Job fields used by toJob; unmarshal payload only.
	var stored struct {
		Payload map[string]interface{} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	again.Payload = stored.Payload
	if _, ok := again.Payload["product_ids"].([]interface{}); !ok {
		t.Fatalf("product_ids = %T, want []interface{}", again.Payload["product_ids"])
	}
}

func TestJobQueue_Complete_WrongAttempt_LeavesProcessing(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "cas", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	if err := q.Complete(ctx, got.ID, got.Attempts-1); err == nil || !errors.Is(err, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("Complete wrong attempt: %v", err)
	}
	doc := loadStoredJob(t, mr, got.ID)
	if doc["status"] != string(jobs.StatusProcessing) {
		t.Fatalf("status = %v, want processing", doc["status"])
	}
	if !listContains(t, mr, "test-queue:processing", got.ID) {
		t.Fatal("id must remain on processing list after wrong-attempt Complete")
	}
}

func TestJobQueue_Fail_WrongAttempt_LeavesProcessing(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "cas", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	if err := q.Fail(ctx, got.ID, got.Attempts-1, errors.New("stale")); err == nil || !errors.Is(err, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("Fail wrong attempt: %v", err)
	}
	doc := loadStoredJob(t, mr, got.ID)
	if doc["status"] != string(jobs.StatusProcessing) {
		t.Fatalf("status = %v, want processing", doc["status"])
	}
	if !listContains(t, mr, "test-queue:processing", got.ID) {
		t.Fatal("id must remain on processing list after wrong-attempt Fail")
	}
}

func removeFromProcessing(t *testing.T, mr *miniredis.Miniredis, id string) {
	t.Helper()
	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	if err := c.LRem(context.Background(), "test-queue:processing", 1, id).Err(); err != nil {
		t.Fatalf("LRem processing: %v", err)
	}
}

func TestJobQueue_Fail_NotOnProcessingList_NoWrite(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "orphan", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	// Simulate list/hash divergence: id left processing without Complete/Fail.
	removeFromProcessing(t, mr, got.ID)
	if err := q.Fail(ctx, got.ID, got.Attempts, errors.New("boom")); err == nil || !errors.Is(err, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("Fail without processing claim: %v", err)
	}
	doc := loadStoredJob(t, mr, got.ID)
	if doc["status"] != string(jobs.StatusProcessing) {
		t.Fatalf("status = %v, want unchanged processing", doc["status"])
	}
	if delayedContains(t, mr, got.ID) {
		t.Fatal("must not ZADD a second schedule when not on processing list")
	}
}

func TestJobQueue_Complete_NotOnProcessingList_NoWrite(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "orphan", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	removeFromProcessing(t, mr, got.ID)
	if err := q.Complete(ctx, got.ID, got.Attempts); err == nil || !errors.Is(err, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("Complete without processing claim: %v", err)
	}
	doc := loadStoredJob(t, mr, got.ID)
	if doc["status"] != string(jobs.StatusProcessing) {
		t.Fatalf("status = %v, want unchanged processing", doc["status"])
	}
}

func TestJobQueue_Fail_Retry_LandsOnDelayed(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "retry", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	if err := q.Fail(ctx, got.ID, got.Attempts, errors.New("temp")); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	doc := loadStoredJob(t, mr, got.ID)
	if doc["status"] != string(jobs.StatusPending) {
		t.Fatalf("status = %v, want pending", doc["status"])
	}
	if listContains(t, mr, "test-queue:processing", got.ID) {
		t.Fatal("id must leave processing after Fail retry")
	}
	if !delayedContains(t, mr, got.ID) {
		t.Fatalf("job %q missing from delayed set", got.ID)
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
