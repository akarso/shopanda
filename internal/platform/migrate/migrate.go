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
// Long-running callers may retry with backoff. Plugin Init typically fails
// boot on this error — operators should free the stuck peer (see RUNBOOK).
var ErrMigrationLockHeld = errors.New("migrate: migration lock held by another process")

// PluginFSResult counts outcomes from RunPluginFS / RunPluginFSOptions.
type PluginFSResult struct {
	Applied int // files whose SQL ran
	Adopted int // legacy bare→namespaced remaps (no SQL)
}

// PluginFSOptions configures RunPluginFS.
type PluginFSOptions struct {
	// LockWait bounds how long to poll pg_try_advisory_xact_lock per file.
	// Zero means DefaultPluginLockWait. A ctx deadline shorter than LockWait
	// still wins. Ctx also cancels SQL execution after the lock is acquired —
	// do not pass a short ctx "only for the lock" around long DDL.
	LockWait time.Duration

	// AdoptBareBasenames lists legacy RunFS basenames that may be remapped to
	// plugin:{name}:{basename} without re-executing SQL. Only these names are
	// consulted; files added later never adopt from bare keys (avoids colliding
	// with core or other RunFS users). Empty means never adopt.
	AdoptBareBasenames []string
}

// pluginNamePattern is the permanent contract for names embedded in
// schema_migrations keys (plugin:{name}:{basename}).
var pluginNamePattern = regexp.MustCompile(`^[a-z0-9]+([_/-][a-z0-9]+)*$`)

// concurrentDDLPattern rejects CONCURRENTLY forms that cannot run inside a
// transaction. Mention in a comment still matches — prefer rewriting the comment.
var concurrentDDLPattern = regexp.MustCompile(`(?is)\b(create\s+(unique\s+)?index|drop\s+index|reindex(\s+\w+)?)\s+concurrently\b`)

// txnControlStatementPattern matches top-level transaction-control keywords
// after comments and quoted strings are stripped (so plpgsql BEGIN/END inside
// $$ is ignored). Bare END is a COMMIT synonym in Postgres; BEGIN ATOMIC …
// END function bodies are rejected — write those as AS $$ … $$ instead.
var txnControlStatementPattern = regexp.MustCompile(`(?is)(?:^|;)\s*(commit(\s+and\s+chain)?|rollback(\s+and\s+chain)?|abort|begin(\s+(work|transaction|atomic))?|start\s+transaction|end(\s+(work|transaction))?)\b`)

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
// Versions are recorded as the bare SQL basename. Those bare keys are a
// permanent contract: AdoptBareBasenames reads them and must keep working.
func RunFS(db *sql.DB, fsys fs.FS, dir string) (int, error) {
	return runMigrations(db, listFSMigrations(fsys, dir), identityVersion)
}

// RunPluginFS applies pending SQL migrations from fsys under dir for a named
// plugin. Versions are recorded as plugin:{pluginName}:{basename}.
//
// Each pending file runs in its own Read Committed transaction on a connection
// from db: the transaction takes pg_advisory_xact_lock (key is per plugin,
// acquired once per file transaction), claims the version row, optionally
// adopts a legacy bare basename from AdoptBareBasenames, otherwise runs SQL,
// then commits. That is safe under PgBouncer transaction pooling and keeps
// ACCESS EXCLUSIVE scoped to one file (B2B 003/004).
//
// Plugin SQL must not use CREATE INDEX CONCURRENTLY, and must not contain
// top-level BEGIN/COMMIT/END/ROLLBACK. Ctx cancels lock wait and statement
// execution; LockWait (default 5m) only bounds the lock poll when ctx has
// no deadline.
func RunPluginFS(ctx context.Context, db *sql.DB, pluginName string, fsys fs.FS, dir string) (PluginFSResult, error) {
	return RunPluginFSOptions(ctx, db, pluginName, fsys, dir, PluginFSOptions{})
}

