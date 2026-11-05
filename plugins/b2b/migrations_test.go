package b2b_test

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/plugins/b2b"
)

func TestLegacyBareAllowlist_MatchesEmbeddedFS(t *testing.T) {
	bases, err := b2b.MigrationBasenamesForTest()
	if err != nil {
		t.Fatal(err)
	}
	present := make(map[string]struct{}, len(bases))
	for _, b := range bases {
		present[b] = struct{}{}
	}
	legacy := b2b.LegacyBareMigrationBasenamesForTest()
	if len(legacy) == 0 {
		t.Fatal("legacy allowlist is empty")
	}
	for _, name := range legacy {
		if _, ok := present[name]; !ok {
			t.Fatalf("legacy allowlist entry %q is not in embedded migrations", name)
		}
	}
}

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
	legacy := b2b.LegacyBareMigrationBasenamesForTest()

	resetB2BSchema(t, db, bases, plugin)

	res, err := b2b.RunMigrationsForTest(db)
	if err != nil {
		t.Fatalf("B2B migrations: %v", err)
	}
	if res.Applied != len(bases) || res.Adopted != 0 {
		t.Fatalf("fresh apply: applied=%d adopted=%d, want applied=%d adopted=0", res.Applied, res.Adopted, len(bases))
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
	for _, base := range legacy {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, base).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("dual-write bare %s missing", base)
		}
	}

	res, err = b2b.RunMigrationsForTest(db)
	if err != nil {
		t.Fatalf("second B2B migrate: %v", err)
	}
	if res.Applied != 0 || res.Adopted != 0 {
		t.Fatalf("idempotent: applied=%d adopted=%d, want 0/0", res.Applied, res.Adopted)
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
	legacy := b2b.LegacyBareMigrationBasenamesForTest()

	if _, err := b2b.RunMigrationsForTest(db); err != nil {
		t.Fatalf("initial B2B migrate: %v", err)
	}

	// Rewrite tracking to bare-only inside one transaction (no parallel gap).
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for _, base := range legacy {
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
		// Restore namespaced keys; bare rows may remain (same as dual-write state).
		for _, base := range legacy {
			version := "plugin:" + plugin + ":" + base
			_, _ = db.Exec(`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING`, version)
		}
	})

	res, err := b2b.RunMigrationsForTest(db)
	if err != nil {
		t.Fatalf("adopt via RunMigrationsForTest: %v", err)
	}
	wantAdopted := len(legacy)
	wantApplied := len(bases) - len(legacy)
	if res.Adopted != wantAdopted || res.Applied != wantApplied {
		t.Fatalf("adopt run: adopted=%d applied=%d, want adopted=%d applied=%d",
			res.Adopted, res.Applied, wantAdopted, wantApplied)
	}
	for _, base := range legacy {
		version := "plugin:" + plugin + ":" + base
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, version).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("adopted %s missing", version)
		}
	}

	res, err = b2b.RunMigrationsForTest(db)
	if err != nil {
		t.Fatalf("second adopt run: %v", err)
	}
	if res.Applied != 0 || res.Adopted != 0 {
		t.Fatalf("second adopt: applied=%d adopted=%d, want 0/0", res.Applied, res.Adopted)
	}
}

func resetB2BSchema(t *testing.T, db *sql.DB, bases []string, plugin string) {
	t.Helper()
	// Shared-DSN tests in this package run with -p 1 in CI; DROP is intentional
	// for a true fresh-apply assertion.
	if _, err := db.Exec(`DROP TABLE IF EXISTS customer_group_prices CASCADE`); err != nil {
		t.Fatalf("drop customer_group_prices: %v", err)
	}
	if _, err := db.Exec(`DROP TABLE IF EXISTS customer_group_members CASCADE`); err != nil {
		t.Fatalf("drop customer_group_members: %v", err)
	}
	if _, err := db.Exec(`DROP TABLE IF EXISTS customer_groups CASCADE`); err != nil {
		t.Fatalf("drop customer_groups: %v", err)
	}
	seen := make(map[string]struct{})
	for _, base := range bases {
		seen[base] = struct{}{}
		seen["plugin:"+plugin+":"+base] = struct{}{}
	}
	for _, base := range b2b.LegacyBareMigrationBasenamesForTest() {
		seen[base] = struct{}{}
		seen["plugin:"+plugin+":"+base] = struct{}{}
	}
	for version := range seen {
		if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version = $1`, version); err != nil {
			t.Fatalf("delete version %s: %v", version, err)
		}
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
