package b2b_test

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/plugins/b2b"
)

func TestRunMigrationsForTest_RecordsNamespacedVersions(t *testing.T) {
	db := openB2BTestDB(t)
	if _, err := migrate.Run(db, "../../migrations"); err != nil {
		t.Fatalf("core migrations: %v", err)
	}

	bases, err := b2b.MigrationBasenamesForTest()
	if err != nil {
		t.Fatal(err)
	}
	if len(bases) < 4 {
		t.Fatalf("embedded migrations = %d, want at least 4", len(bases))
	}
	plugin := b2b.PluginNameForTest()

	resetB2BSchema(t, db, bases, plugin)

	if err := b2b.RunMigrationsForTest(db); err != nil {
		t.Fatalf("B2B migrations: %v", err)
	}

	for _, base := range bases {
		version := "plugin:" + plugin + ":" + base
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, version).Scan(&n); err != nil {
			t.Fatalf("query %s: %v", version, err)
		}
		if n != 1 {
			t.Fatalf("version %s count = %d, want 1", version, n)
		}
	}
	for _, base := range b2b.LegacyBareMigrationBasenames {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, base).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("dual-write bare %s missing", base)
		}
	}

	if err := b2b.RunMigrationsForTest(db); err != nil {
		t.Fatalf("second B2B migrate: %v", err)
	}
}

func TestRunMigrationsForTest_AdoptsLegacyBareBasenames(t *testing.T) {
	db := openB2BTestDB(t)
	if _, err := migrate.Run(db, "../../migrations"); err != nil {
		t.Fatalf("core migrations: %v", err)
	}
	bases, err := b2b.MigrationBasenamesForTest()
	if err != nil {
		t.Fatal(err)
	}
	plugin := b2b.PluginNameForTest()

	if err := b2b.RunMigrationsForTest(db); err != nil {
		t.Fatalf("initial B2B migrate: %v", err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for _, base := range b2b.LegacyBareMigrationBasenames {
		version := "plugin:" + plugin + ":" + base
		if _, err := tx.Exec(`DELETE FROM schema_migrations WHERE version = $1`, version); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`, base); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, base := range b2b.LegacyBareMigrationBasenames {
			version := "plugin:" + plugin + ":" + base
			_, _ = db.Exec(`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`, version)
		}
	})

	if err := b2b.RunMigrationsForTest(db); err != nil {
		t.Fatalf("adopt via RunMigrationsForTest: %v", err)
	}
	for _, base := range b2b.LegacyBareMigrationBasenames {
		version := "plugin:" + plugin + ":" + base
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, version).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("adopted %s missing", version)
		}
	}

	if err := b2b.RunMigrationsForTest(db); err != nil {
		t.Fatalf("second adopt run: %v", err)
	}
	_ = bases
}

func resetB2BSchema(t *testing.T, db *sql.DB, bases []string, plugin string) {
	t.Helper()
	_, _ = db.Exec(`DROP TABLE IF EXISTS customer_group_prices CASCADE`)
	_, _ = db.Exec(`DROP TABLE IF EXISTS customer_group_members CASCADE`)
	_, _ = db.Exec(`DROP TABLE IF EXISTS customer_groups CASCADE`)
	for _, base := range bases {
		_, _ = db.Exec(`DELETE FROM schema_migrations WHERE version = $1`, base)
		_, _ = db.Exec(`DELETE FROM schema_migrations WHERE version = $1`, "plugin:"+plugin+":"+base)
	}
	for _, base := range b2b.LegacyBareMigrationBasenames {
		_, _ = db.Exec(`DELETE FROM schema_migrations WHERE version = $1`, base)
		_, _ = db.Exec(`DELETE FROM schema_migrations WHERE version = $1`, "plugin:"+plugin+":"+base)
	}
}

func openB2BTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
