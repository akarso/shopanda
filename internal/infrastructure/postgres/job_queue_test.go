package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

func TestJobQueue_Enqueue(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
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
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	// Enqueue a job.
	job, err := jobs.NewJob(id.New(), "send_email", map[string]interface{}{"to": "a@b.com"})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Dequeue it.
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
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
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
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
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

	// Verify it's no longer dequeue-able.
	next, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue after complete: %v", err)
	}
	if next != nil {
		t.Fatal("expected nil after complete, got a job")
	}
}

func TestJobQueue_Fail_Retry(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "test", nil)
	_ = q.Enqueue(ctx, job)

	got, _ := q.Dequeue(ctx)

	// Capture time before Fail so we can assert a bounded backoff window.
	now := time.Now()

	// Fail the job — should be re-queued since attempts(1) < maxRetries(3).
	if err := q.Fail(ctx, got.ID, nil); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	// Check status is back to pending with run_at in the expected backoff window.
	// First retry uses retryDelay(0) = 5s base ±25% jitter → [3.75s, 6.25s].
	var status string
	var runAt time.Time
	if err := db.QueryRow("SELECT status, run_at FROM jobs WHERE id = $1", got.ID).Scan(&status, &runAt); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "pending" {
		t.Errorf("status = %q, want pending", status)
	}
	minDelay := 3750 * time.Millisecond // 5s × 0.75
	maxDelay := 8 * time.Second         // 6.25s + buffer for DB/exec overhead
	if !runAt.After(now.Add(minDelay)) {
		t.Errorf("run_at %v should be after now+minDelay %v", runAt, now.Add(minDelay))
	}
	if !runAt.Before(now.Add(maxDelay)) {
		t.Errorf("run_at %v should be before now+maxDelay %v", runAt, now.Add(maxDelay))
	}
}

func TestJobQueue_Fail_Permanent(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	// Create a job with max_retries=1 so first dequeue (attempt 1) exhausts retries.
	job, _ := jobs.NewJob(id.New(), "test", nil)
	job.MaxRetries = 1
	_ = q.Enqueue(ctx, job)

	got, _ := q.Dequeue(ctx) // attempts becomes 1

	if err := q.Fail(ctx, got.ID, nil); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	var status string
	if err := db.QueryRow("SELECT status FROM jobs WHERE id = $1", got.ID).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}
}

func TestJobQueue_Complete_NotFound(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	err = q.Complete(ctx, id.New())
	if err == nil {
		t.Fatal("expected error for non-existent job")
	}
}

// TestJobQueue_Fail_RecordsLastError pins the fix for a failed job's
// reason never being persisted anywhere: Fail(ctx, id, jobErr) now stores
// jobErr.Error() as last_error, retrievable via Get — both on the
// re-queued-for-retry path and the permanently-failed path.
func TestJobQueue_Fail_RecordsLastError(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "test", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)

	if err := q.Fail(ctx, got.ID, errors.New("boom: upstream timed out")); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	detail, err := q.Get(ctx, got.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if detail == nil {
		t.Fatal("Get returned nil")
	}
	if detail.LastError != "boom: upstream timed out" {
		t.Errorf("LastError = %q, want %q", detail.LastError, "boom: upstream timed out")
	}
}

// TestJobQueue_Fail_NilErrorLeavesLastErrorUnchanged pins Fail's documented
// behavior for the "no handler registered" path (worker.go calls Fail with
// a nil jobErr there): it must not invent a message, and must not clobber
// whatever was already recorded from a prior failure.
func TestJobQueue_Fail_NilErrorLeavesLastErrorUnchanged(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "test", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)

	if err := q.Fail(ctx, got.ID, nil); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	detail, err := q.Get(ctx, got.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.LastError != "" {
		t.Errorf("LastError = %q, want empty (nil jobErr must not invent a message)", detail.LastError)
	}
}

