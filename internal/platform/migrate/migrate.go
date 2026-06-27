package migrate

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

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
				return nil, fmt.Errorf("read %s: %w", path, err)
			}
			files[i] = migrationFile{name: filepath.Base(path), content: string(content)}
		}
		return files, nil
	})
}

// RunFS applies pending SQL migrations from fsys under dir.
func RunFS(db *sql.DB, fsys fs.FS, dir string) (int, error) {
	return runMigrations(db, func() ([]migrationFile, error) {
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
				return nil, fmt.Errorf("read %s: %w", e.Name(), err)
			}
			files = append(files, migrationFile{name: e.Name(), content: string(content)})
		}
		sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
		return files, nil
	})
}

type migrationFile struct {
	name    string
	content string
}

func runMigrations(db *sql.DB, list func() ([]migrationFile, error)) (int, error) {
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
		if applied[f.name] {
			continue
		}
		if err := applyMigrationContent(db, f.name, f.content); err != nil {
			return count, fmt.Errorf("migrate %s: %w", f.name, err)
		}
		count++
	}
	return count, nil
}

// ensureTable creates the schema_migrations table if it does not exist.
func ensureTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version  TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("migrate: create tracking table: %w", err)
	}
	return nil
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

// applyMigration reads and executes a single migration file within a transaction.
func applyMigration(db *sql.DB, path, name string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	return applyMigrationContent(db, name, string(content))
}

func applyMigrationContent(db *sql.DB, name, content string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if _, err := tx.Exec(content); err != nil {
		tx.Rollback()
		return fmt.Errorf("exec: %w", err)
	}

	if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", name); err != nil {
		tx.Rollback()
		return fmt.Errorf("record version: %w", err)
	}

	return tx.Commit()
}
