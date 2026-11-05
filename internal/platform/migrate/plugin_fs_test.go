package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var pluginTestSeq atomic.Uint64

func TestPluginVersion_KeyShape(t *testing.T) {
	got := pluginVersion("shopanda/b2b", "001_create_customer_groups.sql")
	want := "plugin:shopanda/b2b:001_create_customer_groups.sql"
	if got != want {
		t.Fatalf("pluginVersion = %q, want %q", got, want)
	}
}

func TestValidatePluginName(t *testing.T) {
	got, err := validatePluginName(" b2b ")
	if err != nil {
		t.Fatalf("trim: %v", err)
	}
	if got != "b2b" {
		t.Fatalf("trimmed name = %q, want b2b", got)
	}
	if _, err := validatePluginName("shopanda/b2b"); err != nil {
		t.Fatalf("slash name: %v", err)
	}
	for _, name := range []string{"", "   ", "\t", "a:b", "B2B", "my plugin", "-bad", "has:colon", "a/", "a-", "a//b"} {
		if _, err := validatePluginName(name); err == nil {
			t.Fatalf("name %q: expected error", name)
		}
	}
}

func TestRunPluginFS_RejectsEmptyPluginName(t *testing.T) {
	ctx := context.Background()
	fsys := fstest.MapFS{
		"migrations/001_noop.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	for _, name := range []string{"", "   ", "\t"} {
		_, err := RunPluginFS(ctx, nil, name, fsys, "migrations")
		if err == nil {
			t.Fatalf("pluginName %q: expected error", name)
		}
	}
}