func TestJobQueue_List(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	a, _ := jobs.NewJob(id.New(), "type_a", nil)
	b, _ := jobs.NewJob(id.New(), "type_b", nil)
	_ = q.Enqueue(ctx, a)
	_ = q.Enqueue(ctx, b)

	all, err := q.List(ctx, jobs.ListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List (unfiltered) len = %d, want 2", len(all))
	}

	filtered, err := q.List(ctx, jobs.ListFilter{Type: "type_a", Limit: 10})
	if err != nil {
		t.Fatalf("List (filtered): %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != a.ID {
		t.Fatalf("List (type_a) = %+v, want just %s", filtered, a.ID)
	}

	byStatus, err := q.List(ctx, jobs.ListFilter{Status: jobs.StatusPending, Limit: 10})
	if err != nil {
		t.Fatalf("List (by status): %v", err)
	}
	if len(byStatus) != 2 {
		t.Fatalf("List (pending) len = %d, want 2", len(byStatus))
	}

	paged, err := q.List(ctx, jobs.ListFilter{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("List (paged): %v", err)
	}
	if len(paged) != 1 {
		t.Fatalf("List (limit=1 offset=1) len = %d, want 1", len(paged))
	}
}

// TestJobQueue_List_StableOrderOnEqualCreatedAt pins the fix for unstable
// offset pagination: ORDER BY created_at DESC alone leaves ties undefined
// across separate queries, which can duplicate or omit rows across pages.
// Seeds several jobs sharing one created_at (bypassing NewJob's per-call
// time.Now(), which would make a real collision unlikely in a fast test)
// and confirms paging through them one at a time visits every job exactly
// once, with a stable, repeatable order — the id DESC tie-breaker.
func TestJobQueue_List_StableOrderOnEqualCreatedAt(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	const n = 5
	same := time.Now().UTC()
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		job, _ := jobs.NewJob(id.New(), "test", nil)
		job.CreatedAt = same
		job.UpdatedAt = same
		job.RunAt = same
		if err := q.Enqueue(ctx, job); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
		ids[i] = job.ID
	}
	// Enqueue writes created_at as given (not server-assigned), so every
	// row above genuinely shares the same timestamp — confirm that before
	// trusting the rest of this test.
	var distinctCreatedAt int
	if err := db.QueryRow("SELECT count(DISTINCT created_at) FROM jobs").Scan(&distinctCreatedAt); err != nil {
		t.Fatalf("count distinct created_at: %v", err)
	}
	if distinctCreatedAt != 1 {
		t.Fatalf("distinct created_at = %d, want 1 (test setup didn't produce a real tie)", distinctCreatedAt)
	}

	seen := make(map[string]int) // id -> how many times it was returned
	var order []string
	for offset := 0; offset < n; offset++ {
		page, err := q.List(ctx, jobs.ListFilter{Limit: 1, Offset: offset})
		if err != nil {
			t.Fatalf("List offset=%d: %v", offset, err)
		}
		if len(page) != 1 {
			t.Fatalf("List offset=%d: len = %d, want 1", offset, len(page))
		}
		seen[page[0].ID]++
		order = append(order, page[0].ID)
	}

	for _, jobID := range ids {
		if seen[jobID] != 1 {
			t.Errorf("job %s appeared %d times across pages, want exactly 1", jobID, seen[jobID])
		}
	}

	// Re-run the same pages and confirm the order repeats exactly —
	// without a deterministic tie-breaker this could vary run to run.
	for offset := 0; offset < n; offset++ {
		page, err := q.List(ctx, jobs.ListFilter{Limit: 1, Offset: offset})
		if err != nil {
			t.Fatalf("List (repeat) offset=%d: %v", offset, err)
		}
		if page[0].ID != order[offset] {
			t.Errorf("offset=%d: got %s on repeat, want %s (order not stable)", offset, page[0].ID, order[offset])
		}
	}
}

func TestJobQueue_Get_NotFound(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}

	got, err := q.Get(context.Background(), id.New())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Fatalf("Get: got %+v, want nil for a non-existent job", got)
	}
}

func TestJobQueue_Get_IncludesPayload(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "test", map[string]interface{}{"order_id": "o-1"})
	_ = q.Enqueue(ctx, job)

	got, err := q.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Payload["order_id"] != "o-1" {
		t.Errorf("Payload[order_id] = %v, want o-1", got.Payload["order_id"])
	}
}

func TestJobQueue_CountsByStatus(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	a, _ := jobs.NewJob(id.New(), "test", nil)
	b, _ := jobs.NewJob(id.New(), "test", nil)
	_ = q.Enqueue(ctx, a)
	_ = q.Enqueue(ctx, b)
	_, _ = q.Dequeue(ctx) // one becomes processing

	counts, err := q.CountsByStatus(ctx)
	if err != nil {
		t.Fatalf("CountsByStatus: %v", err)
	}
	if counts[jobs.StatusPending] != 1 {
		t.Errorf("counts[pending] = %d, want 1", counts[jobs.StatusPending])
	}
	if counts[jobs.StatusProcessing] != 1 {
		t.Errorf("counts[processing] = %d, want 1", counts[jobs.StatusProcessing])
	}
	if _, ok := counts[jobs.StatusFailed]; ok {
		t.Errorf("counts[failed] present with no failed jobs, want absent")
	}
}

