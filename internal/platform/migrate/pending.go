package migrate

import (
	"database/sql"
	"path/filepath"
)

// PendingCount returns how many migration files in dir are not yet applied.
func PendingCount(db *sql.DB, dir string) (int, error) {
	if err := ensureTable(db); err != nil {
		return 0, err
	}

	applied, err := getApplied(db)
	if err != nil {
		return 0, err
	}

	paths, err := listMigrations(dir)
	if err != nil {
		return 0, err
	}

	pending := 0
	for _, path := range paths {
		if !applied[filepath.Base(path)] {
			pending++
		}
	}
	return pending, nil
}
