package groups_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/akarso/shopanda/internal/domain/customergroup"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/plugins/b2b"
	"github.com/akarso/shopanda/plugins/b2b/groups"

	_ "github.com/lib/pq"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set; skipping integration test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func ensureGroupTables(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := migrate.Run(db, "../../../migrations"); err != nil {
		t.Fatalf("run core migrations: %v", err)
	}
	if err := b2b.RunMigrationsForTest(db); err != nil {
		t.Fatalf("run b2b migrations: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM customer_group_members`)
		_, _ = db.Exec(`DELETE FROM customer_groups`)
	})
}

func TestPostgresRepo_SaveListFind(t *testing.T) {
	db := testDB(t)
	ensureGroupTables(t, db)

	repo, err := groups.NewPostgresRepo(db)
	if err != nil {
		t.Fatalf("NewPostgresRepo: %v", err)
	}

	g, err := customergroup.NewGroup(id.New(), "wholesale", "Wholesale buyers", "Tier 1")
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}
	if err := repo.Save(context.Background(), &g); err != nil {
		t.Fatalf("Save: %v", err)
	}

	found, err := repo.FindByCode(context.Background(), "wholesale")
	if err != nil {
		t.Fatalf("FindByCode: %v", err)
	}
	if found == nil || found.ID != g.ID {
		t.Fatalf("FindByCode = %+v, want id %s", found, g.ID)
	}

	list, err := repo.List(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}
}