func TestRunPluginFS_RejectsColonInName(t *testing.T) {
	ctx := context.Background()
	fsys := fstest.MapFS{
		"migrations/001_noop.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	_, err := RunPluginFS(ctx, nil, "a:b", fsys, "migrations")
	if err == nil {
		t.Fatal("expected error for colon in plugin name")
	}
}

func TestRunPluginFS_RejectsMixedWidthPrefixes(t *testing.T) {
	ctx := context.Background()
	fsys := fstest.MapFS{
		"migrations/9_y.sql":  &fstest.MapFile{Data: []byte("SELECT 1;")},
		"migrations/10_x.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	_, err := RunPluginFS(ctx, nil, "hygiene-"+uniqueSuffix(), fsys, "migrations")
	if err == nil {
		t.Fatal("expected mixed-width prefix error")
	}
	if !strings.Contains(err.Error(), "mixed-width") && !strings.Contains(err.Error(), "digit") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPluginFS_RejectsEmptyMigrationsDir(t *testing.T) {
	ctx := context.Background()
	fsys := fstest.MapFS{
		"migrations/README.md": &fstest.MapFile{Data: []byte("no sql")},
	}
	_, err := RunPluginFS(ctx, nil, "empty-"+uniqueSuffix(), fsys, "migrations")
	if err == nil {
		t.Fatal("expected error when no .sql files present")
	}
}

func TestRunPluginFS_RejectsConcurrentIndex(t *testing.T) {
	ctx := context.Background()
	fsys := fstest.MapFS{
		"migrations/001_bad.sql": &fstest.MapFile{Data: []byte("CREATE INDEX CONCURRENTLY ON t (id);")},
	}
	_, err := RunPluginFS(ctx, nil, "ci-"+uniqueSuffix(), fsys, "migrations")
	if err == nil {
		t.Fatal("expected CONCURRENTLY rejection")
	}
	if !strings.Contains(err.Error(), "CONCURRENTLY") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunPluginFS_RejectsTxnControl(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		sql  string
	}{
		{"commit", "CREATE TABLE a_t (id int); COMMIT; CREATE TABLE b_t (id int);"},
		{"end", "CREATE TABLE a_t (id int); END; CREATE TABLE b_t (id int);"},
		{"end_work", "CREATE TABLE a_t (id int); END WORK;"},
		{"end_transaction", "CREATE TABLE a_t (id int); END TRANSACTION;"},
		{"rollback", "CREATE TABLE c_t (id int); ROLLBACK;"},
		{"begin", "BEGIN; CREATE TABLE d_t (id int);"},
		{"start_transaction", "START TRANSACTION; CREATE TABLE e_t (id int);"},
		{"commit_hidden_in_e_string", `SELECT E'\''; COMMIT; SELECT 'x';`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"migrations/001_x.sql": &fstest.MapFile{Data: []byte(tc.sql)},
			}
			_, err := RunPluginFS(ctx, nil, "txn-"+uniqueSuffix(), fsys, "migrations")
			if err == nil {
				t.Fatal("expected transaction-control rejection")
			}
			if !strings.Contains(err.Error(), "BEGIN/COMMIT/END/ROLLBACK") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCheckPluginSQLContent_AllowsPlpgsqlBegin(t *testing.T) {
	sql := `
DO $$
BEGIN
  RAISE NOTICE 'ok';
END $$;
`
	if err := checkPluginSQLContent("001_do.sql", sql); err != nil {
		t.Fatalf("plpgsql BEGIN inside dollar quotes must be allowed: %v", err)
	}
}

func TestCheckPluginSQLContent_AllowsCommitInComment(t *testing.T) {
	sql := "-- COMMIT after deploy\nCREATE TABLE t (id int);\n"
	if err := checkPluginSQLContent("001_ok.sql", sql); err != nil {
		t.Fatalf("COMMIT in comment must be allowed: %v", err)
	}
}

func TestIsCreateExistsRace_Codes(t *testing.T) {
	for _, code := range []string{"23505", "42P07", "42710"} {
		err := &pgconn.PgError{Code: code}
		if !isCreateExistsRace(err) {
			t.Fatalf("code %s should be a create-exists race", code)
		}
	}
	if isCreateExistsRace(&pgconn.PgError{Code: "42P01"}) {
		t.Fatal("undefined_table must not match")
	}
}

func TestRunPluginFS_NilDBAfterValidation(t *testing.T) {
	ctx := context.Background()
	fsys := fstest.MapFS{
		"migrations/001_noop.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	_, err := RunPluginFS(ctx, nil, "ok-plugin", fsys, "migrations")
	if err == nil {
		t.Fatal("expected error for nil database")
	}
	if !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("error = %v, want database is required", err)
	}
}

func TestRunPluginFS_SameBasenameDifferentPlugins(t *testing.T) {
	ctx := context.Background()
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	basename := fmt.Sprintf("001_shared_%s.sql", suffix)
	tableA := fmt.Sprintf("pr1084_a_%s", suffix)
	tableB := fmt.Sprintf("pr1084_b_%s", suffix)
	pluginA := "alpha-" + suffix
	pluginB := "beta-" + suffix

	fsysA := fstest.MapFS{
		"migrations/" + basename: &fstest.MapFile{
			Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", tableA)),
		},
	}
	fsysB := fstest.MapFS{
		"migrations/" + basename: &fstest.MapFile{
			Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", tableB)),
		},
	}

	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + tableA)
		_, _ = db.Exec("DROP TABLE IF EXISTS " + tableB)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1 OR version = $2",
			pluginVersion(pluginA, basename), pluginVersion(pluginB, basename))
	})

	res, err := RunPluginFS(ctx, db, pluginA, fsysA, "migrations")
	if err != nil {
		t.Fatalf("RunPluginFS(%s): %v", pluginA, err)
	}
	if res.Applied != 1 {
		t.Fatalf("RunPluginFS(%s) applied %d, want 1", pluginA, res.Applied)
	}

	res, err = RunPluginFS(ctx, db, pluginB, fsysB, "migrations")
	if err != nil {
		t.Fatalf("RunPluginFS(%s): %v", pluginB, err)
	}
	if res.Applied != 1 {
		t.Fatalf("RunPluginFS(%s) applied %d, want 1 — same basename must be a distinct version", pluginB, res.Applied)
	}

	applied, err := getApplied(db)
	if err != nil {
		t.Fatalf("getApplied: %v", err)
	}
	if !applied[pluginVersion(pluginA, basename)] || !applied[pluginVersion(pluginB, basename)] {
		t.Fatalf("missing namespaced versions")
	}
	if applied[basename] {
		t.Fatalf("bare basename %q must not be recorded for plugin migrations", basename)
	}
}

func TestRunPluginFS_Idempotent(t *testing.T) {
	ctx := context.Background()
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	basename := fmt.Sprintf("001_once_%s.sql", suffix)
	table := fmt.Sprintf("pr1084_once_%s", suffix)
	plugin := "once-" + suffix

	fsys := fstest.MapFS{
		"migrations/" + basename: &fstest.MapFile{
			Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", table)),
		},
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1", pluginVersion(plugin, basename))
	})

	res, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err != nil {
		t.Fatalf("first RunPluginFS: %v", err)
	}
	if res.Applied != 1 {
		t.Fatalf("first apply = %d, want 1", res.Applied)
	}
	res, err = RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err != nil {
		t.Fatalf("second RunPluginFS: %v", err)
	}
	if res.Applied != 0 {
		t.Fatalf("second apply = %d, want 0", res.Applied)
	}
}

