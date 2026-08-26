package db

import (
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Open creates a new database connection pool using the provided DSN.
// The DSN should be a Postgres connection string:
//
//	postgres://user:pass@host:port/dbname?sslmode=disable
//
// Uses pgx (registered under the driver name "pgx" by the stdlib bridge
// import above), not lib/pq — lib/pq is in maintenance mode; pgx is the
// actively maintained driver. This still goes through database/sql, not
// pgx's own native API, so every repository's *sql.DB-based code is
// unaffected.
func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return db, nil
}