func TestJobQueue_Retry_ResetsFailedJobToPending(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "test", nil)
	job.MaxRetries = 1
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx) // attempts becomes 1
	if err := q.Fail(ctx, got.ID, errors.New("boom")); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	var status string
	if err := db.QueryRow("SELECT status FROM jobs WHERE id = $1", got.ID).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "failed" {
		t.Fatalf("status = %q, want failed (test setup precondition)", status)
	}

	if err := q.Retry(ctx, got.ID); err != nil {
		t.Fatalf("Retry: %v", err)
	}

	var attempts int
	if err := db.QueryRow("SELECT status, attempts FROM jobs WHERE id = $1", got.ID).Scan(&status, &attempts); err != nil {
		t.Fatalf("query after retry: %v", err)
	}
	if status != "pending" {
		t.Errorf("status = %q, want pending", status)
	}
	if attempts != 0 {
		t.Errorf("attempts = %d, want 0", attempts)
	}

	// Retried job is dequeue-able again.
	next, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue after retry: %v", err)
	}
	if next == nil || next.ID != got.ID {
		t.Fatalf("Dequeue after retry = %+v, want the retried job", next)
	}
}

func TestJobQueue_Retry_ConflictOnNonFailedJob(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "test", nil)
	_ = q.Enqueue(ctx, job) // stays pending

	err = q.Retry(ctx, job.ID)
	if err == nil {
		t.Fatal("expected error retrying a pending job")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeConflict {
		t.Fatalf("err = %v, want *apperror.Error{Code: conflict}", err)
	}
}

func TestJobQueue_Retry_NotFound(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}

	err = q.Retry(context.Background(), id.New())
	if err == nil {
		t.Fatal("expected error for non-existent job")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeNotFound {
		t.Fatalf("err = %v, want *apperror.Error{Code: not_found}", err)
	}
}

func TestJobQueue_Cancel_MarksPendingJobCancelled(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "test", nil)
	_ = q.Enqueue(ctx, job)

	if err := q.Cancel(ctx, job.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	var status string
	if err := db.QueryRow("SELECT status FROM jobs WHERE id = $1", job.ID).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "cancelled" {
		t.Errorf("status = %q, want cancelled", status)
	}

	// A cancelled job is never dequeued.
	next, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue after cancel: %v", err)
	}
	if next != nil {
		t.Fatalf("Dequeue after cancel = %+v, want nil", next)
	}
}

// TestJobQueue_Cancel_ConflictOnProcessingJob pins the "no in-flight
// cancellation" contract: a processing job cannot be cancelled, and the
// conflict message must name that specific reason (not a generic
// wrong-status message) since it's the most likely point of operator
// confusion.
func TestJobQueue_Cancel_ConflictOnProcessingJob(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "test", nil)
	_ = q.Enqueue(ctx, job)
	if _, err := q.Dequeue(ctx); err != nil { // now processing
		t.Fatalf("Dequeue: %v", err)
	}

	err = q.Cancel(ctx, job.ID)
	if err == nil {
		t.Fatal("expected error cancelling a processing job")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeConflict {
		t.Fatalf("err = %v, want *apperror.Error{Code: conflict}", err)
	}
	if !strings.Contains(appErr.Message, "processing") {
		t.Errorf("message = %q, want it to explain the job is processing", appErr.Message)
	}
}

func TestJobQueue_Cancel_ConflictOnFailedJob(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "test", nil)
	job.MaxRetries = 1
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if err := q.Fail(ctx, got.ID, errors.New("boom")); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	err = q.Cancel(ctx, job.ID)
	if err == nil {
		t.Fatal("expected error cancelling a failed job")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeConflict {
		t.Fatalf("err = %v, want *apperror.Error{Code: conflict}", err)
	}
}

func TestJobQueue_Cancel_NotFound(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}

	err = q.Cancel(context.Background(), id.New())
	if err == nil {
		t.Fatal("expected error for non-existent job")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeNotFound {
		t.Fatalf("err = %v, want *apperror.Error{Code: not_found}", err)
	}
}