func TestRunPluginFS_ConflictSkipsWithoutReexec(t *testing.T) {
	ctx := context.Background()
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	basename := fmt.Sprintf("001_conflict_%s.sql", suffix)
	table := fmt.Sprintf("pr1084_conflict_%s", suffix)
	plugin := "conflict-" + suffix
	version := pluginVersion(plugin, basename)

	if err := ensureTable(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		t.Fatalf("seed version: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1", version)
	})

	// SQL would create the table — must not run when version already claimed.
	fsys := fstest.MapFS{
		"migrations/" + basename: &fstest.MapFile{
			Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", table)),
		},
	}
	res, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err != nil {
		t.Fatalf("RunPluginFS: %v", err)
	}
	if res.Applied != 0 {
		t.Fatalf("applied %d, want 0", res.Applied)
	}
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (
		SELECT 1 FROM information_schema.tables WHERE table_name = $1
	)`, table).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("SQL must not run when version row already exists")
	}
}

func TestRunPluginFS_WaitsForHeldLock(t *testing.T) {
	ctx := context.Background()
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	basename := fmt.Sprintf("001_held_%s.sql", suffix)
	table := fmt.Sprintf("pr1084_held_%s", suffix)
	plugin := "held-" + suffix

	fsys := fstest.MapFS{
		"migrations/" + basename: &fstest.MapFile{
			Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", table)),
		},
	}
	version := pluginVersion(plugin, basename)
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1", version)
	})

	holder, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("holder conn: %v", err)
	}
	defer holder.Close()
	htx, err := holder.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	if _, err := htx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, pluginLockKey(plugin)); err != nil {
		t.Fatalf("holder lock: %v", err)
	}

	type outcome struct {
		n   int
		err error
	}
	started := make(chan struct{})
	done := make(chan outcome, 1)
	go func() {
		close(started)
		res, err := RunPluginFSOptions(context.Background(), db, plugin, fsys, "migrations", PluginFSOptions{
			LockWait: 5 * time.Second,
		})
		done <- outcome{res.Applied, err}
	}()
	<-started

	time.Sleep(200 * time.Millisecond)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, version).Scan(&n); err != nil {
		_ = htx.Rollback()
		t.Fatalf("count while held: %v", err)
	}
	if n != 0 {
		_ = htx.Rollback()
		t.Fatalf("applied %d rows while lock held — advisory lock is not serializing", n)
	}

	if err := htx.Rollback(); err != nil {
		t.Fatalf("holder rollback: %v", err)
	}

	select {
	case out := <-done:
		if out.err != nil {
			t.Fatalf("RunPluginFS after release: %v", out.err)
		}
		if out.n != 1 {
			t.Fatalf("applied %d after release, want 1", out.n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunPluginFS did not finish after lock release")
	}
}

func TestRunPluginFS_LockTimeout(t *testing.T) {
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	basename := fmt.Sprintf("001_timeout_%s.sql", suffix)
	table := fmt.Sprintf("pr1084_timeout_%s", suffix)
	plugin := "timeout-" + suffix

	fsys := fstest.MapFS{
		"migrations/" + basename: &fstest.MapFile{
			Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", table)),
		},
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1", pluginVersion(plugin, basename))
	})

	holder, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("holder conn: %v", err)
	}
	defer holder.Close()
	htx, err := holder.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatalf("holder begin: %v", err)
	}
	defer func() { _ = htx.Rollback() }()
	if _, err := htx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, pluginLockKey(plugin)); err != nil {
		t.Fatalf("holder lock: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	res, err := RunPluginFSOptions(ctx, db, plugin, fsys, "migrations", PluginFSOptions{
		LockWait: time.Hour, // ctx deadline must win
	})
	if err == nil {
		t.Fatal("expected lock wait timeout error")
	}
	if res.Applied != 0 {
		t.Fatalf("applied %d on timeout, want 0", res.Applied)
	}
	if !errors.Is(err, ErrMigrationLockHeld) {
		t.Fatalf("error = %v, want ErrMigrationLockHeld", err)
	}
}

func TestRunPluginFS_PartialFailureKeepsPriorFile(t *testing.T) {
	ctx := context.Background()
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	table := fmt.Sprintf("pr1084_partial_%s", suffix)
	plugin := "partial-" + suffix
	okName := fmt.Sprintf("001_ok_%s.sql", suffix)
	badName := fmt.Sprintf("002_bad_%s.sql", suffix)

	fsys := fstest.MapFS{
		"migrations/" + okName:  &fstest.MapFile{Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", table))},
		"migrations/" + badName: &fstest.MapFile{Data: []byte("THIS IS NOT VALID SQL;;;")},
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1 OR version = $2",
			pluginVersion(plugin, okName), pluginVersion(plugin, badName))
	})

	res, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err == nil {
		t.Fatal("expected apply error")
	}
	if res.Applied != 1 {
		t.Fatalf("applied count = %d, want 1 (first file committed)", res.Applied)
	}
	if !strings.Contains(err.Error(), pluginVersion(plugin, badName)) {
		t.Fatalf("error = %v, want version key for failing file", err)
	}

	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (
		SELECT 1 FROM information_schema.tables WHERE table_name = $1
	)`, table).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("first file table must remain after second file fails")
	}

	// Retry after failure must not hang; second file still bad → still errors, count 0.
	res, err = RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err == nil {
		t.Fatal("expected second apply to still fail on bad file")
	}
	if res.Applied != 0 {
		t.Fatalf("retry applied %d, want 0", res.Applied)
	}
}

