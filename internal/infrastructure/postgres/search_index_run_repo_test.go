package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

func TestSearchIndexRunRepo_CreateGetUpdateFinish(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM search_index_runs") })

	repo, err := postgres.NewSearchIndexRunRepo(db)
	if err != nil {
		t.Fatalf("NewSearchIndexRunRepo: %v", err)
	}
	ctx := context.Background()

	runID := id.New()
	run := domainsearch.Run{
		ID:     runID,
		Scope:  "all",
		Status: domainsearch.RunStatusProcessing,
	}
	if err := repo.Create(ctx, run); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil after Create")
	}
	if got.Scope != "all" || got.Status != domainsearch.RunStatusProcessing {
		t.Errorf("got = %+v, want scope=all status=processing", got)
	}
	if got.TotalCount != 0 || got.ProcessedCount != 0 {
		t.Errorf("got counts = %d/%d, want 0/0", got.ProcessedCount, got.TotalCount)
	}

	if err := repo.UpdateProgress(ctx, runID, 10, 4, 0); err != nil {
		t.Fatalf("UpdateProgress: %v", err)
	}
	got, err = repo.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get after UpdateProgress: %v", err)
	}
	if got.TotalCount != 10 || got.ProcessedCount != 4 {
		t.Errorf("got counts = %d/%d, want 4/10", got.ProcessedCount, got.TotalCount)
	}
	if got.Status != domainsearch.RunStatusProcessing {
		t.Errorf("status changed to %q after UpdateProgress, want still processing", got.Status)
	}

	if err := repo.Finish(ctx, runID, domainsearch.RunStatusCompleted, ""); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	got, err = repo.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get after Finish: %v", err)
	}
	if got.Status != domainsearch.RunStatusCompleted {
		t.Errorf("status = %q, want completed", got.Status)
	}
	if got.FinishedAt.IsZero() {
		t.Error("expected FinishedAt to be set after Finish")
	}
	if got.LastError != "" {
		t.Errorf("LastError = %q, want empty for a completed run", got.LastError)
	}
}

func TestSearchIndexRunRepo_Finish_RecordsLastError(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM search_index_runs") })

	repo, _ := postgres.NewSearchIndexRunRepo(db)
	ctx := context.Background()
	runID := id.New()
	if err := repo.Create(ctx, domainsearch.Run{ID: runID, Scope: "all", Status: domainsearch.RunStatusProcessing}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Finish(ctx, runID, domainsearch.RunStatusFailed, "index down"); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	got, err := repo.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domainsearch.RunStatusFailed || got.LastError != "index down" {
		t.Errorf("got status=%q lastError=%q, want failed/%q", got.Status, got.LastError, "index down")
	}
}

func TestSearchIndexRunRepo_Finish_AlreadyTerminalIsNoop(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM search_index_runs") })

	repo, _ := postgres.NewSearchIndexRunRepo(db)
	ctx := context.Background()
	runID := id.New()
	if err := repo.Create(ctx, domainsearch.Run{ID: runID, Scope: "all", Status: domainsearch.RunStatusProcessing}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// First Finish call wins the race and completes the run.
	if err := repo.Finish(ctx, runID, domainsearch.RunStatusCompleted, ""); err != nil {
		t.Fatalf("first Finish: %v", err)
	}

	// A second, independent caller (e.g. the reconciliation sweep, acting
	// on a stale decision) must not be able to overwrite it.
	err := repo.Finish(ctx, runID, domainsearch.RunStatusFailed, "reconciled: stale decision")
	if !errors.Is(err, domainsearch.ErrRunNotProcessing) {
		t.Fatalf("second Finish err = %v, want ErrRunNotProcessing", err)
	}

	got, getErr := repo.Get(ctx, runID)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if got.Status != domainsearch.RunStatusCompleted {
		t.Errorf("status = %q, want still completed (must not have been clobbered)", got.Status)
	}
	if got.LastError != "" {
		t.Errorf("LastError = %q, want still empty (must not have been clobbered)", got.LastError)
	}
}

func TestSearchIndexRunRepo_Finish_NotFoundErrors(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	repo, _ := postgres.NewSearchIndexRunRepo(db)

	err := repo.Finish(context.Background(), "does-not-exist", domainsearch.RunStatusFailed, "x")
	if err == nil {
		t.Fatal("expected an error finishing a nonexistent run")
	}
	if errors.Is(err, domainsearch.ErrRunNotProcessing) {
		t.Fatal("a genuinely unknown run id must not be reported as ErrRunNotProcessing")
	}
}

func TestSearchIndexRunRepo_Get_NotFoundReturnsNil(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	repo, _ := postgres.NewSearchIndexRunRepo(db)
	got, err := repo.Get(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("Get(unknown id) = %+v, want nil", got)
	}
}

func TestSearchIndexRunRepo_UpdateProgress_NotFoundErrors(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	repo, _ := postgres.NewSearchIndexRunRepo(db)
	if err := repo.UpdateProgress(context.Background(), "does-not-exist", 1, 1, 0); err == nil {
		t.Fatal("expected an error updating a nonexistent run")
	}
}

func TestSearchIndexRunRepo_FindStaleProcessing(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM search_index_runs") })

	repo, _ := postgres.NewSearchIndexRunRepo(db)
	ctx := context.Background()

	staleID, freshID, completedID := id.New(), id.New(), id.New()
	now := time.Now().UTC()

	seed := func(runID string, startedAt time.Time, status domainsearch.RunStatus) {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO search_index_runs (id, scope, scope_params, status, started_at) VALUES ($1, 'all', '{}', $2, $3)`,
			runID, status, startedAt,
		); err != nil {
			t.Fatalf("seed %s: %v", runID, err)
		}
	}
	seed(staleID, now.Add(-3*time.Hour), domainsearch.RunStatusProcessing)
	seed(freshID, now.Add(-5*time.Minute), domainsearch.RunStatusProcessing)
	seed(completedID, now.Add(-3*time.Hour), domainsearch.RunStatusCompleted)

	stale, err := repo.FindStaleProcessing(ctx, now.Add(-1*time.Hour), 500)
	if err != nil {
		t.Fatalf("FindStaleProcessing: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != staleID {
		t.Fatalf("FindStaleProcessing = %+v, want exactly [%s]", stale, staleID)
	}
}

func TestSearchIndexRunRepo_FindStaleProcessing_RespectsLimit(t *testing.T) {
	db := testDB(t)
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM search_index_runs") })

	repo, _ := postgres.NewSearchIndexRunRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 3; i++ {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO search_index_runs (id, scope, scope_params, status, started_at) VALUES ($1, 'all', '{}', 'processing', $2)`,
			id.New(), now.Add(-3*time.Hour),
		); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	stale, err := repo.FindStaleProcessing(ctx, now.Add(-1*time.Hour), 2)
	if err != nil {
		t.Fatalf("FindStaleProcessing: %v", err)
	}
	if len(stale) != 2 {
		t.Fatalf("FindStaleProcessing returned %d runs, want exactly 2 (the limit)", len(stale))
	}
}

func TestNewSearchIndexRunRepo_NilDB(t *testing.T) {
	if _, err := postgres.NewSearchIndexRunRepo(nil); err == nil {
		t.Fatal("expected error for nil *sql.DB")
	}
}
