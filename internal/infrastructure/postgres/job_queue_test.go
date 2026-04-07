package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

func ensureJobsTable(t *testing.T, db interface {
	Exec(query string, args ...interface{}) (interface{ RowsAffected() (int64, error) }, error)
}) {
	// testDB + ensureProductsTable already handle migration; we reuse the same pattern.
}

func TestJobQueue_Enqueue(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q := postgres.NewJobQueue(db)
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

	q := postgres.NewJobQueue(db)
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

	q := postgres.NewJobQueue(db)
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

	q := postgres.NewJobQueue(db)
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

	q := postgres.NewJobQueue(db)
	ctx := context.Background()

	job, _ := jobs.NewJob(id.New(), "test", nil)
	_ = q.Enqueue(ctx, job)

	got, _ := q.Dequeue(ctx)

	// Fail the job — should be re-queued since attempts(1) < maxRetries(3).
	if err := q.Fail(ctx, got.ID, nil); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	// Check status is back to pending.
	var status string
	if err := db.QueryRow("SELECT status FROM jobs WHERE id = $1", got.ID).Scan(&status); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if status != "pending" {
		t.Errorf("status = %q, want pending", status)
	}
}

func TestJobQueue_Fail_Permanent(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs") })

	q := postgres.NewJobQueue(db)
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

	q := postgres.NewJobQueue(db)
	ctx := context.Background()

	err := q.Complete(ctx, id.New())
	if err == nil {
		t.Fatal("expected error for non-existent job")
	}
}
