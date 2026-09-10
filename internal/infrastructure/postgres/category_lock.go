package postgres

import (
	"context"
	"database/sql"
	"fmt"

	domainsearch "github.com/akarso/shopanda/internal/domain/search"
)

// Compile-time check that AdvisoryLock implements domainsearch.CategoryLock.
var _ domainsearch.CategoryLock = (*AdvisoryLock)(nil)

// AdvisoryLock implements domainsearch.CategoryLock using Postgres session
// advisory locks (pg_advisory_xact_lock), which are visible to — and
// mutually exclusive across — every connection to the same database, not
// just goroutines within one process. This is what makes it safe across
// multiple API server instances, unlike an in-process sync.Mutex.
//
// Each Lock call opens its own transaction and holds pg_advisory_xact_lock
// for the transaction's lifetime; the lock releases automatically on
// commit (which is what the returned unlock func does) — this avoids the
// classic session-advisory-lock pitfall of acquiring on one pooled
// connection and trying to release on a different one, since a
// transaction pins a single connection for its whole duration.
// hashtextextended (bigint output, PostgreSQL 11+) hashes the string key
// into the bigint pg_advisory_xact_lock expects; a 64-bit hash makes an
// accidental collision between two different category IDs negligible at
// any realistic catalog size.
type AdvisoryLock struct {
	db *sql.DB
}

// NewAdvisoryLock returns an AdvisoryLock backed by db.
func NewAdvisoryLock(db *sql.DB) (*AdvisoryLock, error) {
	if db == nil {
		return nil, fmt.Errorf("NewAdvisoryLock: nil *sql.DB")
	}
	return &AdvisoryLock{db: db}, nil
}

// Lock implements domainsearch.CategoryLock.
func (l *AdvisoryLock) Lock(ctx context.Context, key string) (func() error, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("advisory_lock: begin: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", key); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("advisory_lock: acquire %q: %w", key, err)
	}
	return func() error {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("advisory_lock: release %q: %w", key, err)
		}
		return nil
	}, nil
}