func TestRunPluginFS_ReleasesLockBetweenFiles(t *testing.T) {
	// B2B 003/004 contract: ACCESS EXCLUSIVE from file N must not still be held
	// when file N+1 starts. We encode that as: while file 001 holds a table lock,
	// another session can take the advisory lock after 001 commits (between files).
	ctx := context.Background()
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	plugin := "splitlock-" + suffix
	table := fmt.Sprintf("pr1084_split_%s", suffix)
	f1 := fmt.Sprintf("001_lock_%s.sql", suffix)
	f2 := fmt.Sprintf("002_after_%s.sql", suffix)

	fsys := fstest.MapFS{
		"migrations/" + f1: &fstest.MapFile{Data: []byte(fmt.Sprintf(`
CREATE TABLE %s (id INT);
LOCK TABLE %s IN ACCESS EXCLUSIVE MODE;
`, table, table))},
		"migrations/" + f2: &fstest.MapFile{Data: []byte(fmt.Sprintf(`
INSERT INTO %s (id) VALUES (1);
`, table))},
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1 OR version = $2",
			pluginVersion(plugin, f1), pluginVersion(plugin, f2))
	})

	res, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err != nil {
		t.Fatalf("RunPluginFS: %v", err)
	}
	if res.Applied != 2 {
		t.Fatalf("applied %d, want 2", res.Applied)
	}

	// After both files, another session must acquire the same advisory lock immediately
	// (proves we are not holding a batch-scoped lock across files).
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	done := make(chan error, 1)
	go func() {
		_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, pluginLockKey(plugin))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("post-batch lock: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("advisory lock still held after RunPluginFS returned — batch lock leaked across files")
	}
}

