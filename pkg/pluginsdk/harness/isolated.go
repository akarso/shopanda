package harness

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// IsolatedOptions configures OpenIsolated.
type IsolatedOptions struct {
	// CoreMigrationsDir is the path to core SQL migrations. Empty walks up
	// from the working directory for migrations/001_create_products.sql.
	// Set to "-" to skip core migrations.
	CoreMigrationsDir string
	// PluginName + PluginMigration apply pluginsdk.ApplyMigrationFS after core.
	// Ignored when PluginMigration.FS is nil or PluginName is empty.
	PluginName      string
	PluginMigration pluginsdk.MigrationSpec
}

// Isolated is a Postgres schema with search_path pinned on every connection.
// t.Cleanup drops the schema and closes the pool.
type Isolated struct {
	DB     *sql.DB
	Schema string
}

// OpenIsolated opens SHOPANDA_TEST_DSN into a fresh schema, pins search_path,
// applies core (and optional plugin) migrations, and registers cleanup.
// Skips when SHOPANDA_TEST_DSN is unset.
func OpenIsolated(t testing.TB, opts IsolatedOptions) *Isolated {
	t.Helper()
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set")
	}

	schema, err := randomSchemaName()
	if err != nil {
		t.Fatalf("pluginsdk/harness: schema name: %v", err)
	}

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("pluginsdk/harness: open admin db: %v", err)
	}
	if err := admin.Ping(); err != nil {
		_ = admin.Close()
		t.Fatalf("pluginsdk/harness: ping: %v", err)
	}

	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec("CREATE SCHEMA " + quoted); err != nil {
		_ = admin.Close()
		t.Fatalf("pluginsdk/harness: CREATE SCHEMA %s: %v", schema, err)
	}

	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		_, _ = admin.Exec("DROP SCHEMA " + quoted + " CASCADE")
		_ = admin.Close()
		t.Fatalf("pluginsdk/harness: parse dsn: %v", err)
	}
	cfg.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*cfg,
		stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "SET search_path TO "+quoted)
			return err
		}),
	)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		_, _ = admin.Exec("DROP SCHEMA " + quoted + " CASCADE")
		_ = admin.Close()
		t.Fatalf("pluginsdk/harness: ping isolated: %v", err)
	}

	iso := &Isolated{DB: db, Schema: schema}
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = admin.Exec("DROP SCHEMA " + quoted + " CASCADE")
		_ = admin.Close()
	})

	coreDir := opts.CoreMigrationsDir
	switch coreDir {
	case "-":
		// skip core
	case "":
		discovered, err := discoverMigrationsDir()
		if err != nil {
			t.Fatalf("pluginsdk/harness: core migrations: %v", err)
		}
		coreDir = discovered
		fallthrough
	default:
		if _, err := migrate.Run(db, coreDir); err != nil {
			t.Fatalf("pluginsdk/harness: migrate.Run(%s): %v", coreDir, err)
		}
	}

	if opts.PluginName != "" && opts.PluginMigration.FS != nil {
		if _, err := pluginsdk.ApplyMigrationFS(context.Background(), db, opts.PluginName, opts.PluginMigration); err != nil {
			t.Fatalf("pluginsdk/harness: plugin migrations: %v", err)
		}
	}

	return iso
}

func randomSchemaName() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "h" + hex.EncodeToString(b[:]), nil
}

func discoverMigrationsDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for i := 0; i < 16; i++ {
		candidate := filepath.Join(dir, "migrations")
		if _, err := os.Stat(filepath.Join(candidate, "001_create_products.sql")); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("migrations/001_create_products.sql not found walking from %s", wd)
}
