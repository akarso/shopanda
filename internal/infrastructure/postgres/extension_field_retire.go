package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/platform/apperror"
)

const extensionValuePurgeBatchSize = 1000

// RetireActive soft-deletes an active plugin-only field and optionally purges
// its values in the same transaction.
func (r *ExtensionFieldRepo) RetireActive(ctx context.Context, code string, purgeValues bool) (int64, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return 0, fmt.Errorf("extension_field_repo: retire active: empty code")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("extension_field_repo: retire active begin: %w", err)
	}
	defer tx.Rollback()

	if err := r.softDeleteActive(ctx, tx, code, true); err != nil {
		return 0, err
	}
	var purged int64
	if purgeValues {
		purged, err = deleteExtensionValuesByFieldCodeTx(ctx, tx, code, extensionValuePurgeBatchSize)
		if err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("extension_field_repo: retire active commit: %w", err)
	}
	return purged, nil
}

// PurgeIfSoftDeletedPluginOnly deletes values when a soft-deleted plugin-only
// row exists for code. Never-stored codes and ordinary soft-deleted rows return NotFound.
func (r *ExtensionFieldRepo) PurgeIfSoftDeletedPluginOnly(ctx context.Context, code string) (int64, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return 0, fmt.Errorf("extension_field_repo: purge retired: empty code")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("extension_field_repo: purge retired begin: %w", err)
	}
	defer tx.Rollback()

	const q = `SELECT definition, deleted_at IS NOT NULL
		FROM extension_fields WHERE code = $1 FOR UPDATE`
	var raw []byte
	var softDeleted bool
	err = tx.QueryRowContext(ctx, q, code).Scan(&raw, &softDeleted)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, apperror.NotFound("extension field not found")
	}
	if err != nil {
		return 0, fmt.Errorf("extension_field_repo: purge retired lookup: %w", err)
	}
	field, err := decodeExtensionField(code, raw)
	if err != nil {
		return 0, err
	}
	if !softDeleted || !field.Access.PluginOnlyWrite {
		return 0, apperror.NotFound("extension field not found")
	}

	purged, err := deleteExtensionValuesByFieldCodeTx(ctx, tx, code, extensionValuePurgeBatchSize)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("extension_field_repo: purge retired commit: %w", err)
	}
	return purged, nil
}

func deleteExtensionValuesByFieldCodeTx(ctx context.Context, tx *sql.Tx, fieldCode string, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = extensionValuePurgeBatchSize
	}
	var total int64
	for {
		const q = `WITH doomed AS (
				SELECT ctid FROM extension_values WHERE field_code = $1 LIMIT $2
			)
			DELETE FROM extension_values v USING doomed WHERE v.ctid = doomed.ctid`
		res, err := tx.ExecContext(ctx, q, fieldCode, batchSize)
		if err != nil {
			return total, fmt.Errorf("extension_value_repo: delete by field code batch: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("extension_value_repo: delete by field code rows: %w", err)
		}
		total += n
		if n < int64(batchSize) {
			return total, nil
		}
	}
}
