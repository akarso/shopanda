package redis

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func newInternalQueue(t *testing.T) *JobQueue {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	q, err := NewJobQueue(QueueConfig{URL: "redis://" + mr.Addr(), KeyPrefix: "test-queue:"})
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func (q *JobQueue) ageClaim(t *testing.T, id string) {
	t.Helper()
	age := time.Now().UTC().Add(-10 * time.Minute).UnixMilli()
	if err := q.client.HSet(context.Background(), q.jobMetaKey(id), "updated_at_ms", age, "claimed_at_ms", age).Err(); err != nil {
		t.Fatal(err)
	}
}

func TestJobQueue_saveJob_RefusesReclaimingOverwrite(t *testing.T) {
	q := newInternalQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "late-save", nil)
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := q.Dequeue(ctx)
	if err != nil || got == nil {
		t.Fatalf("Dequeue: %v job=%v", err, got)
	}
	q.ageClaim(t, got.ID)

	started := make(chan struct{})
	release := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- q.RequeueWithBeforeCommit(ctx, got.ID, got.Attempts, func(context.Context, string, int) error {
			close(started)
			<-release
			return nil
		}, jobs.WithRequeueMinAge(time.Minute))
	}()
	<-started

	late := storedJob{
		ID:         got.ID,
		Type:       got.Type,
		Payload:    got.Payload,
		Status:     jobs.StatusProcessing,
		Attempts:   got.Attempts,
		MaxRetries: got.MaxRetries,
		CreatedAt:  got.CreatedAt,
		UpdatedAt:  time.Now().UTC(),
	}
	if err := q.saveJob(ctx, late); err == nil {
		t.Fatal("late saveJob over reclaiming must refuse")
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatalf("requeue: %v", err)
	}
	stored, err := q.loadJob(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != jobs.StatusPending {
		t.Fatalf("status = %s, want pending after reclaim commit (late save must not win)", stored.Status)
	}
}

// A reclaim that wins the claimJobLua→saveJob gap must not be handed to a
// worker or pushed to ready by Dequeue; only commit schedules it, once.
func TestJobQueue_Dequeue_ReclaimInClaimGapIsNotRun(t *testing.T) {
	for _, stage := range []string{"claimed", "loaded"} {
		t.Run(stage, func(t *testing.T) {
			q := newInternalQueue(t)
			ctx := context.Background()

			job, _ := jobs.NewJob(id.New(), "gap-"+stage, nil)
			if err := q.Enqueue(ctx, job); err != nil {
				t.Fatal(err)
			}

			started := make(chan struct{})
			release := make(chan struct{})
			errCh := make(chan error, 1)
			fired := false
			q.dequeueStage = func(name, jid string, attempt int) {
				if name != stage || jid != job.ID || fired {
					return
				}
				fired = true
				q.ageClaim(t, jid)
				go func() {
					errCh <- q.RequeueWithBeforeCommit(ctx, jid, attempt, func(context.Context, string, int) error {
						close(started)
						<-release
						return nil
					}, jobs.WithRequeueMinAge(jobs.MinRequeueMinAge))
				}()
				<-started
			}

			got, err := q.Dequeue(ctx)
			if err != nil {
				t.Fatalf("Dequeue: %v", err)
			}
			if got != nil {
				t.Fatalf("Dequeue returned %s while OnRequeued holds the reclaim", got.ID)
			}
			if n, _ := q.client.LLen(ctx, q.readyKey()).Result(); n != 0 {
				t.Fatalf("ready = %d during hook, want 0", n)
			}
			if again, _ := q.Dequeue(ctx); again != nil {
				t.Fatalf("second Dequeue returned %s during hook", again.ID)
			}

			close(release)
			if err := <-errCh; err != nil {
				t.Fatalf("requeue: %v", err)
			}
			if n, _ := q.client.LLen(ctx, q.readyKey()).Result(); n != 1 {
				t.Fatalf("ready = %d after commit, want exactly 1", n)
			}
			next, err := q.Dequeue(ctx)
			if err != nil || next == nil || next.ID != job.ID {
				t.Fatalf("Dequeue after commit = %+v, %v", next, err)
			}
			if next.Attempts != 2 {
				t.Fatalf("attempts = %d, want 2 (one claim before reclaim, one after)", next.Attempts)
			}
		})
	}
}

func TestJobQueue_FailRetry_ClearsReadyBesideDelayed(t *testing.T) {
	q := newInternalQueue(t)
	ctx := context.Background()
	job, _ := jobs.NewJob(id.New(), "fail-delay", nil)
	job.MaxRetries = 3
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := q.Dequeue(ctx)
	if err != nil || got == nil {
		t.Fatalf("Dequeue: %v %v", err, got)
	}
	// Leftover ready copy beside the claim (would race a delayed retry).
	_ = q.client.LPush(ctx, q.readyKey(), got.ID).Err()
	if err := q.Fail(ctx, got.ID, got.Attempts, errors.New("temp")); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	if n, _ := q.client.LLen(ctx, q.readyKey()).Result(); n != 0 {
		t.Fatalf("ready = %d after delayed Fail, want 0", n)
	}
	if _, err := q.client.ZScore(ctx, q.delayedKey(), got.ID).Result(); err != nil {
		t.Fatalf("expected delayed member: %v", err)
	}
}

