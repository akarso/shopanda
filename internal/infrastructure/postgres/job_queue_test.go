package postgres_test

import (
	"context"
	"database/sql"
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

	if err := q.Complete(ctx, got.ID, got.Attempts); err != nil {
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

func TestJobQueue_Complete_StaleAttempt_LeavesProcessing(t *testing.T) {
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
	job, _ := jobs.NewJob(id.New(), "stale", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	err = q.Complete(ctx, got.ID, got.Attempts-1)
	if err == nil || !errors.Is(err, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("Complete stale: %v", err)
	}
	detail, err := q.Get(ctx, got.ID)
	if err != nil || detail == nil {
		t.Fatalf("Get: %v %#v", err, detail)
	}
	if detail.Status != jobs.StatusProcessing || detail.Attempts != got.Attempts {
		t.Fatalf("row = %+v, want still processing at attempt %d", detail, got.Attempts)
	}
	if err := q.Complete(ctx, got.ID, got.Attempts); err != nil {
		t.Fatalf("Complete matching attempt: %v", err)
	}
}

func TestJobQueue_Requeue_RequiresAgeAndAttempt(t *testing.T) {
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
	job, _ := jobs.NewJob(id.New(), "requeue", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	minAge := jobs.WithRequeueMinAge(time.Minute)
	// Too fresh → ErrRequeueTooFresh even with matching attempt (db clock).
	if err := q.Requeue(ctx, got.ID, got.Attempts, minAge); err == nil || !errors.Is(err, jobs.ErrRequeueTooFresh) {
		t.Fatalf("Requeue fresh claim: %v, want ErrRequeueTooFresh", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE jobs SET updated_at = NOW() - INTERVAL '2 minutes' WHERE id = $1`, got.ID); err != nil {
		t.Fatal(err)
	}
	// Aged but wrong attempt must leave the live row processing.
	if err := q.Requeue(ctx, got.ID, got.Attempts-1, minAge); err == nil || !errors.Is(err, jobs.ErrRequeueWrongAttempt) {
		t.Fatalf("Requeue wrong attempt on aged row: %v, want ErrRequeueWrongAttempt", err)
	}
	detail, err := q.Get(ctx, got.ID)
	if err != nil || detail == nil {
		t.Fatalf("Get after wrong attempt: %v %#v", err, detail)
	}
	if detail.Status != jobs.StatusProcessing || detail.Attempts != got.Attempts {
		t.Fatalf("after wrong attempt = %+v, want still processing", detail)
	}
	if err := q.Requeue(ctx, got.ID, got.Attempts, minAge); err != nil {
		t.Fatalf("Requeue aged claim: %v", err)
	}
	detail, err = q.Get(ctx, got.ID)
	if err != nil || detail == nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.Status != jobs.StatusPending || detail.Attempts != got.Attempts {
		t.Fatalf("after requeue = %+v, want pending with attempts kept", detail)
	}
}

func TestJobQueue_RequeueWithBeforeCommit_HookErrorKeepsProcessing(t *testing.T) {
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
	job, _ := jobs.NewJob(id.New(), "requeue-hook", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	if _, err := db.ExecContext(ctx, `UPDATE jobs SET updated_at = NOW() - INTERVAL '2 minutes' WHERE id = $1`, got.ID); err != nil {
		t.Fatal(err)
	}
	want := errors.New("fence release failed")
	var seenAttempt int
	err = q.RequeueWithBeforeCommit(ctx, got.ID, got.Attempts, func(_ context.Context, _ string, attempt int) error {
		seenAttempt = attempt
		return want
	}, jobs.WithRequeueMinAge(time.Minute))
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if seenAttempt != got.Attempts {
		t.Fatalf("hook attempt = %d, want %d", seenAttempt, got.Attempts)
	}
	detail, err := q.Get(ctx, got.ID)
	if err != nil || detail == nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.Status != jobs.StatusProcessing {
		t.Fatalf("status = %s, want processing after hook error", detail.Status)
	}
}

func TestJobQueue_RequeueTerminal_KeepsAttempts(t *testing.T) {
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
	job, _ := jobs.NewJob(id.New(), "terminal-requeue", nil)
	job.MaxRetries = 1
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	if err := q.Fail(ctx, got.ID, got.Attempts, errors.New("boom")); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	var beforeAttempts int
	var lastError sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT attempts, last_error FROM jobs WHERE id = $1`, got.ID).Scan(&beforeAttempts, &lastError); err != nil {
		t.Fatalf("precondition query: %v", err)
	}
	if beforeAttempts != got.Attempts {
		t.Fatalf("precondition attempts = %d, want %d", beforeAttempts, got.Attempts)
	}
	if err := q.Requeue(ctx, got.ID, beforeAttempts, jobs.WithRequeueTerminal()); err != nil {
		t.Fatalf("Requeue terminal: %v", err)
	}
	detail, err := q.Get(ctx, got.ID)
	if err != nil || detail == nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.Status != jobs.StatusPending || detail.Attempts != beforeAttempts {
		t.Fatalf("after terminal requeue = %+v, want pending attempts=%d", detail, beforeAttempts)
	}
	var afterErr sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT last_error FROM jobs WHERE id = $1`, got.ID).Scan(&afterErr); err != nil {
		t.Fatalf("last_error query: %v", err)
	}
	if afterErr.String != lastError.String {
		t.Fatalf("last_error changed: %q → %q", lastError.String, afterErr.String)
	}
	next, err := q.Dequeue(ctx)
	if err != nil || next == nil || next.ID != got.ID {
		t.Fatalf("Dequeue after terminal requeue = %+v err=%v", next, err)
	}
}

func TestJobQueue_RequeueTerminal_HookErrorKeepsFailed(t *testing.T) {
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
	job, _ := jobs.NewJob(id.New(), "terminal-hook", nil)
	job.MaxRetries = 1
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if err := q.Fail(ctx, got.ID, got.Attempts, errors.New("boom")); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	want := errors.New("fence write failed")
	err = q.RequeueWithBeforeCommit(ctx, got.ID, got.Attempts, func(_ context.Context, _ string, _ int) error {
		return want
	}, jobs.WithRequeueTerminal())
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	detail, err := q.Get(ctx, got.ID)
	if err != nil || detail == nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.Status != jobs.StatusFailed {
		t.Fatalf("status = %s, want failed after hook error", detail.Status)
	}
}

func TestJobQueue_RequeueTerminal_RejectsProcessing(t *testing.T) {
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
	job, _ := jobs.NewJob(id.New(), "terminal-processing", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if err := q.Requeue(ctx, got.ID, got.Attempts, jobs.WithRequeueTerminal()); err == nil || !errors.Is(err, jobs.ErrRequeueNotTerminal) {
		t.Fatalf("err = %v, want ErrRequeueNotTerminal", err)
	}
}

func TestJobQueue_RequeueTerminal_DoneAndCancelled(t *testing.T) {
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

	doneJob, err := jobs.NewJob(id.New(), "terminal-done", nil)
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if err := q.Enqueue(ctx, doneJob); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	got, err := q.Dequeue(ctx)
	if err != nil || got == nil {
		t.Fatalf("Dequeue: %v %#v", err, got)
	}
	if err := q.Complete(ctx, got.ID, got.Attempts); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE jobs SET last_error = $2, run_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, got.ID, "prior-note"); err != nil {
		t.Fatal(err)
	}
	var runAtBefore time.Time
	if err := db.QueryRowContext(ctx, `SELECT run_at FROM jobs WHERE id = $1`, got.ID).Scan(&runAtBefore); err != nil {
		t.Fatal(err)
	}
	if err := q.Requeue(ctx, got.ID, got.Attempts, jobs.WithRequeueTerminal()); err != nil {
		t.Fatalf("Requeue done: %v", err)
	}
	detail, err := q.Get(ctx, got.ID)
	if err != nil || detail == nil || detail.Status != jobs.StatusPending || detail.Attempts != got.Attempts {
		t.Fatalf("after done requeue = %+v err=%v", detail, err)
	}
	if detail.LastError != "prior-note" {
		t.Fatalf("last_error = %q, want prior-note preserved", detail.LastError)
	}
	if !detail.RunAt.After(runAtBefore) {
		t.Fatalf("run_at = %s, want after prior %s", detail.RunAt, runAtBefore)
	}

	cancelJob, err := jobs.NewJob(id.New(), "terminal-cancel", nil)
	if err != nil {
		t.Fatalf("NewJob cancel: %v", err)
	}
	if err := q.Enqueue(ctx, cancelJob); err != nil {
		t.Fatalf("Enqueue cancel: %v", err)
	}
	if err := q.Cancel(ctx, cancelJob.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE jobs SET run_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, cancelJob.ID); err != nil {
		t.Fatal(err)
	}
	var cancelAttempts int
	var cancelRunAtBefore time.Time
	if err := db.QueryRowContext(ctx, `SELECT attempts, run_at FROM jobs WHERE id = $1`, cancelJob.ID).Scan(&cancelAttempts, &cancelRunAtBefore); err != nil {
		t.Fatal(err)
	}
	if err := q.Requeue(ctx, cancelJob.ID, cancelAttempts, jobs.WithRequeueTerminal()); err != nil {
		t.Fatalf("Requeue cancelled: %v", err)
	}
	detail, err = q.Get(ctx, cancelJob.ID)
	if err != nil || detail == nil || detail.Status != jobs.StatusPending {
		t.Fatalf("after cancel requeue = %+v err=%v", detail, err)
	}
	if !detail.RunAt.After(cancelRunAtBefore) {
		t.Fatalf("cancel run_at = %s, want after prior %s", detail.RunAt, cancelRunAtBefore)
	}
}

// Well-formed absent UUID hits FOR UPDATE / sql.ErrNoRows (not the parse
// reject path). Requires SHOPANDA_TEST_DSN.
func TestJobQueue_Requeue_MissingID(t *testing.T) {
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
	missing := id.New()
	if err := q.Requeue(ctx, missing, 1); err == nil || !errors.Is(err, jobs.ErrRequeueNotFound) {
		t.Fatalf("default missing: %v, want ErrRequeueNotFound", err)
	}
	if err := q.Requeue(ctx, missing, 1, jobs.WithRequeueTerminal()); err == nil || !errors.Is(err, jobs.ErrRequeueNotFound) {
		t.Fatalf("terminal missing: %v, want ErrRequeueNotFound", err)
	}
}

// Non-UUID ids are rejected before BeginTx (no DSN). Same sentinel as a miss.
func TestJobQueue_Requeue_InvalidUUIDNoDB(t *testing.T) {
	db, err := sql.Open("pgx", "postgres://invalid/invalid")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	ctx := context.Background()
	if err := q.Requeue(ctx, "missing-id", 1); err == nil || !errors.Is(err, jobs.ErrRequeueNotFound) {
		t.Fatalf("default: %v, want ErrRequeueNotFound", err)
	}
	if err := q.RequeueWithBeforeCommit(ctx, "not-a-uuid", 1, nil, jobs.WithRequeueTerminal()); err == nil || !errors.Is(err, jobs.ErrRequeueNotFound) {
		t.Fatalf("terminal WithBeforeCommit: %v, want ErrRequeueNotFound", err)
	}
}

// Get is on the jobs:requeue / jobs:show / admin get path; non-UUID must
// return (nil, nil), not SQLSTATE 22P02.
func TestJobQueue_Get_InvalidUUIDNoDB(t *testing.T) {
	db, err := sql.Open("pgx", "postgres://invalid/invalid")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	got, err := q.Get(context.Background(), "missing-id")
	if err != nil {
		t.Fatalf("Get: %v, want nil err (no SQLSTATE)", err)
	}
	if got != nil {
		t.Fatalf("Get: got %+v, want nil", got)
	}
}

func TestJobQueue_Retry_InvalidUUIDNoDB(t *testing.T) {
	db, err := sql.Open("pgx", "postgres://invalid/invalid")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	q, err := postgres.NewJobQueue(db)
	if err != nil {
		t.Fatalf("NewJobQueue: %v", err)
	}
	err = q.Retry(context.Background(), "not-a-uuid")
	if err == nil {
		t.Fatal("expected not found")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeNotFound {
		t.Fatalf("err = %v, want not_found", err)
	}
	if strings.Contains(err.Error(), "22P02") || strings.Contains(err.Error(), "invalid input syntax") {
		t.Fatalf("err leaked SQL: %v", err)
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
	if err := q.Fail(ctx, got.ID, got.Attempts, nil); err != nil {
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

	if err := q.Fail(ctx, got.ID, got.Attempts, nil); err != nil {
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

	err = q.Complete(ctx, id.New(), 1)
	if err == nil || !errors.Is(err, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("Complete missing job: err=%v, want ErrOutcomeNotApplied", err)
	}
}

func TestJobQueue_Fail_NotFound(t *testing.T) {
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

	err = q.Fail(ctx, id.New(), 1, errors.New("boom"))
	if err == nil || !errors.Is(err, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("Fail missing job: err=%v, want ErrOutcomeNotApplied", err)
	}
}

func TestJobQueue_Fail_WrongAttempt_LeavesProcessing(t *testing.T) {
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
	job, _ := jobs.NewJob(id.New(), "fail-cas", nil)
	_ = q.Enqueue(ctx, job)
	got, _ := q.Dequeue(ctx)
	if got == nil {
		t.Fatal("expected dequeued job")
	}
	if err := q.Fail(ctx, got.ID, got.Attempts-1, errors.New("stale")); err == nil || !errors.Is(err, jobs.ErrOutcomeNotApplied) {
		t.Fatalf("Fail wrong attempt: %v", err)
	}
	detail, err := q.Get(ctx, got.ID)
	if err != nil || detail == nil {
		t.Fatalf("Get: %v %#v", err, detail)
	}
	if detail.Status != jobs.StatusProcessing {
		t.Fatalf("status = %s, want processing", detail.Status)
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

	if err := q.Fail(ctx, got.ID, got.Attempts, errors.New("boom: upstream timed out")); err != nil {
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

	if err := q.Fail(ctx, got.ID, got.Attempts, nil); err != nil {
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

	byType, err := q.CountsByType(ctx, "test")
	if err != nil {
		t.Fatalf("CountsByType: %v", err)
	}
	if byType[jobs.StatusPending]+byType[jobs.StatusProcessing] != 2 {
		t.Fatalf("CountsByType(test) = %#v, want 2 jobs", byType)
	}
	empty, err := q.CountsByType(ctx, "no.such.type")
	if err != nil {
		t.Fatalf("CountsByType missing: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("CountsByType missing = %#v, want empty", empty)
	}
	if _, err := q.CountsByType(ctx, ""); err == nil {
		t.Fatal("CountsByType empty type: want error")
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
	if err := q.Fail(ctx, got.ID, got.Attempts, errors.New("boom")); err != nil {
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
	if err := q.Fail(ctx, got.ID, got.Attempts, errors.New("boom")); err != nil {
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