// RunPluginFSOptions is RunPluginFS with explicit options (tests shorten LockWait).
func RunPluginFSOptions(ctx context.Context, db *sql.DB, pluginName string, fsys fs.FS, dir string, opts PluginFSOptions) (PluginFSResult, error) {
	var result PluginFSResult
	if ctx == nil {
		ctx = context.Background()
	}

	name, err := validatePluginName(pluginName)
	if err != nil {
		return result, err
	}

	files, err := loadPluginMigrations(fsys, dir)
	if err != nil {
		return result, fmt.Errorf("migrate: plugin %s: %w", name, err)
	}

	if err := validateAdoptBareBasenames(opts.AdoptBareBasenames, files); err != nil {
		return result, fmt.Errorf("migrate: plugin %s: %w", name, err)
	}

	if db == nil {
		return result, fmt.Errorf("migrate: plugin %s: database is required", name)
	}

	if err := ensureTable(db); err != nil {
		return result, err
	}

	adoptSet := bareAdoptSet(opts.AdoptBareBasenames)
	for _, f := range files {
		version := pluginVersion(name, f.name)
		outcome, applyErr := applyPluginFile(ctx, db, name, f.name, version, f.content, opts, adoptSet)
		if applyErr != nil {
			return result, applyErr
		}
		switch outcome {
		case fileApplied:
			result.Applied++
		case fileAdopted:
			result.Adopted++
		}
	}
	return result, nil
}

type fileOutcome int

const (
	fileSkipped fileOutcome = iota
	fileApplied
	fileAdopted
)

func bareAdoptSet(names []string) map[string]struct{} {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return set
}