func TestJobQueue_Complete_DropsDelayedReadyAndExpiresMeta(t *testing.T) {
	q := newInternalQueue(t)
	ctx := context.Background()
	job, _ := jobs.NewJob(id.New(), "done-delay", nil)
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := q.Dequeue(ctx)
	if err != nil || got == nil {
		t.Fatalf("Dequeue: %v %v", err, got)
	}
	_ = q.client.ZAdd(ctx, q.delayedKey(), goredis.Z{
		Score:  float64(time.Now().UTC().Add(time.Hour).UnixMilli()),
		Member: got.ID,
	}).Err()
	_ = q.client.LPush(ctx, q.readyKey(), got.ID).Err()
	if err := q.Complete(ctx, got.ID, got.Attempts); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, err := q.client.ZScore(ctx, q.delayedKey(), got.ID).Result(); err == nil {
		t.Fatal("delayed member must be removed on Complete")
	}
	if n, _ := q.client.LLen(ctx, q.readyKey()).Result(); n != 0 {
		t.Fatalf("ready = %d after Complete, want 0", n)
	}
	ttl, err := q.client.PTTL(ctx, q.jobMetaKey(got.ID)).Result()
	if err != nil || ttl <= 0 {
		t.Fatalf("meta PTTL = %v err=%v, want positive (same retention as blob)", ttl, err)
	}
}

func TestJobQueue_releaseClaim_SkipsPushWhenDelayed(t *testing.T) {
	q := newInternalQueue(t)
	ctx := context.Background()
	job, _ := jobs.NewJob(id.New(), "release-delay", nil)
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := q.Dequeue(ctx)
	if err != nil || got == nil {
		t.Fatalf("Dequeue: %v %v", err, got)
	}
	claimedAt, _ := q.client.HGet(ctx, q.jobMetaKey(got.ID), "claimed_at_ms").Result()
	_ = q.client.ZAdd(ctx, q.delayedKey(), goredis.Z{
		Score:  float64(time.Now().UTC().Add(time.Hour).UnixMilli()),
		Member: got.ID,
	}).Err()
	_ = q.client.LPush(ctx, q.readyKey(), got.ID).Err()
	if err := q.releaseClaim(ctx, got.ID, got.Attempts, claimedAt, jobs.StatusPending, got.Attempts-1, true); err != nil {
		t.Fatal(err)
	}
	if n, _ := q.client.LLen(ctx, q.readyKey()).Result(); n != 0 {
		t.Fatalf("ready = %d, want 0 (delayed wins over releaseClaim push)", n)
	}
	if _, err := q.client.ZScore(ctx, q.delayedKey(), got.ID).Result(); err != nil {
		t.Fatalf("delayed must remain: %v", err)
	}
}

func TestJobQueue_Enqueue_RejectsDuplicateID(t *testing.T) {
	q := newInternalQueue(t)
	ctx := context.Background()
	job, _ := jobs.NewJob(id.New(), "dup", nil)
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, job); err == nil {
		t.Fatal("second Enqueue of same id must fail")
	}
}

func TestJobQueue_DelayedClearsReadyCopies(t *testing.T) {
	q := newInternalQueue(t)
	ctx := context.Background()
	job, _ := jobs.NewJob(id.New(), "both", nil)
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	// Leak a ready copy beside a delayed member (old Enqueue race shape).
	_ = q.client.LPush(ctx, q.readyKey(), job.ID).Err()
	_ = q.client.ZAdd(ctx, q.delayedKey(), goredis.Z{
		Score:  float64(time.Now().UTC().Add(time.Hour).UnixMilli()),
		Member: job.ID,
	}).Err()
	// Ensure pending path must strip ready when delayed wins.
	if err := q.ensurePendingScheduled(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := q.client.LLen(ctx, q.readyKey()).Result(); n != 0 {
		t.Fatalf("ready = %d, want 0 while delayed", n)
	}
	if sc, _ := q.client.ZScore(ctx, q.delayedKey(), job.ID).Result(); sc == 0 {
		t.Fatal("delayed membership must remain")
	}

	// Due promote must leave a single ready entry.
	_ = q.client.ZAdd(ctx, q.delayedKey(), goredis.Z{
		Score:  float64(time.Now().UTC().Add(-time.Second).UnixMilli()),
		Member: job.ID,
	}).Err()
	_ = q.client.LPush(ctx, q.readyKey(), job.ID).Err() // stale ready beside delayed
	if err := q.promoteDelayed(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := q.client.LLen(ctx, q.readyKey()).Result(); n != 1 {
		t.Fatalf("ready after promote = %d, want 1", n)
	}
	if _, err := q.client.ZScore(ctx, q.delayedKey(), job.ID).Result(); err == nil {
		t.Fatal("delayed member must be removed after promote")
	}
}

func TestJobQueue_Dequeue_StampsClaimFromRedisClock(t *testing.T) {
	q := newInternalQueue(t)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "redis-clock", nil)
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatal(err)
	}
	var claimedAt string
	q.dequeueStage = func(name, jid string, _ int) {
		if name == "claimed" {
			claimedAt, _ = q.client.HGet(ctx, q.jobMetaKey(jid), "claimed_at_ms").Result()
		}
	}
	got, err := q.Dequeue(ctx)
	if err != nil || got == nil {
		t.Fatalf("Dequeue: %v job=%v", err, got)
	}
	updated, _ := q.client.HGet(ctx, q.jobMetaKey(got.ID), "updated_at_ms").Result()
	if claimedAt == "" || updated != claimedAt {
		t.Fatalf("updated_at_ms = %q, claimed_at_ms = %q; saveJob must keep the claim stamp", updated, claimedAt)
	}
	if want := strconv.FormatInt(got.UpdatedAt.UnixMilli(), 10); updated != want {
		t.Fatalf("job.UpdatedAt = %s, meta updated_at_ms = %q", want, updated)
	}
}
