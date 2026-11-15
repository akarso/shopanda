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

// extensionValuePurgeBatchSize is the per-transaction delete limit.
// Tests must change it only via SetExtensionValuePurgeBatchSizeForTest.
var extensionValuePurgeBatchSize = 1000

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
	return wrapLockedPurge(purgeExtensionValuesByFieldCode(ctx, r.db, code, extensionValuePurgeBatchSize, true))
}

// PurgeIfSoftDeletedPluginOnly deletes values when a soft-deleted plugin-only
// row exists for code. Never-stored codes and ordinary soft-deleted rows return NotFound.
// Timeout/cancel wrap domainext.ErrPurgeIncomplete. A restored definition returns
// ErrPurgeDefinitionRestored; a missing row returns ErrPurgeDefinitionMissing.
func (r *ExtensionFieldRepo) PurgeIfSoftDeletedPluginOnly(ctx context.Context, code string) (int64, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return 0, fmt.Errorf("extension_field_repo: purge retired: empty code")
	}
	if err := assertSoftDeletedPluginOnly(ctx, r.db, code); err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return 0, err
		}
		return 0, fmt.Errorf("%w: %v", domainext.ErrPurgeIncomplete, err)
	}
	return wrapLockedPurge(purgeExtensionValuesByFieldCode(ctx, r.db, code, extensionValuePurgeBatchSize, true))
}

func wrapLockedPurge(n int64, err error) (int64, error) {
	if err == nil {
		return n, nil
	}
	if errors.Is(err, domainext.ErrPurgeDefinitionRestored) || errors.Is(err, domainext.ErrPurgeDefinitionMissing) {
		return n, err
	}
	if apperror.Is(err, apperror.CodeNotFound) {
		return n, err
	}
	return n, fmt.Errorf("%w: %v", domainext.ErrPurgeIncomplete, err)
}

type dbBatchExec interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func assertSoftDeletedPluginOnly(ctx context.Context, db interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}, code string) error {
	const q = `SELECT definition, deleted_at IS NOT NULL FROM extension_fields WHERE code = $1`
	var raw []byte
	var softDeleted bool
	err := db.QueryRowContext(ctx, q, code).Scan(&raw, &softDeleted)
	if errors.Is(err, sql.ErrNoRows) {
		return apperror.NotFound("extension field not found")
	}
	if err != nil {
		return fmt.Errorf("extension_field_repo: purge retired lookup: %w", err)
	}
	field, err := decodeExtensionField(code, raw)
	if err != nil {
		return err
	}
	if !softDeleted || !field.Access.PluginOnlyWrite {
		return apperror.NotFound("extension field not found")
	}
	return nil
}

// purgeExtensionValuesByFieldCode deletes values in short transactions.
// When lockField is true (retire path), each batch FOR UPDATE-locks the field row
// and returns ErrPurgeDefinitionRestored / ErrPurgeDefinitionMissing when the row
// is no longer a soft-deleted plugin-only definition.
func purgeExtensionValuesByFieldCode(ctx context.Context, db dbBatchExec, fieldCode string, batchSize int, lockField bool) (int64, error) {
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
		n, err := purgeOneExtensionValueBatch(ctx, tx, fieldCode, batchSize, lockField)
		if err != nil {
			_ = tx.Rollback()
			return total, err
		}
		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			return total, fmt.Errorf("extension_value_repo: purge batch commit: %w", err)
		}
		total += n
		// Finished only when a batch deletes fewer than the limit (no remaining rows).
		if n < int64(batchSize) {
			return total, nil
		}
	}
}

func purgeOneExtensionValueBatch(ctx context.Context, tx *sql.Tx, fieldCode string, batchSize int, lockField bool) (n int64, err error) {
	if lockField {
		const lockQ = `SELECT definition, deleted_at IS NOT NULL
			FROM extension_fields WHERE code = $1 FOR UPDATE`
		var raw []byte
		var softDeleted bool
		err = tx.QueryRowContext(ctx, lockQ, fieldCode).Scan(&raw, &softDeleted)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("%w: field %q no longer exists", domainext.ErrPurgeDefinitionMissing, fieldCode)
		}
		if err != nil {
			return 0, fmt.Errorf("extension_field_repo: purge lock: %w", err)
		}
		field, err := decodeExtensionField(fieldCode, raw)
		if err != nil {
			return 0, err
		}
		if !softDeleted {
			return 0, fmt.Errorf("%w: field %q is active again", domainext.ErrPurgeDefinitionRestored, fieldCode)
		}
		// Soft-deleted but no longer plugin-only: treat as not found (not "restored").
		if !field.Access.PluginOnlyWrite {
			return 0, apperror.NotFound("extension field not found")
		}
	}

	const q = `WITH doomed AS (
			SELECT ctid FROM extension_values WHERE field_code = $1 LIMIT $2
		)
		DELETE FROM extension_values v USING doomed WHERE v.ctid = doomed.ctid`
	res, err := tx.ExecContext(ctx, q, fieldCode, batchSize)
	if err != nil {
		return 0, fmt.Errorf("extension_value_repo: delete by field code batch: %w", err)
	}
	n, err = res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("extension_value_repo: delete by field code rows: %w", err)
	}
	return n, nil
}
