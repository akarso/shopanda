package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

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

	n, err := RunPluginFS(ctx, db, pluginA, fsysA, "migrations")
	if err != nil {
		t.Fatalf("RunPluginFS(%s): %v", pluginA, err)
	}
	if n != 1 {
		t.Fatalf("RunPluginFS(%s) applied %d, want 1", pluginA, n)
	}

	n, err = RunPluginFS(ctx, db, pluginB, fsysB, "migrations")
	if err != nil {
		t.Fatalf("RunPluginFS(%s): %v", pluginB, err)
	}
	if n != 1 {
		t.Fatalf("RunPluginFS(%s) applied %d, want 1 — same basename must be a distinct version", pluginB, n)
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

	n, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err != nil {
		t.Fatalf("first RunPluginFS: %v", err)
	}
	if n != 1 {
		t.Fatalf("first apply = %d, want 1", n)
	}
	n, err = RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err != nil {
		t.Fatalf("second RunPluginFS: %v", err)
	}
	if n != 0 {
		t.Fatalf("second apply = %d, want 0", n)
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
	n, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err != nil {
		t.Fatalf("RunPluginFS: %v", err)
	}
	if n != 0 {
		t.Fatalf("applied %d, want 0", n)
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
		n, err := RunPluginFSOptions(context.Background(), db, plugin, fsys, "migrations", PluginFSOptions{
			LockWait: 5 * time.Second,
		})
		done <- outcome{n, err}
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
	n, err := RunPluginFSOptions(ctx, db, plugin, fsys, "migrations", PluginFSOptions{
		LockWait: time.Hour, // ctx deadline must win
	})
	if err == nil {
		t.Fatal("expected lock wait timeout error")
	}
	if n != 0 {
		t.Fatalf("applied %d on timeout, want 0", n)
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

	n, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err == nil {
		t.Fatal("expected apply error")
	}
	if n != 1 {
		t.Fatalf("applied count = %d, want 1 (first file committed)", n)
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
	n, err = RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err == nil {
		t.Fatal("expected second apply to still fail on bad file")
	}
	if n != 0 {
		t.Fatalf("retry applied %d, want 0", n)
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

	n, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err != nil {
		t.Fatalf("RunPluginFS: %v", err)
	}
	if n != 2 {
		t.Fatalf("applied %d, want 2", n)
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
		n, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
		mu.Lock()
		defer mu.Unlock()
		if err != nil && firstErr == nil {
			firstErr = err
		}
		applied += n
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
	n, err := RunPluginFS(ctx, db, plugin, fsys, "migrations")
	if err == nil {
		t.Fatal("expected context cancellation during SQL")
	}
	if n != 0 {
		t.Fatalf("applied %d, want 0 (tx rolled back)", n)
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

	n, err := RunPluginFS(ctx, db, plugin, os.DirFS(dir), "migrations")
	if err != nil {
		t.Fatalf("RunPluginFS: %v", err)
	}
	if n != 1 {
		t.Fatalf("applied %d, want 1", n)
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
