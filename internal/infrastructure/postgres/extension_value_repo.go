package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// ExtensionValueRepo persists extension field values in Postgres.
type ExtensionValueRepo struct {
	db *sql.DB
}

// NewExtensionValueRepo creates an ExtensionValueRepo.
func NewExtensionValueRepo(db *sql.DB) (*ExtensionValueRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("extension_value_repo: db must not be nil")
	}
	return &ExtensionValueRepo{db: db}, nil
}

// ListByTarget returns all values for a target.
func (r *ExtensionValueRepo) ListByTarget(ctx context.Context, target domainext.Target) ([]domainext.Value, error) {
	const q = `SELECT field_code, value, updated_by, updated_at
		FROM extension_values
		WHERE target_type = $1 AND target_id = $2
		ORDER BY field_code ASC`
	rows, err := r.db.QueryContext(ctx, q, string(target.Type), target.ID)
	if err != nil {
		return nil, fmt.Errorf("extension_value_repo: list: %w", err)
	}
	defer rows.Close()

	out := make([]domainext.Value, 0)
	for rows.Next() {
		value, err := scanExtensionValue(rows.Scan, target)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("extension_value_repo: list rows: %w", err)
	}
	return out, nil
}

// Upsert stores or replaces a value row.
func (r *ExtensionValueRepo) Upsert(ctx context.Context, value domainext.Value) error {
	payload, err := json.Marshal(value.Payload)
	if err != nil {
		return fmt.Errorf("extension_value_repo: upsert marshal: %w", err)
	}
	updatedAt := value.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	const q = `INSERT INTO extension_values (target_type, target_id, field_code, value, updated_by, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (target_type, target_id, field_code) DO UPDATE SET
			value = EXCLUDED.value,
			updated_by = EXCLUDED.updated_by,
			updated_at = EXCLUDED.updated_at`
	_, err = r.db.ExecContext(ctx, q,
		string(value.TargetType), value.TargetID, value.FieldCode, payload, value.UpdatedBy, updatedAt,
	)
	if err != nil {
		return fmt.Errorf("extension_value_repo: upsert: %w", err)
	}
	return nil
}

// Delete removes a value row.
func (r *ExtensionValueRepo) Delete(ctx context.Context, target domainext.Target, fieldCode string) error {
	const q = `DELETE FROM extension_values WHERE target_type = $1 AND target_id = $2 AND field_code = $3`
	res, err := r.db.ExecContext(ctx, q, string(target.Type), target.ID, fieldCode)
	if err != nil {
		return fmt.Errorf("extension_value_repo: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("extension_value_repo: delete rows: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("extension value not found")
	}
	return nil
}

func scanExtensionValue(scan func(dest ...interface{}) error, target domainext.Target) (domainext.Value, error) {
	var fieldCode, updatedBy string
	var payloadRaw []byte
	var updatedAt time.Time
	if err := scan(&fieldCode, &payloadRaw, &updatedBy, &updatedAt); err != nil {
		return domainext.Value{}, fmt.Errorf("extension_value_repo: scan: %w", err)
	}
	var payload domainext.ValuePayload
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return domainext.Value{}, fmt.Errorf("extension_value_repo: decode payload: %w", err)
	}
	return domainext.Value{
		FieldCode:  fieldCode,
		TargetType: target.Type,
		TargetID:   target.ID,
		Payload:    payload,
		UpdatedBy:  updatedBy,
		UpdatedAt:  updatedAt.UTC(),
	}, nil
}
