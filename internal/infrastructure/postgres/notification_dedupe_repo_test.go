package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/migrate"
)

func ensureNotificationDedupeTable(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() {
		mustExec(t, db, "DELETE FROM notification_dedupe")
	})
}

func TestNotificationDedupeRepo_TryInsert(t *testing.T) {
	db := testDB(t)
	ensureNotificationDedupeTable(t, db)

	repo, err := postgres.NewNotificationDedupeRepo(db)
	if err != nil {
		t.Fatalf("NewNotificationDedupeRepo: %v", err)
	}
	ctx := context.Background()

	inserted, err := repo.TryInsert(ctx, "ord-1", "order_confirmed")
	if err != nil {
		t.Fatalf("TryInsert: %v", err)
	}
	if !inserted {
		t.Fatal("expected first insert to claim the key")
	}

	inserted, err = repo.TryInsert(ctx, "ord-1", "order_confirmed")
	if err != nil {
		t.Fatalf("TryInsert duplicate: %v", err)
	}
	if inserted {
		t.Fatal("expected duplicate insert to report existed")
	}

	// Different kind shares subject but is independent.
	inserted, err = repo.TryInsert(ctx, "ord-1", "other_kind")
	if err != nil || !inserted {
		t.Fatalf("different kind: inserted=%v err=%v", inserted, err)
	}
}

func TestNotificationDedupeRepo_DeleteAllowsRetry(t *testing.T) {
	db := testDB(t)
	ensureNotificationDedupeTable(t, db)

	repo, err := postgres.NewNotificationDedupeRepo(db)
	if err != nil {
		t.Fatalf("NewNotificationDedupeRepo: %v", err)
	}
	ctx := context.Background()

	if _, err := repo.TryInsert(ctx, "ord-2", "order_confirmed"); err != nil {
		t.Fatalf("TryInsert: %v", err)
	}
	if err := repo.Delete(ctx, "ord-2", "order_confirmed"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	inserted, err := repo.TryInsert(ctx, "ord-2", "order_confirmed")
	if err != nil {
		t.Fatalf("TryInsert after delete: %v", err)
	}
	if !inserted {
		t.Fatal("expected insert after delete to succeed")
	}
}

func TestNotificationDedupeRepo_TryInsert_ReclaimsStale(t *testing.T) {
	db := testDB(t)
	ensureNotificationDedupeTable(t, db)

	repo, err := postgres.NewNotificationDedupeRepo(db)
	if err != nil {
		t.Fatalf("NewNotificationDedupeRepo: %v", err)
	}
	ctx := context.Background()

	if _, err := repo.TryInsert(ctx, "ord-stale", "order_confirmed"); err != nil {
		t.Fatalf("TryInsert: %v", err)
	}
	mustExec(t, db, `UPDATE notification_dedupe SET created_at = now() - interval '15 minutes'
		WHERE subject_key = 'ord-stale' AND kind = 'order_confirmed'`)

	inserted, err := repo.TryInsert(ctx, "ord-stale", "order_confirmed")
	if err != nil {
		t.Fatalf("TryInsert reclaim: %v", err)
	}
	if !inserted {
		t.Fatal("expected stale claim to be reclaimed")
	}

	// Fresh claim must still block.
	inserted, err = repo.TryInsert(ctx, "ord-stale", "order_confirmed")
	if err != nil {
		t.Fatalf("TryInsert fresh: %v", err)
	}
	if inserted {
		t.Fatal("expected fresh claim to report existed")
	}
}

func TestNotificationDedupeRepo_TryInsert_DoesNotReclaimFresh(t *testing.T) {
	db := testDB(t)
	ensureNotificationDedupeTable(t, db)

	repo, err := postgres.NewNotificationDedupeRepo(db)
	if err != nil {
		t.Fatalf("NewNotificationDedupeRepo: %v", err)
	}
	ctx := context.Background()

	if _, err := repo.TryInsert(ctx, "ord-fresh", "order_confirmed"); err != nil {
		t.Fatalf("TryInsert: %v", err)
	}
	mustExec(t, db, `UPDATE notification_dedupe SET created_at = now() - interval '1 minute'
		WHERE subject_key = 'ord-fresh' AND kind = 'order_confirmed'`)

	inserted, err := repo.TryInsert(ctx, "ord-fresh", "order_confirmed")
	if err != nil {
		t.Fatalf("TryInsert: %v", err)
	}
	if inserted {
		t.Fatal("expected claim younger than stale threshold to block")
	}
}
