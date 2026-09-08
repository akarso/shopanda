package postgres_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

func TestReindexJobFinder_FindReindexJobByRunID(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs WHERE type = 'search.reindex'") })

	ctx := context.Background()
	finder, err := postgres.NewReindexJobFinder(db)
	if err != nil {
		t.Fatalf("NewReindexJobFinder: %v", err)
	}

	runID := id.New()
	jobID := id.New()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO jobs (id, type, payload, status) VALUES ($1, 'search.reindex', jsonb_build_object('run_id', $2::text), 'failed')`,
		jobID, runID,
	); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	got, err := finder.FindReindexJobByRunID(ctx, runID)
	if err != nil {
		t.Fatalf("FindReindexJobByRunID: %v", err)
	}
	if !got.Found || got.JobID != jobID || got.Status != "failed" || !got.Terminal {
		t.Errorf("got = %+v, want Found=true JobID=%s Status=failed Terminal=true", got, jobID)
	}
}

func TestReindexJobFinder_FindReindexJobByRunID_NotFound(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	finder, _ := postgres.NewReindexJobFinder(db)

	got, err := finder.FindReindexJobByRunID(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("FindReindexJobByRunID: %v", err)
	}
	if got.Found {
		t.Errorf("got = %+v, want Found=false", got)
	}
}

func TestReindexJobFinder_FindReindexJobByRunID_NonTerminalStatus(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM jobs WHERE type = 'search.reindex'") })

	ctx := context.Background()
	finder, _ := postgres.NewReindexJobFinder(db)

	runID := id.New()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO jobs (id, type, payload, status) VALUES ($1, 'search.reindex', jsonb_build_object('run_id', $2::text), 'processing')`,
		id.New(), runID,
	); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	got, err := finder.FindReindexJobByRunID(ctx, runID)
	if err != nil {
		t.Fatalf("FindReindexJobByRunID: %v", err)
	}
	if !got.Found || got.Terminal {
		t.Errorf("got = %+v, want Found=true Terminal=false", got)
	}
}

func TestNewReindexJobFinder_NilDB(t *testing.T) {
	if _, err := postgres.NewReindexJobFinder(nil); err == nil {
		t.Fatal("expected error for nil *sql.DB")
	}
}
