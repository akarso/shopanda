package postgres

import (
	"context"
	"database/sql"

	"github.com/akarso/shopanda/internal/domain/jobs"
)

// WithRequeueTx attaches the reclaim transaction (alias of jobs.WithRequeueTx).
func WithRequeueTx(ctx context.Context, tx *sql.Tx) context.Context {
	return jobs.WithRequeueTx(ctx, tx)
}

// RequeueTx returns the reclaim *sql.Tx (alias of jobs.RequeueTx).
func RequeueTx(ctx context.Context) (*sql.Tx, bool) {
	return jobs.RequeueTx(ctx)
}
