package postgres

import (
	"context"
	"database/sql"
)

// SetExtensionValuePurgeBatchSizeForTest lowers the purge batch size for tests.
func SetExtensionValuePurgeBatchSizeForTest(n int) (restore func()) {
	prev := extensionValuePurgeBatchSize
	extensionValuePurgeBatchSize = n
	return func() { extensionValuePurgeBatchSize = prev }
}

// ExtensionFieldRepoDB exposes the repo DB for purge batch tests.
func ExtensionFieldRepoDB(r *ExtensionFieldRepo) *sql.DB { return r.db }

// PurgeExtensionValuesByFieldCodeForTest runs the batched purge without wrapping ErrPurgeIncomplete.
func PurgeExtensionValuesByFieldCodeForTest(ctx context.Context, db interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}, fieldCode string, batchSize int) (int64, error) {
	return purgeExtensionValuesByFieldCode(ctx, db, fieldCode, batchSize, true)
}
