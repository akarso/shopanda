package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// DefaultPluginLockWait is how long RunPluginFS polls for the per-file
// advisory lock when PluginFSOptions.LockWait is zero and ctx has no deadline.
const DefaultPluginLockWait = 5 * time.Minute

// ErrMigrationLockHeld means another process holds the plugin migrate lock.
// Callers (plugin Init) should treat it as retryable, not a permanent failure.
var ErrMigrationLockHeld = errors.New("migrate: migration lock held by another process")

// PluginFSOptions configures RunPluginFS.
type PluginFSOptions struct {
	// LockWait bounds how long to poll pg_try_advisory_xact_lock per file.
	// Zero means DefaultPluginLockWait. A ctx deadline shorter than LockWait
	// still wins. Ctx also cancels SQL execution after the lock is acquired —
	// do not pass a short ctx "only for the lock" around long DDL.
	LockWait time.Duration
}

// pluginNamePattern is the permanent contract for names embedded in
// schema_migrations keys (plugin:{name}:{basename}).
var pluginNamePattern = regexp.MustCompile(`^[a-z0-9]+([_/-][a-z0-9]+)*$`)

var concurrentIndexPattern = regexp.MustCompile(`(?is)\bcreate\s+(unique\s+)?index\s+concurrently\b`)

// Run applies all pending SQL migrations from dir to the database.
func Run(db *sql.DB, dir string) (int, error) {
	return runMigrations(db, func() ([]migrationFile, error) {
		paths, err := listMigrations(dir)
		if err != nil {
			return nil, err
		}
		files := make([]migrationFile, len(paths))
		for i, path := range paths {
			content, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("migrate: read %s: %w", path, err)
			}
			files[i] = migrationFile{name: filepath.Base(path), content: string(content)}
		}
		return files, nil
	}, identityVersion)
}

// RunFS applies pending SQL migrations from fsys under dir.
// Versions are recorded as the bare SQL basename (unchanged; PR-1085 backfill
// depends on this for historical B2B rows).
func RunFS(db *sql.DB, fsys fs.FS, dir string) (int, error) {
	return runMigrations(db, listFSMigrations(fsys, dir), identityVersion)
}

// RunPluginFS applies pending SQL migrations from fsys under dir for a named
// plugin. Versions are recorded as plugin:{pluginName}:{basename}.
//
// Each pending file runs in its own Read Committed transaction on a connection
// from db: the transaction takes pg_advisory_xact_lock, claims the version row
// (INSERT … ON CONFLICT DO NOTHING), runs the SQL only if the claim succeeded,
// then commits. That is safe under PgBouncer transaction pooling, keeps
// ACCESS EXCLUSIVE (and similar) scoped to one file — so splits like B2B
// 003/004 still release locks between files — and prevents double-apply.
//
// Plugin SQL must not use CREATE INDEX CONCURRENTLY, and must not contain
// top-level BEGIN/COMMIT/ROLLBACK (those would end the migrate transaction
// early and drop the advisory lock). Ctx cancels lock wait and statement
// execution; LockWait (default 5m) only bounds the lock poll when ctx has
// no deadline.
func RunPluginFS(ctx context.Context, db *sql.DB, pluginName string, fsys fs.FS, dir string) (int, error) {
	return RunPluginFSOptions(ctx, db, pluginName, fsys, dir, PluginFSOptions{})
}

// RunPluginFSOptions is RunPluginFS with explicit options (tests shorten LockWait).
func RunPluginFSOptions(ctx context.Context, db *sql.DB, pluginName string, fsys fs.FS, dir string, opts PluginFSOptions) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	name, err := validatePluginName(pluginName)
	if err != nil {
		return 0, err
	}

	files, err := loadPluginMigrations(fsys, dir)
	if err != nil {
		return 0, fmt.Errorf("migrate: plugin %s: %w", name, err)
	}

	if db == nil {
		return 0, fmt.Errorf("migrate: plugin %s: database is required", name)
	}

	if err := ensureTable(db); err != nil {
		return 0, err
	}

	lockWait := opts.LockWait
	if lockWait <= 0 {
		lockWait = DefaultPluginLockWait
	}

	count := 0
	for _, f := range files {
		version := pluginVersion(name, f.name)
		applied, applyErr := applyPluginFile(ctx, db, name, version, f.content, lockWait)
		if applyErr != nil {
			return count, applyErr
		}
		if applied {
			count++
		}
	}
	return count, nil
}