func TestRunPluginFS_ConcurrentNoDoubleApply(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()
	basename := fmt.Sprintf("001_concurrent_%s.sql", suffix)
	table := fmt.Sprintf("pr1084_concurrent_%s", suffix)
	plugin := "concurrent-" + suffix
	sqlBody := fmt.Sprintf("CREATE TABLE %s (id INT); SELECT pg_sleep(0.2);", table)

	fsys := fstest.MapFS{
		"migrations/" + basename: &fstest.MapFile{Data: []byte(sqlBody)},
	}

	dbA := openPluginTestDB(t)
	dbB := openPluginTestDB(t)
	t.Cleanup(func() {
		_, _ = dbA.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = dbA.Exec("DELETE FROM schema_migrations WHERE version = $1", pluginVersion(plugin, basename))
	})

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		applied  int
		firstErr error
	)
	run := func(db *sql.DB) {
		defer wg.Done()
		res, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
		mu.Lock()
		defer mu.Unlock()
		if err != nil && firstErr == nil {
			firstErr = err
		}
		applied += res.Applied
	}

	wg.Add(2)
	go run(dbA)
	go run(dbB)
	wg.Wait()

	if firstErr != nil {
		t.Fatalf("concurrent RunPluginFS: %v", firstErr)
	}
	if applied != 1 {
		t.Fatalf("total applied = %d, want 1", applied)
	}
}

func TestRunPluginFS_CtxCancelDuringSQL(t *testing.T) {
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	basename := fmt.Sprintf("001_sleep_%s.sql", suffix)
	plugin := "sleep-" + suffix
	version := pluginVersion(plugin, basename)

	fsys := fstest.MapFS{
		"migrations/" + basename: &fstest.MapFile{Data: []byte("SELECT pg_sleep(10);")},
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1", version)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	res, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err == nil {
		t.Fatal("expected context cancellation during SQL")
	}
	if res.Applied != 0 {
		t.Fatalf("applied %d, want 0 (tx rolled back)", res.Applied)
	}
	var recorded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, version).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 0 {
		t.Fatal("version must not remain after cancelled SQL")
	}
}

func TestRunPluginFS_IgnoresNonSQLAlongsideValid(t *testing.T) {
	ctx := context.Background()
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	basename := fmt.Sprintf("001_sqlonly_%s.sql", suffix)
	table := fmt.Sprintf("pr1084_sqlonly_%s", suffix)
	plugin := "sqlonly-" + suffix

	dir := t.TempDir()
	migDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(migDir, basename), []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", table)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(migDir, "README.md"), []byte("not sql"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1", pluginVersion(plugin, basename))
	})

	res, err := RunPluginFS(ctx, db, plugin, os.DirFS(dir), "migrations")
	if err != nil {
		t.Fatalf("RunPluginFS: %v", err)
	}
	if res.Applied != 1 {
		t.Fatalf("applied %d, want 1", res.Applied)
	}
}

func TestEnsureTable_ConcurrentFreshDatabase(t *testing.T) {
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set")
	}

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		t.Fatal(err)
	}

	dbName := fmt.Sprintf("pr1084_ensure_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE " + dbName); err != nil {
		t.Fatalf("CREATE DATABASE %s: %v (role needs CREATEDB)", dbName, err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, dbName)
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + dbName)
	})

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	u.Path = "/" + dbName
	fresh, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	fresh.SetMaxOpenConns(16)
	if err := fresh.Ping(); err != nil {
		t.Fatal(err)
	}

	const n = 16
	start := make(chan struct{})
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- ensureTable(fresh)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("ensureTable under concurrency: %v", err)
		}
	}
}

func TestRunPluginFS_CommitInSQLRejectedBeforeApply(t *testing.T) {
	// Regression for half-migrated claim: COMMIT mid-file must fail at load
	// time so the version row is never inserted.
	ctx := context.Background()
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	plugin := "commitbug-" + suffix
	basename := "001_x.sql"
	version := pluginVersion(plugin, basename)

	if err := ensureTable(db); err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{
		"migrations/" + basename: &fstest.MapFile{Data: []byte(
			"CREATE TABLE a_t_" + suffix + " (id int); COMMIT; CREATE TABLE b_t_" + suffix + " (id int); SELECT 1/0;",
		)},
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS a_t_" + suffix)
		_, _ = db.Exec("DROP TABLE IF EXISTS b_t_" + suffix)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1", version)
	})

	_, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err == nil {
		t.Fatal("expected rejection")
	}
	if !strings.Contains(err.Error(), "BEGIN/COMMIT/END/ROLLBACK") {
		t.Fatalf("error = %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, version).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("version must not be recorded when COMMIT is rejected at load time")
	}
}