// validateAdoptBareBasenames requires every allowlisted basename to exist in
// the loaded plugin FS so a typo cannot silently disable adopt for that file.
func validateAdoptBareBasenames(allowlist []string, files []migrationFile) error {
	if len(allowlist) == 0 {
		return nil
	}
	present := make(map[string]struct{}, len(files))
	for _, f := range files {
		present[f.name] = struct{}{}
	}
	var missing []string
	for _, name := range allowlist {
		if _, ok := present[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("AdoptBareBasenames entries not found in plugin migrations: %s", strings.Join(missing, ", "))
}

func applyPluginFile(ctx context.Context, db *sql.DB, pluginName, basename, version, content string, opts PluginFSOptions, adoptSet map[string]struct{}) (outcome fileOutcome, err error) {
	lockWait := opts.LockWait
	if lockWait <= 0 {
		lockWait = DefaultPluginLockWait
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fileSkipped, fmt.Errorf("migrate: plugin %s: begin: %w", pluginName, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	lockKey := pluginLockKey(pluginName)
	if err = waitPluginAdvisoryLock(ctx, tx, lockKey, lockWait); err != nil {
		return fileSkipped, fmt.Errorf("migrate: plugin %s: %w", pluginName, err)
	}

	var recorded sql.NullString
	scanErr := tx.QueryRowContext(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1)
		 ON CONFLICT (version) DO NOTHING
		 RETURNING version`, version).Scan(&recorded)
	if scanErr == sql.ErrNoRows {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fileSkipped, fmt.Errorf("migrate: plugin %s: rollback skip %s: %w", pluginName, version, rbErr)
		}
		err = nil
		return fileSkipped, nil
	}
	if scanErr != nil {
		return fileSkipped, fmt.Errorf("migrate: plugin %s: claim %s: %w", pluginName, version, scanErr)
	}

	// Claim succeeded. If this basename is on the frozen adopt allowlist and a
	// legacy bare row exists, keep the claim and skip SQL.
	if _, allow := adoptSet[basename]; allow {
		var bareExists bool
		if bareErr := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, basename).Scan(&bareExists); bareErr != nil {
			return fileSkipped, fmt.Errorf("migrate: plugin %s: check bare %s: %w", pluginName, basename, bareErr)
		}
		if bareExists {
			if err = tx.Commit(); err != nil {
				return fileSkipped, fmt.Errorf("migrate: plugin %s: commit adopt %s: %w", pluginName, version, err)
			}
			return fileAdopted, nil
		}
	}

	_, execErr := tx.ExecContext(ctx, content)

	// Defense in depth: a top-level COMMIT/END/ROLLBACK inside the file ends the
	// server transaction early (claim may already be durable) while database/sql
	// still believes the Tx is open. Detect that, clear the stale claim, and
	// fail so the next boot re-attempts instead of skipping a half-migration.
	var inTx bool
	checkErr := tx.QueryRowContext(ctx, `SELECT pg_current_xact_id_if_assigned() IS NOT NULL`).Scan(&inTx)
	stillInTx := checkErr == nil && inTx
	if stillInTx {
		if execErr != nil {
			return fileSkipped, fmt.Errorf("migrate: plugin %s: apply %s: %w", pluginName, version, execErr)
		}
		// Dual-write bare key for allowlisted legacy files so rolling the binary
		// back to RunFS still sees them as applied.
		if _, allow := adoptSet[basename]; allow {
			if _, bareErr := tx.ExecContext(ctx,
				`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT (version) DO NOTHING`, basename); bareErr != nil {
				return fileSkipped, fmt.Errorf("migrate: plugin %s: dual-write bare %s: %w", pluginName, basename, bareErr)
			}
		}
		if err = tx.Commit(); err != nil {
			return fileSkipped, fmt.Errorf("migrate: plugin %s: commit %s: %w", pluginName, version, err)
		}
		return fileApplied, nil
	}

	if execErr != nil && checkErr != nil {
		return fileSkipped, fmt.Errorf("migrate: plugin %s: apply %s: %w", pluginName, version, execErr)
	}

	_ = tx.Rollback()
	err = nil
	clearErr := clearPluginVersionClaim(ctx, db, version)
	msg := fmt.Sprintf("migrate: plugin %s: apply %s: migration SQL ended the transaction (top-level BEGIN/COMMIT/END/ROLLBACK are not allowed)", pluginName, version)
	if execErr != nil {
		msg = fmt.Sprintf("%s: %v", msg, execErr)
	}
	if clearErr != nil {
		return fileSkipped, fmt.Errorf("%s; also failed to clear stale claim: %w", msg, clearErr)
	}
	return fileSkipped, errors.New(msg)
}

func clearPluginVersionClaim(ctx context.Context, db *sql.DB, version string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = $1`, version)
	return err
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
	if concurrentDDLPattern.MatchString(content) {
		return fmt.Errorf("%q: CONCURRENTLY DDL is not allowed in plugin migrations (each file runs inside a transaction)", name)
	}
	stripped := stripSQLNoise(content)
	if txnControlStatementPattern.MatchString(stripped) {
		return fmt.Errorf("%q: top-level BEGIN/COMMIT/END/ROLLBACK/START TRANSACTION is not allowed in plugin migrations (each file runs inside a migrate transaction; write BEGIN ATOMIC function bodies as AS $$ … $$)", name)
	}
	return nil
}

// stripSQLNoise removes -- and /* */ comments plus single-quoted (including
// E'…' with backslash escapes) and dollar-quoted string literals so content
// checks see only statement text.
func stripSQLNoise(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		// Line comment
		if s[i] == '-' && i+1 < len(s) && s[i+1] == '-' {
			i += 2
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		// Block comment
		if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			if i+1 < len(s) {
				i += 2
			}
			continue
		}
		// E'…' / e'…' string with backslash escapes
		if (s[i] == 'E' || s[i] == 'e') && i+1 < len(s) && s[i+1] == '\'' {
			i += 2
			for i < len(s) {
				if s[i] == '\\' && i+1 < len(s) {
					i += 2
					continue
				}
				if s[i] == '\'' {
					if i+1 < len(s) && s[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			b.WriteByte(' ')
			continue
		}
		// Single-quoted string
		if s[i] == '\'' {
			i++
			for i < len(s) {
				if s[i] == '\'' {
					if i+1 < len(s) && s[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			b.WriteByte(' ')
			continue
		}
		// Dollar-quoted string: $$…$$ or $tag$…$tag$
		if s[i] == '$' {
			tag, ok := readDollarTag(s, i)
			if ok {
				i += len(tag)
				end := strings.Index(s[i:], tag)
				if end < 0 {
					break
				}
				i += end + len(tag)
				b.WriteByte(' ')
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func readDollarTag(s string, start int) (string, bool) {
	if start >= len(s) || s[start] != '$' {
		return "", false
	}
	j := start + 1
	for j < len(s) {
		c := s[j]
		if c == '$' {
			return s[start : j+1], true
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			j++
			continue
		}
		return "", false
	}
	return "", false
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
// Concurrent CREATE TABLE IF NOT EXISTS races surface as 42P07 (duplicate
// relation), 42710 (duplicate type), or occasionally 23505 — not only 23505.
// Retry once; the second IF NOT EXISTS sees the winner's table.
func ensureTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version  TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil && isCreateExistsRace(err) {
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

func isCreateExistsRace(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Code {
	case "23505", // unique_violation (catalog race)
		"42P07", // duplicate_table
		"42710": // duplicate_object (e.g. composite type)
		return true
	default:
		return false
	}
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
