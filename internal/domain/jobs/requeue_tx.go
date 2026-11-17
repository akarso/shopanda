package jobs

import (
	"context"
	"database/sql"
)

type requeueTxKey struct{}

// WithRequeueTx attaches the reclaim transaction for OnRequeued / before-commit
// hooks. Postgres RequeueWithBeforeCommit sets this; hooks should write fence
// state through this *sql.Tx so it commits or rolls back with the jobs row.
func WithRequeueTx(ctx context.Context, tx *sql.Tx) context.Context {
	if ctx == nil || tx == nil {
		return ctx
	}
	return context.WithValue(ctx, requeueTxKey{}, tx)
}

// RequeueTx returns the reclaim *sql.Tx when inside a before-commit hook.
func RequeueTx(ctx context.Context) (*sql.Tx, bool) {
	if ctx == nil {
		return nil, false
	}
	tx, ok := ctx.Value(requeueTxKey{}).(*sql.Tx)
	return tx, ok && tx != nil
}