func TestRunPluginFS_EndInSQLRejectedBeforeApply(t *testing.T) {
	// END is a COMMIT synonym; same half-migration hazard as COMMIT.
	ctx := context.Background()
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	plugin := "endbug-" + suffix
	basename := "001_x.sql"
	version := pluginVersion(plugin, basename)
	tableA := "a_end_" + suffix
	tableB := "b_end_" + suffix

	if err := ensureTable(db); err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{
		"migrations/" + basename: &fstest.MapFile{Data: []byte(
			"CREATE TABLE " + tableA + " (id int); END; CREATE TABLE " + tableB + " (id int); SELECT 1/0;",
		)},
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + tableA)
		_, _ = db.Exec("DROP TABLE IF EXISTS " + tableB)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1", version)
	})

	_, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err == nil {
		t.Fatal("expected rejection")
	}
	if !strings.Contains(err.Error(), "BEGIN/COMMIT/END/ROLLBACK") {
		t.Fatalf("error = %v", err)
	}

	res, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err == nil {
		t.Fatal("run 2 must still error — file remains pending")
	}
	if res.Applied != 0 {
		t.Fatalf("run 2 applied %d, want 0", res.Applied)
	}
	var recorded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, version).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 0 {
		t.Fatal("version must not be recorded when END is rejected at load time")
	}
}

func TestRunPluginFS_AdoptBareVersions(t *testing.T) {
	ctx := context.Background()
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	plugin := "adopt-" + suffix
	basename := fmt.Sprintf("001_legacy_%s.sql", suffix)
	table := fmt.Sprintf("pr1085_adopt_%s", suffix)
	version := pluginVersion(plugin, basename)

	if err := ensureTable(db); err != nil {
		t.Fatal(err)
	}
	// Simulate a prior RunFS apply: bare basename recorded + table exists.
	if _, err := db.Exec(fmt.Sprintf("CREATE TABLE %s (id INT)", table)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, basename); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1 OR version = $2", basename, version)
	})

	// SQL would fail if re-executed (table already exists, no IF NOT EXISTS).
	fsys := fstest.MapFS{
		"migrations/" + basename: &fstest.MapFile{
			Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", table)),
		},
	}
	res, err := RunPluginFSOptions(ctx, db, plugin, fsys, "migrations", PluginFSOptions{
		AdoptBareBasenames: []string{basename},
	})
	if err != nil {
		t.Fatalf("AdoptBareBasenames: %v", err)
	}
	if res.Applied != 0 || res.Adopted != 1 {
		t.Fatalf("applied=%d adopted=%d, want applied=0 adopted=1", res.Applied, res.Adopted)
	}

	var recorded bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if !recorded {
		t.Fatalf("missing adopted version %s", version)
	}

	// Idempotent: second call skips.
	res, err = RunPluginFSOptions(ctx, db, plugin, fsys, "migrations", PluginFSOptions{
		AdoptBareBasenames: []string{basename},
	})
	if err != nil {
		t.Fatalf("second adopt: %v", err)
	}
	if res.Applied != 0 || res.Adopted != 0 {
		t.Fatalf("second applied=%d adopted=%d, want 0/0", res.Applied, res.Adopted)
	}
}

func TestRunPluginFS_RejectsUnknownAdoptBasename(t *testing.T) {
	ctx := context.Background()
	fsys := fstest.MapFS{
		"migrations/001_ok.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}
	_, err := RunPluginFSOptions(ctx, nil, "ok-plugin", fsys, "migrations", PluginFSOptions{
		AdoptBareBasenames: []string{"001_missing.sql"},
	})
	if err == nil {
		t.Fatal("expected error for AdoptBareBasenames entry missing from FS")
	}
	if !strings.Contains(err.Error(), "001_missing.sql") {
		t.Fatalf("error = %v, want missing basename", err)
	}
}

func TestRunPluginFS_AdoptAllowlistOnly(t *testing.T) {
	// Bare row for a non-allowlisted basename must not be adopted — SQL runs and fails.
	ctx := context.Background()
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	plugin := "noadopt-" + suffix
	basename := fmt.Sprintf("001_collide_%s.sql", suffix)
	other := fmt.Sprintf("002_listed_%s.sql", suffix)
	table := fmt.Sprintf("pr1085_noadopt_%s", suffix)
	tableOther := fmt.Sprintf("pr1085_listed_%s", suffix)
	version := pluginVersion(plugin, basename)

	if err := ensureTable(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf("CREATE TABLE %s (id INT)", table)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, basename); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = db.Exec("DROP TABLE IF EXISTS " + tableOther)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1 OR version = $2 OR version = $3",
			basename, version, pluginVersion(plugin, other))
	})

	fsys := fstest.MapFS{
		"migrations/" + basename: &fstest.MapFile{
			Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", table)),
		},
		"migrations/" + other: &fstest.MapFile{
			Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", tableOther)),
		},
	}
	_, err := RunPluginFSOptions(ctx, db, plugin, fsys, "migrations", PluginFSOptions{
		AdoptBareBasenames: []string{other}, // collide basename not listed
	})
	if err == nil {
		t.Fatal("expected SQL re-exec failure when bare row is not allowlisted")
	}
}

