package redis_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func setupJobQueue(t *testing.T) (*miniredis.Miniredis, *inredis.JobQueue) {
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
	ctx := context.Background()
	if err := c.LRem(ctx, "test-queue:processing", 1, id).Err(); err != nil {
		t.Fatalf("LRem processing: %v", err)
	}
	if err := c.SRem(ctx, "test-queue:processing_set", id).Err(); err != nil {
		t.Fatalf("SRem processing_set: %v", err)
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
	if err := q.Fail(ctx, got.ID, got.Attempts, errors.New("boom")); err == nil || !errors.Is(err, inredis.ErrProcessingMembership) {
		t.Fatalf("Fail without processing claim: %v, want ErrProcessingMembership", err)
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
	if err := q.Complete(ctx, got.ID, got.Attempts); err == nil || !errors.Is(err, inredis.ErrProcessingMembership) {
		t.Fatalf("Complete without processing claim: %v, want ErrProcessingMembership", err)
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

func ageProcessingClaim(t *testing.T, mr *miniredis.Miniredis, id string, age time.Duration) {
	t.Helper()
	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()
	raw, err := c.Get(ctx, "test-queue:job:"+id).Bytes()
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	updated := time.Now().UTC().Add(-age)
	doc["updated_at"] = updated.Format(time.RFC3339Nano)
	blob, _ := json.Marshal(doc)
	if err := c.Set(ctx, "test-queue:job:"+id, blob, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := c.HSet(ctx, "test-queue:jobmeta:"+id, "updated_at_ms", updated.UnixMilli()).Err(); err != nil {
		t.Fatal(err)
	}
}

func TestJobQueue_Requeue_CompleteWinsKeepsDoneOffReady(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "race", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	ageProcessingClaim(t, mr, got.ID, 10*time.Minute)

	if err := q.Complete(ctx, got.ID, got.Attempts); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	err := q.Requeue(ctx, got.ID, got.Attempts, jobs.WithRequeueMinAge(time.Minute))
	if err == nil || !errors.Is(err, jobs.ErrRequeueNotProcessing) {
		t.Fatalf("Requeue after Complete: %v, want ErrRequeueNotProcessing", err)
	}
	doc := loadStoredJob(t, mr, got.ID)
	if doc["status"] != string(jobs.StatusDone) {
		t.Fatalf("status = %v, want done", doc["status"])
	}
	if listContains(t, mr, "test-queue:ready", got.ID) {
		t.Fatal("id must not be on ready after Complete wins")
	}
}

func TestJobQueue_RequeueTerminal_Unsupported(t *testing.T) {
	_, q := setupJobQueue(t)
	err := q.Requeue(context.Background(), "any", 1, jobs.WithRequeueTerminal())
	if err == nil || !errors.Is(err, jobs.ErrRequeueUnsupported) {
		t.Fatalf("err = %v, want ErrRequeueUnsupported", err)
	}
}

func TestJobQueue_Requeue_HookErrorDoesNotPushReady(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "hook", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	ageProcessingClaim(t, mr, got.ID, 10*time.Minute)
	metaBefore := metaUpdatedAtMs(t, mr, got.ID)

	want := errors.New("fence boom")
	err := q.RequeueWithBeforeCommit(ctx, got.ID, got.Attempts, func(context.Context, string, int) error {
		return want
	}, jobs.WithRequeueMinAge(time.Minute))
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if listContains(t, mr, "test-queue:ready", got.ID) {
		t.Fatal("ready must stay empty when hook fails")
	}
	doc := loadStoredJob(t, mr, got.ID)
	if doc["status"] != string(jobs.StatusProcessing) {
		t.Fatalf("status = %v, want processing after abort", doc["status"])
	}
	if got := metaUpdatedAtMs(t, mr, got.ID); got != metaBefore {
		t.Fatalf("updated_at_ms = %d, want preserved %d so retry is not TooFresh", got, metaBefore)
	}
	// Immediate retry must succeed (age preserved).
	if err := q.Requeue(ctx, got.ID, got.Attempts, jobs.WithRequeueMinAge(time.Minute)); err != nil {
		t.Fatalf("requeue after aborted hook: %v", err)
	}
}

func metaUpdatedAtMs(t *testing.T, mr *miniredis.Miniredis, id string) int64 {
	t.Helper()
	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	n, err := c.HGet(context.Background(), "test-queue:jobmeta:"+id, "updated_at_ms").Int64()
	if err != nil {
		t.Fatalf("HGet updated_at_ms: %v", err)
	}
	return n
}

func TestJobQueue_Requeue_CanceledCtxStillAborts(t *testing.T) {
	mr, q := setupJobQueue(t)

	job, _ := jobs.NewJob(id.New(), "cancel", nil)
	_ = q.Enqueue(context.Background(), job)
	got, _ := q.Dequeue(context.Background())
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	ageProcessingClaim(t, mr, got.ID, 10*time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	err := q.RequeueWithBeforeCommit(ctx, got.ID, got.Attempts, func(context.Context, string, int) error {
		cancel() // cancel caller ctx while reclaiming
		return errors.New("hook fail")
	}, jobs.WithRequeueMinAge(time.Minute))
	if err == nil {
		t.Fatal("expected hook error")
	}
	doc := loadStoredJob(t, mr, got.ID)
	if doc["status"] != string(jobs.StatusProcessing) {
		t.Fatalf("status = %v, want processing (durable abort despite cancel)", doc["status"])
	}
	if listContains(t, mr, "test-queue:ready", got.ID) {
		t.Fatal("must not be on ready after aborted reclaim")
	}
}

func TestJobQueue_Requeue_RecoversStaleReclaiming(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "stale-reclaim", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	ageProcessingClaim(t, mr, got.ID, 10*time.Minute)

	// Simulate crash after begin: leave reclaiming off both lists.
	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	raw, _ := c.Get(ctx, "test-queue:job:"+got.ID).Bytes()
	var doc map[string]interface{}
	_ = json.Unmarshal(raw, &doc)
	doc["status"] = string(jobs.StatusReclaiming)
	blob, _ := json.Marshal(doc)
	_ = c.Set(ctx, "test-queue:job:"+got.ID, blob, 0).Err()
	_ = c.HSet(ctx, "test-queue:jobmeta:"+got.ID,
		"status", "reclaiming",
		"attempts", got.Attempts,
		"reclaiming_at_ms", time.Now().UTC().Add(-10*time.Minute).UnixMilli(),
	).Err()
	_ = c.SRem(ctx, "test-queue:processing_set", got.ID).Err()
	_ = c.LRem(ctx, "test-queue:processing", 0, got.ID).Err()

	if err := q.Requeue(ctx, got.ID, got.Attempts, jobs.WithRequeueMinAge(time.Minute)); err != nil {
		t.Fatalf("requeue stale reclaiming: %v", err)
	}
	out := loadStoredJob(t, mr, got.ID)
	if out["status"] != string(jobs.StatusPending) {
		t.Fatalf("status = %v, want pending", out["status"])
	}
	if !listContains(t, mr, "test-queue:ready", got.ID) {
		t.Fatal("id must be on ready after recovering stale reclaiming")
	}
}

func TestJobQueue_Complete_PendingOnProcessingReturnsToReady(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "claim-gap", nil)
	_ = q.Enqueue(ctx, job)
	// Simulate crashed claim: pending meta + processing membership, aged stamp.
	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	_ = c.RPopLPush(ctx, "test-queue:ready", "test-queue:processing").Err()
	_ = c.SAdd(ctx, "test-queue:processing_set", job.ID).Err()
	old := time.Now().UTC().Add(-10 * time.Minute).UnixMilli()
	_ = c.HSet(ctx, "test-queue:jobmeta:"+job.ID,
		"status", "pending",
		"attempts", 0,
		"claimed_at_ms", old,
		"updated_at_ms", old,
	).Err()

	err := q.Complete(ctx, job.ID, 1)
	if err == nil || !errors.Is(err, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("Complete: %v, want ErrOutcomeNotApplied", err)
	}
	if !listContains(t, mr, "test-queue:ready", job.ID) {
		t.Fatal("aged pending+processing orphan must return to ready")
	}
	if n := listLen(t, mr, "test-queue:ready"); n != 1 {
		t.Fatalf("ready len = %d, want 1 after claim-gap repair", n)
	}
}

func listLen(t *testing.T, mr *miniredis.Miniredis, key string) int {
	t.Helper()
	items, err := mr.List(key)
	if err != nil {
		return 0
	}
	return len(items)
}

func TestJobQueue_Complete_AfterRequeue_DoesNotDoubleReady(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "no-double", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	ageProcessingClaim(t, mr, got.ID, 10*time.Minute)
	if err := q.Requeue(ctx, got.ID, got.Attempts, jobs.WithRequeueMinAge(time.Minute)); err != nil {
		t.Fatalf("Requeue: %v", err)
	}
	if n := listLen(t, mr, "test-queue:ready"); n != 1 {
		t.Fatalf("ready after requeue = %d, want 1", n)
	}
	// Stale Complete from the old claim must not LPUSH a second ready entry.
	err := q.Complete(ctx, got.ID, got.Attempts)
	if err == nil || !errors.Is(err, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("Complete after requeue: %v, want ErrOutcomeNotApplied", err)
	}
	if n := listLen(t, mr, "test-queue:ready"); n != 1 {
		t.Fatalf("ready after stale Complete = %d, want 1 (no second LPUSH)", n)
	}
}

func TestJobQueue_Complete_AfterFailRetry_DoesNotPushReady(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "delayed-stale", nil)
	job.MaxRetries = 3
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	if err := q.Fail(ctx, got.ID, got.Attempts, errors.New("temp")); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if !delayedContains(t, mr, got.ID) {
		t.Fatal("expected delayed member")
	}
	if listContains(t, mr, "test-queue:ready", got.ID) {
		t.Fatal("must not be on ready after Fail retry")
	}
	err := q.Complete(ctx, got.ID, got.Attempts)
	if err == nil || !errors.Is(err, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("Complete after Fail: %v", err)
	}
	if listContains(t, mr, "test-queue:ready", got.ID) {
		t.Fatal("stale Complete must not LPUSH a delayed retry onto ready")
	}
	if !delayedContains(t, mr, got.ID) {
		t.Fatal("delayed membership must remain")
	}
}

func TestJobQueue_Requeue_YoungReclaimingIsInProgress(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "young-reclaim", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	ageProcessingClaim(t, mr, got.ID, 10*time.Minute)

	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	raw, _ := c.Get(ctx, "test-queue:job:"+got.ID).Bytes()
	var doc map[string]interface{}
	_ = json.Unmarshal(raw, &doc)
	doc["status"] = string(jobs.StatusReclaiming)
	blob, _ := json.Marshal(doc)
	_ = c.Set(ctx, "test-queue:job:"+got.ID, blob, 0).Err()
	_ = c.HSet(ctx, "test-queue:jobmeta:"+got.ID,
		"status", "reclaiming",
		"attempts", got.Attempts,
		"reclaiming_at_ms", time.Now().UTC().UnixMilli(),
	).Err()
	_ = c.SRem(ctx, "test-queue:processing_set", got.ID).Err()
	_ = c.LRem(ctx, "test-queue:processing", 0, got.ID).Err()

	err := q.Requeue(ctx, got.ID, got.Attempts, jobs.WithRequeueMinAge(time.Minute))
	if err == nil || !errors.Is(err, jobs.ErrRequeueInProgress) {
		t.Fatalf("err = %v, want ErrRequeueInProgress", err)
	}
}

func TestJobQueue_Requeue_OverlappingSecondDoesNotSteal(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "overlap", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	ageProcessingClaim(t, mr, got.ID, 10*time.Minute)

	started := make(chan struct{})
	release := make(chan struct{})
	errCh := make(chan error, 2)
	minAge := jobs.WithRequeueMinAge(time.Minute)

	go func() {
		errCh <- q.RequeueWithBeforeCommit(ctx, got.ID, got.Attempts, func(context.Context, string, int) error {
			close(started)
			<-release
			return nil
		}, minAge)
	}()

	<-started
	second := q.RequeueWithBeforeCommit(ctx, got.ID, got.Attempts, func(context.Context, string, int) error {
		t.Error("second hook must not run while first reclaim is in progress")
		return nil
	}, minAge)
	if second == nil || !errors.Is(second, jobs.ErrRequeueInProgress) {
		t.Fatalf("second requeue = %v, want ErrRequeueInProgress", second)
	}
	if n := listLen(t, mr, "test-queue:ready"); n != 0 {
		t.Fatalf("ready = %d during first hook, want 0", n)
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatalf("first requeue: %v", err)
	}
	if n := listLen(t, mr, "test-queue:ready"); n != 1 {
		t.Fatalf("ready after first hook = %d, want 1", n)
	}
}

// Holder began with DefaultRequeueMinAge; a second call with MinRequeueMinAge
// must not abort after 1s and steal the reclaim.
func TestJobQueue_Requeue_OverlappingShorterMinAgeDoesNotSteal(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "overlap-short", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	ageProcessingClaim(t, mr, got.ID, 10*time.Minute)

	started := make(chan struct{})
	release := make(chan struct{})
	errCh := make(chan error, 1)

	go func() {
		errCh <- q.RequeueWithBeforeCommit(ctx, got.ID, got.Attempts, func(context.Context, string, int) error {
			close(started)
			<-release
			return nil
		}, jobs.WithRequeueMinAge(jobs.DefaultRequeueMinAge))
	}()

	<-started
	time.Sleep(jobs.MinRequeueMinAge + 200*time.Millisecond)
	second := q.RequeueWithBeforeCommit(ctx, got.ID, got.Attempts, func(context.Context, string, int) error {
		t.Error("second hook must not run; in-progress lease is DefaultRequeueMinAge, not caller min-age")
		return nil
	}, jobs.WithRequeueMinAge(jobs.MinRequeueMinAge))
	if second == nil || !errors.Is(second, jobs.ErrRequeueInProgress) {
		t.Fatalf("second requeue = %v, want ErrRequeueInProgress", second)
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatalf("first requeue: %v", err)
	}
	if n := listLen(t, mr, "test-queue:ready"); n != 1 {
		t.Fatalf("ready after first hook = %d, want 1", n)
	}
}

// A live reclaim that leaked onto ready is popped and left alone: meta,
// attempt and reclaiming_at_ms survive, and only commit schedules it.
func TestJobQueue_Dequeue_ReclaimingOnReadyIsNotClaimed(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "reclaim-ready", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	attempt := got.Attempts

	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	raw, _ := c.Get(ctx, "test-queue:job:"+got.ID).Bytes()
	var doc map[string]interface{}
	_ = json.Unmarshal(raw, &doc)
	doc["status"] = string(jobs.StatusReclaiming)
	blob, _ := json.Marshal(doc)
	stamp := time.Now().UTC().UnixMilli()
	_ = c.Set(ctx, "test-queue:job:"+got.ID, blob, 0).Err()
	_ = c.HSet(ctx, "test-queue:jobmeta:"+got.ID,
		"status", "reclaiming",
		"attempts", attempt,
		"reclaiming_at_ms", stamp,
	).Err()
	_ = c.SRem(ctx, "test-queue:processing_set", got.ID).Err()
	_ = c.LRem(ctx, "test-queue:processing", 0, got.ID).Err()
	_ = c.LPush(ctx, "test-queue:ready", got.ID).Err()

	claimed, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if claimed != nil {
		t.Fatalf("claimed %s; a live reclaim must not be handed to a worker", claimed.ID)
	}
	meta, err := c.HGetAll(ctx, "test-queue:jobmeta:"+got.ID).Result()
	if err != nil {
		t.Fatal(err)
	}
	if meta["status"] != string(jobs.StatusReclaiming) {
		t.Fatalf("meta status = %q, want reclaiming", meta["status"])
	}
	if meta["attempts"] != fmt.Sprint(attempt) {
		t.Fatalf("meta attempts = %q, want %d", meta["attempts"], attempt)
	}
	if meta["reclaiming_at_ms"] != fmt.Sprint(stamp) {
		t.Fatalf("reclaiming_at_ms = %q, want %d", meta["reclaiming_at_ms"], stamp)
	}
	if n := listLen(t, mr, "test-queue:ready"); n != 0 {
		t.Fatalf("ready = %d, want 0", n)
	}
	if n := listLen(t, mr, "test-queue:processing"); n != 0 {
		t.Fatalf("processing list = %d, want 0", n)
	}
}

// staleReclaim leaves id in a crashed reclaim (off processing) whose
// reclaiming_at_ms is older than the lease.
func staleReclaim(t *testing.T, mr *miniredis.Miniredis, c *goredis.Client, jobID string, attempt int) {
	t.Helper()
	ctx := context.Background()
	raw, _ := c.Get(ctx, "test-queue:job:"+jobID).Bytes()
	var doc map[string]interface{}
	_ = json.Unmarshal(raw, &doc)
	oldAt := time.Now().UTC().Add(-2 * jobs.DefaultRequeueMinAge)
	doc["status"] = string(jobs.StatusReclaiming)
	doc["updated_at"] = oldAt.Format(time.RFC3339Nano)
	blob, _ := json.Marshal(doc)
	_ = c.Set(ctx, "test-queue:job:"+jobID, blob, 0).Err()
	old := oldAt.UnixMilli()
	_ = c.HSet(ctx, "test-queue:jobmeta:"+jobID,
		"status", "reclaiming",
		"attempts", attempt,
		"reclaiming_at_ms", old,
		"updated_at_ms", old,
	).Err()
	_ = c.SRem(ctx, "test-queue:processing_set", jobID).Err()
	_ = c.LRem(ctx, "test-queue:processing", 0, jobID).Err()
}

func TestJobQueue_Complete_RepairsStaleReclaimOnce(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "stale-repair", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	staleReclaim(t, mr, c, got.ID, got.Attempts)

	for i := 0; i < 2; i++ {
		if err := q.Complete(ctx, got.ID, got.Attempts); !errors.Is(err, jobs.ErrOutcomeNotApplied) {
			t.Fatalf("Complete #%d: %v, want ErrOutcomeNotApplied", i, err)
		}
	}
	if n := listLen(t, mr, "test-queue:ready"); n != 1 {
		t.Fatalf("ready = %d, want exactly 1 after repair", n)
	}
	if n := listLen(t, mr, "test-queue:processing"); n != 0 {
		t.Fatalf("processing list = %d, want 0", n)
	}
	if out := loadStoredJob(t, mr, got.ID); out["status"] != string(jobs.StatusPending) {
		t.Fatalf("status = %v, want pending", out["status"])
	}
	next, err := q.Dequeue(ctx)
	if err != nil || next == nil || next.Attempts != got.Attempts+1 {
		t.Fatalf("Dequeue after repair = %+v, %v; want attempt %d", next, err, got.Attempts+1)
	}
}

func TestJobQueue_Fail_RepairDoesNotDuplicateReady(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "stale-repair-ready", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	staleReclaim(t, mr, c, got.ID, got.Attempts)
	_ = c.LPush(ctx, "test-queue:ready", got.ID).Err()

	if err := q.Fail(ctx, got.ID, got.Attempts, errors.New("late")); !errors.Is(err, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("Fail: %v, want ErrOutcomeNotApplied", err)
	}
	if n := listLen(t, mr, "test-queue:ready"); n != 1 {
		t.Fatalf("ready = %d, want 1 (already on ready)", n)
	}
}

func TestJobQueue_Complete_YoungReclaimNotRepaired(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "young-repair", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	staleReclaim(t, mr, c, got.ID, got.Attempts)
	_ = c.HSet(ctx, "test-queue:jobmeta:"+got.ID, "reclaiming_at_ms", time.Now().UTC().UnixMilli()).Err()

	_ = q.Complete(ctx, got.ID, got.Attempts)
	if n := listLen(t, mr, "test-queue:ready"); n != 0 {
		t.Fatalf("ready = %d, want 0 inside lease", n)
	}
	if out := loadStoredJob(t, mr, got.ID); out["status"] != string(jobs.StatusReclaiming) {
		t.Fatalf("status = %v, want reclaiming", out["status"])
	}
}

func TestJobQueue_Requeue_MissingReclaimStampFallsBackToUpdatedAt(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "nil-stamp", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	staleReclaim(t, mr, c, got.ID, got.Attempts)
	_ = c.HDel(ctx, "test-queue:jobmeta:"+got.ID, "reclaiming_at_ms").Err()

	if err := q.Requeue(ctx, got.ID, got.Attempts, jobs.WithRequeueMinAge(time.Minute)); err != nil {
		t.Fatalf("Requeue with missing stamp and old updated_at: %v", err)
	}
	if n := listLen(t, mr, "test-queue:ready"); n != 1 {
		t.Fatalf("ready = %d, want 1", n)
	}
}

func TestJobQueue_Dequeue_TerminalOnReadyPreservesMeta(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	doneJob, _ := jobs.NewJob(id.New(), "terminal-leak", nil)
	_ = q.Enqueue(ctx, doneJob)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	if err := q.Complete(ctx, got.ID, got.Attempts); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	// RPOP pops the rightmost id — put the terminal leak there first, then
	// enqueue a real pending so Dequeue skips done then claims pending.
	_ = c.LPush(ctx, "test-queue:ready", got.ID).Err()

	pending, _ := jobs.NewJob(id.New(), "after-terminal", nil)
	_ = q.Enqueue(ctx, pending)

	claimed, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if claimed == nil || claimed.ID != pending.ID {
		t.Fatalf("claimed = %+v, want pending %s", claimed, pending.ID)
	}
	meta, err := c.HGetAll(ctx, "test-queue:jobmeta:"+got.ID).Result()
	if err != nil {
		t.Fatal(err)
	}
	if meta["status"] != string(jobs.StatusDone) {
		t.Fatalf("terminal meta = %q, want done (dropClaim must not rewrite)", meta["status"])
	}
	out := loadStoredJob(t, mr, got.ID)
	if out["status"] != string(jobs.StatusDone) {
		t.Fatalf("terminal blob = %v, want done", out["status"])
	}
}

func TestJobQueue_Complete_DuringClaimGap_LeavesSingleOwner(t *testing.T) {
	mr, q := setupJobQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "claim-race", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	ageProcessingClaim(t, mr, got.ID, 10*time.Minute)
	oldAttempt := got.Attempts
	if err := q.Requeue(ctx, got.ID, oldAttempt, jobs.WithRequeueMinAge(time.Minute)); err != nil {
		t.Fatalf("Requeue: %v", err)
	}
	if n := listLen(t, mr, "test-queue:ready"); n != 1 {
		t.Fatalf("ready = %d", n)
	}

	// Simulate claimJobLua without saveJob: meta→processing, on set, blob still pending.
	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	now := time.Now().UTC().UnixMilli()
	id, err := c.Eval(ctx, `
local id = redis.call('RPOP', KEYS[1])
if not id then return false end
redis.call('LPUSH', KEYS[2], id)
redis.call('SADD', KEYS[3], id)
local meta = ARGV[1] .. id
local attempts = tonumber(redis.call('HGET', meta, 'attempts') or '0') + 1
redis.call('HSET', meta, 'status', 'processing', 'attempts', attempts, 'updated_at_ms', ARGV[2], 'claimed_at_ms', ARGV[2])
return {id, tostring(attempts)}
`, []string{"test-queue:ready", "test-queue:processing", "test-queue:processing_set"},
		"test-queue:jobmeta:", now).Result()
	if err != nil {
		t.Fatal(err)
	}
	pair := id.([]interface{})
	claimedID := pair[0].(string)
	newAttempt := 0
	fmt.Sscanf(fmt.Sprint(pair[1]), "%d", &newAttempt)

	// Stale Complete from previous owner during claim→saveJob gap.
	cerr := q.Complete(ctx, claimedID, oldAttempt)
	if cerr == nil || !errors.Is(cerr, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("stale Complete: %v", cerr)
	}
	if n := listLen(t, mr, "test-queue:ready"); n != 0 {
		t.Fatalf("ready = %d, want 0 (no second schedule)", n)
	}
	onSet, _ := c.SIsMember(ctx, "test-queue:processing_set", claimedID).Result()
	if !onSet {
		t.Fatal("new claim must keep processing-set membership")
	}

	// Finish saveJob as Dequeue would, then Complete with new attempt.
	doc := loadStoredJob(t, mr, claimedID)
	doc["status"] = string(jobs.StatusProcessing)
	doc["attempts"] = float64(newAttempt)
	blob, _ := json.Marshal(doc)
	_ = c.Set(ctx, "test-queue:job:"+claimedID, blob, 0).Err()
	if err := q.Complete(ctx, claimedID, newAttempt); err != nil {
		t.Fatalf("owner Complete: %v", err)
	}
	out := loadStoredJob(t, mr, claimedID)
	if out["status"] != string(jobs.StatusDone) {
		t.Fatalf("status = %v, want done", out["status"])
	}
}
