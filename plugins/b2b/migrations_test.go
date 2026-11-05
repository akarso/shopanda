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
	if err := b2b.RunMigrationsForTest(db); err != nil {
		t.Fatalf("B2B migrations: %v", err)
	}

	for _, base := range b2bMigrationBasenames() {
		version := "plugin:shopanda/b2b:" + base
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, version).Scan(&n); err != nil {
			t.Fatalf("query %s: %v", version, err)
		}
		if n != 1 {
			t.Fatalf("version %s count = %d, want 1", version, n)
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
	// Apply once under namespaced keys so tables exist, then rewrite tracking
	// rows to bare basenames (the pre-1085 shape) without dropping tables.
	if err := b2b.RunMigrationsForTest(db); err != nil {
		t.Fatalf("initial B2B migrate: %v", err)
	}
	for _, base := range b2bMigrationBasenames() {
		version := "plugin:shopanda/b2b:" + base
		if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version = $1`, version); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`, base); err != nil {
			t.Fatal(err)
		}
	}

	if err := b2b.RunMigrationsForTest(db); err != nil {
		t.Fatalf("adopt via RunMigrationsForTest: %v", err)
	}
	for _, base := range b2bMigrationBasenames() {
		version := "plugin:shopanda/b2b:" + base
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
}

func b2bMigrationBasenames() []string {
	return []string{
		"001_create_customer_groups.sql",
		"002_create_customer_group_prices.sql",
		"003_allow_zero_group_price_amounts.sql",
		"004_validate_zero_group_price_amount_check.sql",
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