func TestRunPluginFS_AdoptFlagOffReexecutes(t *testing.T) {
	ctx := context.Background()
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	plugin := "flagoff-" + suffix
	basename := fmt.Sprintf("001_flagoff_%s.sql", suffix)
	table := fmt.Sprintf("pr1085_flagoff_%s", suffix)
	version := pluginVersion(plugin, basename)

	if err := ensureTable(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf("CREATE TABLE %s (id INT)", table)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, basename); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1 OR version = $2", basename, version)
	})

	fsys := fstest.MapFS{
		"migrations/" + basename: &fstest.MapFile{
			Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", table)),
		},
	}
	_, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err == nil {
		t.Fatal("expected failure when AdoptBareBasenames is empty and bare row exists")
	}
}

func TestRunPluginFS_MixedAdoptSkipApply(t *testing.T) {
	ctx := context.Background()
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	plugin := "mixed-" + suffix
	b1 := fmt.Sprintf("001_a_%s.sql", suffix)
	b2 := fmt.Sprintf("002_b_%s.sql", suffix)
	b3 := fmt.Sprintf("003_c_%s.sql", suffix)
	t1 := fmt.Sprintf("pr1085_m1_%s", suffix)
	t2 := fmt.Sprintf("pr1085_m2_%s", suffix)
	t3 := fmt.Sprintf("pr1085_m3_%s", suffix)

	if err := ensureTable(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf("CREATE TABLE %s (id INT); CREATE TABLE %s (id INT)", t1, t2)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES ($1), ($2)`, b1, pluginVersion(plugin, b2)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + t1)
		_, _ = db.Exec("DROP TABLE IF EXISTS " + t2)
		_, _ = db.Exec("DROP TABLE IF EXISTS " + t3)
		for _, v := range []string{b1, b2, b3, pluginVersion(plugin, b1), pluginVersion(plugin, b2), pluginVersion(plugin, b3)} {
			_, _ = db.Exec(`DELETE FROM schema_migrations WHERE version = $1`, v)
		}
	})

	fsys := fstest.MapFS{
		"migrations/" + b1: &fstest.MapFile{Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", t1))},
		"migrations/" + b2: &fstest.MapFile{Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", t2))},
		"migrations/" + b3: &fstest.MapFile{Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", t3))},
	}
	res, err := RunPluginFSOptions(ctx, db, plugin, fsys, "migrations", PluginFSOptions{
		AdoptBareBasenames: []string{b1, b2, b3},
	})
	if err != nil {
		t.Fatalf("mixed: %v", err)
	}
	if res.Adopted != 1 || res.Applied != 1 {
		t.Fatalf("adopted=%d applied=%d, want adopted=1 applied=1 (002 skipped)", res.Adopted, res.Applied)
	}
}

func TestRunPluginFS_FreshApplyDualWritesBare(t *testing.T) {
	ctx := context.Background()
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	plugin := "dual-" + suffix
	basename := fmt.Sprintf("001_dual_%s.sql", suffix)
	table := fmt.Sprintf("pr1085_dual_%s", suffix)
	version := pluginVersion(plugin, basename)

	if err := ensureTable(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1 OR version = $2", basename, version)
	})

	fsys := fstest.MapFS{
		"migrations/" + basename: &fstest.MapFile{
			Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", table)),
		},
	}
	res, err := RunPluginFSOptions(ctx, db, plugin, fsys, "migrations", PluginFSOptions{
		AdoptBareBasenames: []string{basename},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied != 1 {
		t.Fatalf("applied=%d, want 1", res.Applied)
	}
	var bare, namespaced int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, basename).Scan(&bare); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, version).Scan(&namespaced); err != nil {
		t.Fatal(err)
	}
	if bare != 1 || namespaced != 1 {
		t.Fatalf("bare=%d namespaced=%d, want both 1 (rollback-safe dual-write)", bare, namespaced)
	}
}

func TestRunPluginFS_ConcurrentAdopt(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()
	plugin := "concadopt-" + suffix
	basename := fmt.Sprintf("001_ca_%s.sql", suffix)
	table := fmt.Sprintf("pr1085_ca_%s", suffix)
	version := pluginVersion(plugin, basename)

	dbA := openPluginTestDB(t)
	dbB := openPluginTestDB(t)
	if err := ensureTable(dbA); err != nil {
		t.Fatal(err)
	}
	if _, err := dbA.Exec(fmt.Sprintf("CREATE TABLE %s (id INT)", table)); err != nil {
		t.Fatal(err)
	}
	if _, err := dbA.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, basename); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = dbA.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = dbA.Exec("DELETE FROM schema_migrations WHERE version = $1 OR version = $2", basename, version)
	})

	fsys := fstest.MapFS{
		"migrations/" + basename: &fstest.MapFile{
			Data: []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", table)),
		},
	}
	opts := PluginFSOptions{AdoptBareBasenames: []string{basename}}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var adopted, applied int
	var firstErr error
	run := func(db *sql.DB) {
		defer wg.Done()
		res, err := RunPluginFSOptions(ctx, db, plugin, fsys, "migrations", opts)
		mu.Lock()
		defer mu.Unlock()
		if err != nil && firstErr == nil {
			firstErr = err
		}
		adopted += res.Adopted
		applied += res.Applied
	}
	wg.Add(2)
	go run(dbA)
	go run(dbB)
	wg.Wait()
	if firstErr != nil {
		t.Fatal(firstErr)
	}
	if adopted != 1 || applied != 0 {
		t.Fatalf("adopted=%d applied=%d, want 1/0", adopted, applied)
	}
}

func TestApplyPluginFile_ClearsStaleClaimWhenTxnEndedEarly(t *testing.T) {
	// Bypass load-time checks to exercise the runtime guard: END commits the
	// claim, then we must DELETE it so a later RunPluginFS does not skip.
	ctx := context.Background()
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	plugin := "staleclaim-" + suffix
	basename := fmt.Sprintf("001_stale_%s.sql", suffix)
	version := pluginVersion(plugin, basename)
	table := "stale_" + suffix

	if err := ensureTable(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1", version)
	})

	body := fmt.Sprintf("CREATE TABLE %s (id int); END;", table)
	outcome, err := applyPluginFile(ctx, db, plugin, basename, version, body, PluginFSOptions{LockWait: time.Second}, nil)
	if err == nil {
		t.Fatal("expected runtime error when END ends the transaction")
	}
	if outcome != fileSkipped {
		t.Fatal("must not report applied")
	}
	if !strings.Contains(err.Error(), "ended the transaction") {
		t.Fatalf("error = %v", err)
	}

	var recorded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, version).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 0 {
		t.Fatal("stale claim must be cleared so the file stays pending")
	}
}

func TestRunFS_StillRecordsBareBasename(t *testing.T) {
	db := openPluginTestDB(t)
	suffix := uniqueSuffix()
	basename := fmt.Sprintf("runfs_bare_%s.sql", suffix)
	table := fmt.Sprintf("pr1084_runfs_%s", suffix)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, basename), []byte(fmt.Sprintf("CREATE TABLE %s (id INT);", table)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1", basename)
	})

	n, err := RunFS(db, os.DirFS(dir), ".")
	if err != nil {
		t.Fatalf("RunFS: %v", err)
	}
	if n != 1 {
		t.Fatalf("RunFS applied %d, want 1", n)
	}
	applied, err := getApplied(db)
	if err != nil {
		t.Fatalf("getApplied: %v", err)
	}
	if !applied[basename] {
		t.Fatalf("RunFS must record bare basename %q (PR-1085 backfill depends on it)", basename)
	}
}

func openPluginTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func uniqueSuffix() string {
	return fmt.Sprintf("%d_%d", time.Now().UnixNano(), pluginTestSeq.Add(1))
}
