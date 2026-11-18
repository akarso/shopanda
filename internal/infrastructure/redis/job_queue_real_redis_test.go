package redis

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/id"
)

// TestJobQueue_RepairStaleReclaim_Redis5 requires SHOPANDA_TEST_REDIS_URL
// pointing at a Redis 5.x instance (CI integration service redis:5-alpine).
// It does not fall back to Docker or miniredis — a missing daemon must not
// skip this check when the env is set, and unit runs without the env skip.
func TestJobQueue_RepairStaleReclaim_Redis5(t *testing.T) {
	url := os.Getenv("SHOPANDA_TEST_REDIS_URL")
	if url == "" {
		t.Skip("SHOPANDA_TEST_REDIS_URL not set")
	}
	q, err := NewJobQueue(QueueConfig{URL: url, KeyPrefix: "r5:" + t.Name() + ":"})
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	info, err := q.client.Info(ctx, "server").Result()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info, "redis_version:5.") {
		t.Fatalf("SHOPANDA_TEST_REDIS_URL must point at Redis 5.x for this test, got:\n%s", info)
	}

	job, _ := jobs.NewJob(id.New(), "r5-repair", nil)
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := q.Dequeue(ctx)
	if err != nil || got == nil {
		t.Fatalf("Dequeue: %v %v", err, got)
	}

	raw, _ := q.client.Get(ctx, q.jobKey(got.ID)).Bytes()
	var doc map[string]interface{}
	_ = json.Unmarshal(raw, &doc)
	oldAt := time.Now().UTC().Add(-2 * jobs.DefaultRequeueMinAge)
	doc["status"] = string(jobs.StatusReclaiming)
	doc["updated_at"] = oldAt.Format(time.RFC3339Nano)
	blob, _ := json.Marshal(doc)
	_ = q.client.Set(ctx, q.jobKey(got.ID), blob, 0).Err()
	_ = q.client.HSet(ctx, q.jobMetaKey(got.ID),
		"status", "reclaiming",
		"attempts", got.Attempts,
		"reclaiming_at_ms", oldAt.UnixMilli(),
		"updated_at_ms", oldAt.UnixMilli(),
	).Err()
	_ = q.client.SRem(ctx, q.processingSetKey(), got.ID).Err()
	_ = q.client.LRem(ctx, q.processingKey(), 0, got.ID).Err()

	cerr := q.Complete(ctx, got.ID, got.Attempts)
	if !errors.Is(cerr, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("Complete: %v, want ErrOutcomeNotApplied (repair must not fail on Redis 5)", cerr)
	}
	n, err := q.client.LLen(ctx, q.readyKey()).Result()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("ready = %d, want 1 after Redis 5 repair (LPOS must not be required)", n)
	}
	meta, err := q.client.HGet(ctx, q.jobMetaKey(got.ID), "status").Result()
	if err != nil || meta != string(jobs.StatusPending) {
		t.Fatalf("meta status = %q err=%v, want pending", meta, err)
	}
}

func TestJobQueue_CommitPendingWithoutReadyIsRescheduled(t *testing.T) {
	q := newInternalQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "crash-commit", nil)
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := q.Dequeue(ctx)
	if err != nil || got == nil {
		t.Fatalf("Dequeue: %v %v", err, got)
	}
	q.ageClaim(t, got.ID)

	reclaiming := storedJob{
		ID: got.ID, Type: got.Type, Payload: got.Payload,
		Status: jobs.StatusReclaiming, Attempts: got.Attempts, MaxRetries: got.MaxRetries,
		CreatedAt: got.CreatedAt, UpdatedAt: got.UpdatedAt,
	}
	reclaimBlob, _ := json.Marshal(reclaiming)
	n, err := q.client.Eval(ctx, beginRequeueLua,
		[]string{q.jobKey(got.ID), q.processingKey(), q.processingSetKey(), q.jobMetaKey(got.ID)},
		got.Attempts, got.ID, int64(time.Minute/time.Millisecond), reclaimBlob,
	).Int()
	if err != nil || n != 1 {
		t.Fatalf("begin: n=%d err=%v", n, err)
	}

	// Crash after meta→pending, before ready push.
	pending := reclaiming
	pending.Status = jobs.StatusPending
	pendingBlob, _ := json.Marshal(pending)
	_ = q.client.Set(ctx, q.jobKey(got.ID), pendingBlob, 0).Err()
	_ = q.client.HSet(ctx, q.jobMetaKey(got.ID), "status", "pending", "attempts", got.Attempts).Err()
	_ = q.client.HDel(ctx, q.jobMetaKey(got.ID), "reclaiming_at_ms").Err()

	proc := reclaiming
	proc.Status = jobs.StatusProcessing
	procBlob, _ := json.Marshal(proc)
	if err := q.commitReclaim(ctx, got.ID, got.Attempts, pendingBlob, procBlob, got.UpdatedAt.UnixMilli()); err != nil {
		t.Fatalf("commitReclaim: %v", err)
	}
	ready, _ := q.client.LLen(ctx, q.readyKey()).Result()
	if ready != 1 {
		t.Fatalf("ready = %d, want 1 after pending-without-schedule recovery", ready)
	}
}
