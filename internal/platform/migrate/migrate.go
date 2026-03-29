package migrate

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Run applies all pending SQL migrations from dir to the database.
// Migration files must be named like 001_description.sql and contain plain SQL.
// Applied migrations are tracked in the schema_migrations table.
func Run(db *sql.DB, dir string) (int, error) {
	if err := ensureTable(db); err != nil {
		return 0, err
	}

	applied, err := getApplied(db)
	if err != nil {
		return 0, err
	}

	files, err := listMigrations(dir)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, f := range files {
		name := filepath.Base(f)
		if applied[name] {
			continue
		}
		if err := applyMigration(db, f, name); err != nil {
			return count, fmt.Errorf("migrate %s: %w", name, err)
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

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if _, err := tx.Exec(string(content)); err != nil {
		tx.Rollback()
		return fmt.Errorf("exec: %w", err)
	}

	if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES ($1)", name); err != nil {
		tx.Rollback()
		return fmt.Errorf("record version: %w", err)
	}

	return tx.Commit()
}