func applyPluginFile(ctx context.Context, db *sql.DB, pluginName, version, content string, lockWait time.Duration) (applied bool, err error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, fmt.Errorf("migrate: plugin %s: begin: %w", pluginName, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	lockKey := pluginLockKey(pluginName)
	if err = waitPluginAdvisoryLock(ctx, tx, lockKey, lockWait); err != nil {
		return false, fmt.Errorf("migrate: plugin %s: %w", pluginName, err)
	}

	var recorded sql.NullString
	scanErr := tx.QueryRowContext(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1)
		 ON CONFLICT (version) DO NOTHING
		 RETURNING version`, version).Scan(&recorded)
	if scanErr == sql.ErrNoRows {
		if rbErr := tx.Rollback(); rbErr != nil {
			return false, fmt.Errorf("migrate: plugin %s: rollback skip %s: %w", pluginName, version, rbErr)
		}
		err = nil // suppress deferred rollback
		return false, nil
	}
	if scanErr != nil {
		return false, fmt.Errorf("migrate: plugin %s: claim %s: %w", pluginName, version, scanErr)
	}

	if _, execErr := tx.ExecContext(ctx, content); execErr != nil {
		return false, fmt.Errorf("migrate: plugin %s: apply %s: %w", pluginName, version, execErr)
	}

	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("migrate: plugin %s: commit %s: %w", pluginName, version, err)
	}
	return true, nil
}

func pluginLockKey(pluginName string) string {
	return "shopanda:migrate:plugin:" + pluginName
}

// pluginVersion is the schema_migrations key for a plugin SQL basename.
func pluginVersion(pluginName, basename string) string {
	return "plugin:" + pluginName + ":" + basename
}

func identityVersion(basename string) string { return basename }

func validatePluginName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("migrate: plugin name is required")
	}
	if !pluginNamePattern.MatchString(name) {
		return "", fmt.Errorf("migrate: plugin name %q must match %s", name, pluginNamePattern.String())
	}
	return name, nil
}

func waitPluginAdvisoryLock(ctx context.Context, tx *sql.Tx, key string, lockWait time.Duration) error {
	deadline := time.Now().Add(lockWait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	for {
		var got bool
		if err := tx.QueryRowContext(ctx,
			`SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))`, key).Scan(&got); err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return fmt.Errorf("%w (%q): %w", ErrMigrationLockHeld, key, err)
			}
			return fmt.Errorf("lock acquire %q: %w", key, err)
		}
		if got {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w (%q): %w", ErrMigrationLockHeld, key, err)
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("%w (%q)", ErrMigrationLockHeld, key)
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w (%q): %w", ErrMigrationLockHeld, key, ctx.Err())
		case <-timer.C:
		}
	}
}

func listFSMigrations(fsys fs.FS, dir string) func() ([]migrationFile, error) {
	return func() ([]migrationFile, error) {
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			return nil, fmt.Errorf("migrate: read dir %s: %w", dir, err)
		}
		var files []migrationFile
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
				continue
			}
			content, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
			if err != nil {
				return nil, fmt.Errorf("migrate: read %s: %w", path.Join(dir, e.Name()), err)
			}
			files = append(files, migrationFile{name: e.Name(), content: string(content)})
		}
		sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
		return files, nil
	}
}

// loadPluginMigrations lists .sql files under dir, enforces numeric-prefix
// hygiene (no core allowlist), rejects empty sets / ':' basenames / CONCURRENTLY.
func loadPluginMigrations(fsys fs.FS, dir string) ([]migrationFile, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if strings.Contains(e.Name(), ":") {
			return nil, fmt.Errorf("migration basename %q must not contain ':'", e.Name())
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no .sql migrations in %s", dir)
	}
	if err := checkPluginFilenameHygiene(names); err != nil {
		return nil, err
	}
	sort.Strings(names)

	files := make([]migrationFile, 0, len(names))
	for _, name := range names {
		content, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path.Join(dir, name), err)
		}
		if err := checkPluginSQLContent(name, string(content)); err != nil {
			return nil, err
		}
		files = append(files, migrationFile{name: name, content: string(content)})
	}
	return files, nil
}

func checkPluginSQLContent(name, content string) error {
	if concurrentIndexPattern.MatchString(content) {
		return fmt.Errorf("%q: CREATE INDEX CONCURRENTLY is not allowed in plugin migrations (each file runs inside a transaction)", name)
	}
	return nil
}

type migrationFile struct {
	name    string
	content string
}

func runMigrations(db *sql.DB, list func() ([]migrationFile, error), versionOf func(basename string) string) (int, error) {
	if err := ensureTable(db); err != nil {
		return 0, err
	}

	applied, err := getApplied(db)
	if err != nil {
		return 0, err
	}

	files, err := list()
	if err != nil {
		return 0, err
	}

	count := 0
	for _, f := range files {
		version := versionOf(f.name)
		if applied[version] {
			continue
		}
		if err := applyMigrationContent(db, version, f.content); err != nil {
			return count, fmt.Errorf("migrate %s: %w", version, err)
		}
		count++
	}
	return count, nil
}

// ensureTable creates the schema_migrations table if it does not exist.
// Retries once on unique_violation (23505) from concurrent CREATE TABLE races.
func ensureTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version  TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil && isUniqueViolation(err) {
		_, err = db.Exec(`
			CREATE TABLE IF NOT EXISTS schema_migrations (
				version  TEXT PRIMARY KEY,
				applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
			)
		`)
	}
	if err != nil {
		return fmt.Errorf("migrate: create tracking table: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// getApplied returns a set of already-applied migration filenames.
func getApplied(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query("SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("migrate: query applied: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("migrate: scan version: %w", err)
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// listMigrations returns sorted .sql file paths from the given directory.
func listMigrations(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("migrate: read dir %s: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

func applyMigrationContent(db *sql.DB, name, content string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if _, err := tx.Exec(content); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("exec: %w", err)
	}

	if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", name); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record version: %w", err)
	}

	return tx.Commit()
}
