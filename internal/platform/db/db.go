package db

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

// Open creates a new database connection pool using the provided DSN.
// The DSN should be a Postgres connection string:
//
//	postgres://user:pass@host:port/dbname?sslmode=disable
func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return db, nil
}
