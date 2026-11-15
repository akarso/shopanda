package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

const extensionValuePurgeBatchSize = 1000

// RetireActive soft-deletes an active plugin-only field and commits, then optionally
// purges values in separate short transactions so a timeout keeps completed batches.
func (r *ExtensionFieldRepo) RetireActive(ctx context.Context, code string, purgeValues bool) (int64, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return 0, fmt.Errorf("extension_field_repo: retire active: empty code")
	}
	if err := r.Retire(ctx, code); err != nil {
		return 0, err
	}
	if !purgeValues {
		return 0, nil
	}
	n, err := purgeExtensionValuesByFieldCode(ctx, r.db, code, extensionValuePurgeBatchSize)
	if err != nil {
		return n, fmt.Errorf("%w: %v", domainext.ErrPurgeIncomplete, err)
	}
	return n, nil
}

// PurgeIfSoftDeletedPluginOnly deletes values when a soft-deleted plugin-only
// row exists for code. Never-stored codes and ordinary soft-deleted rows return NotFound.
func (r *ExtensionFieldRepo) PurgeIfSoftDeletedPluginOnly(ctx context.Context, code string) (int64, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return 0, fmt.Errorf("extension_field_repo: purge retired: empty code")
	}
	const q = `SELECT definition, deleted_at IS NOT NULL
		FROM extension_fields WHERE code = $1`
	var raw []byte
	var softDeleted bool
	err := r.db.QueryRowContext(ctx, q, code).Scan(&raw, &softDeleted)
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
	return purgeExtensionValuesByFieldCode(ctx, r.db, code, extensionValuePurgeBatchSize)
}

type dbBatchExec interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

func purgeExtensionValuesByFieldCode(ctx context.Context, db dbBatchExec, fieldCode string, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = extensionValuePurgeBatchSize
	}
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return total, fmt.Errorf("extension_value_repo: purge batch begin: %w", err)
		}
		n, err := deleteExtensionValuesByFieldCodeBatch(ctx, tx, fieldCode, batchSize)
		if err != nil {
			_ = tx.Rollback()
			return total, err
		}
		if err := tx.Commit(); err != nil {
			return total, fmt.Errorf("extension_value_repo: purge batch commit: %w", err)
		}
		total += n
		if n < int64(batchSize) {
			return total, nil
		}
	}
}

func deleteExtensionValuesByFieldCodeBatch(ctx context.Context, tx *sql.Tx, fieldCode string, batchSize int) (int64, error) {
	const q = `WITH doomed AS (
			SELECT ctid FROM extension_values WHERE field_code = $1 LIMIT $2
		)
		DELETE FROM extension_values v USING doomed WHERE v.ctid = doomed.ctid`
	res, err := tx.ExecContext(ctx, q, fieldCode, batchSize)
	if err != nil {
		return 0, fmt.Errorf("extension_value_repo: delete by field code batch: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("extension_value_repo: delete by field code rows: %w", err)
	}
	return n, nil
}
