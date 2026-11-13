package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// ExtensionFieldRepo persists extension field definitions in Postgres.
type ExtensionFieldRepo struct {
	db *sql.DB
}

// NewExtensionFieldRepo creates an ExtensionFieldRepo.
func NewExtensionFieldRepo(db *sql.DB) (*ExtensionFieldRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("extension_field_repo: db must not be nil")
	}
	return &ExtensionFieldRepo{db: db}, nil
}

// Create inserts a new active field or restores a soft-deleted row.
func (r *ExtensionFieldRepo) Create(ctx context.Context, field domainext.ExtensionField) error {
	if field.Code == "" {
		return fmt.Errorf("extension_field_repo: create: empty code")
	}
	definition, err := json.Marshal(field)
	if err != nil {
		return fmt.Errorf("extension_field_repo: create: marshal definition: %w", err)
	}
	now := time.Now().UTC()
	const q = `INSERT INTO extension_fields (code, definition, created_at, updated_at, deleted_at)
		VALUES ($1, $2, $3, $3, NULL)
		ON CONFLICT (code) DO UPDATE SET
			definition = EXCLUDED.definition,
			updated_at = EXCLUDED.updated_at,
			deleted_at = NULL
		WHERE extension_fields.deleted_at IS NOT NULL
		RETURNING code`
	var returnedCode string
	err = r.db.QueryRowContext(ctx, q, field.Code, definition, now).Scan(&returnedCode)
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		if _, findErr := r.FindByCode(ctx, field.Code); findErr == nil {
			return apperror.Conflict("extension field already exists")
		}
		return fmt.Errorf("extension_field_repo: create: field %q was not persisted", field.Code)
	}
	return fmt.Errorf("extension_field_repo: create: %w", err)
}

// Save upserts an extension field definition.
func (r *ExtensionFieldRepo) Save(ctx context.Context, field domainext.ExtensionField) error {
	if field.Code == "" {
		return fmt.Errorf("extension_field_repo: save: empty code")
	}
	definition, err := json.Marshal(field)
	if err != nil {
		return fmt.Errorf("extension_field_repo: save: marshal definition: %w", err)
	}
	now := time.Now().UTC()
	const q = `INSERT INTO extension_fields (code, definition, created_at, updated_at, deleted_at)
		VALUES ($1, $2, $3, $3, NULL)
		ON CONFLICT (code) DO UPDATE SET
			definition = EXCLUDED.definition,
			updated_at = EXCLUDED.updated_at,
			deleted_at = NULL
		WHERE extension_fields.deleted_at IS NOT NULL
			OR COALESCE((extension_fields.definition->'access'->>'PluginOnlyWrite')::boolean, false) = false
		RETURNING code`
	// The plugin-only guard lives in the WHERE clause so check and write are atomic.
	// A soft-deleted (retired) row may be restored by Save, like Create does.
	var returned string
	err = r.db.QueryRowContext(ctx, q, field.Code, definition, now).Scan(&returned)
	if errors.Is(err, sql.ErrNoRows) {
		return apperror.PluginOnlyFieldImmutable("plugin-only extension field cannot be modified")
	}
	if err != nil {
		return fmt.Errorf("extension_field_repo: save: %w", err)
	}
	return nil
}

// FindByCode returns an active field by code.
func (r *ExtensionFieldRepo) FindByCode(ctx context.Context, code string) (domainext.ExtensionField, error) {
	if code == "" {
		return domainext.ExtensionField{}, fmt.Errorf("extension_field_repo: find: empty code")
	}
	const q = `SELECT definition FROM extension_fields WHERE code = $1 AND deleted_at IS NULL`
	var raw []byte
	err := r.db.QueryRowContext(ctx, q, code).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return domainext.ExtensionField{}, apperror.NotFound("extension field not found")
	}
	if err != nil {
		return domainext.ExtensionField{}, fmt.Errorf("extension_field_repo: find: %w", err)
	}
	field, err := decodeExtensionField(code, raw)
	if err != nil {
		return domainext.ExtensionField{}, err
	}
	return field, nil
}

// ListActive returns non-deleted fields, optionally filtered by scope.
func (r *ExtensionFieldRepo) ListActive(ctx context.Context, scope domainext.TargetType) ([]domainext.ExtensionField, error) {
	q := `SELECT code, definition FROM extension_fields WHERE deleted_at IS NULL`
	args := []interface{}{}
	if scope != "" {
		q += ` AND definition->>'scope' = $1`
		args = append(args, string(scope))
	}
	q += ` ORDER BY code ASC`

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("extension_field_repo: list active: %w", err)
	}
	defer rows.Close()

	out := make([]domainext.ExtensionField, 0)
	for rows.Next() {
		var code string
		var raw []byte
		if err := rows.Scan(&code, &raw); err != nil {
			return nil, fmt.Errorf("extension_field_repo: list active: scan: %w", err)
		}
		field, err := decodeExtensionField(code, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, field)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("extension_field_repo: list active: rows: %w", err)
	}
	return out, nil
}

// SoftDelete marks a field as deleted.
func (r *ExtensionFieldRepo) SoftDelete(ctx context.Context, code string) error {
	if code == "" {
		return fmt.Errorf("extension_field_repo: soft delete: empty code")
	}
	// One statement: lock the active row, delete it unless it is plugin-only, and
	// report what was found so not-found vs immutable is decided atomically.
	const q = `WITH cur AS (
			SELECT code, COALESCE((definition->'access'->>'PluginOnlyWrite')::boolean, false) AS locked
			FROM extension_fields WHERE code = $1 AND deleted_at IS NULL FOR UPDATE
		), upd AS (
			UPDATE extension_fields e SET deleted_at = $2, updated_at = $2
			FROM cur WHERE e.code = cur.code AND NOT cur.locked
			RETURNING e.code
		)
		SELECT EXISTS (SELECT 1 FROM cur), EXISTS (SELECT 1 FROM upd)`
	var found, deleted bool
	if err := r.db.QueryRowContext(ctx, q, code, time.Now().UTC()).Scan(&found, &deleted); err != nil {
		return fmt.Errorf("extension_field_repo: soft delete: %w", err)
	}
	switch {
	case !found:
		return apperror.NotFound("extension field not found")
	case !deleted:
		return apperror.PluginOnlyFieldImmutable("plugin-only extension field cannot be deleted")
	}
	return nil
}

func decodeExtensionField(code string, raw []byte) (domainext.ExtensionField, error) {
	var field domainext.ExtensionField
	if err := json.Unmarshal(raw, &field); err != nil {
		return domainext.ExtensionField{}, fmt.Errorf("extension_field_repo: decode definition: %w", err)
	}
	if field.Code == "" {
		field.Code = code
	}
	if field.Code != code {
		return domainext.ExtensionField{}, fmt.Errorf("extension_field_repo: definition code %q does not match row %q", field.Code, code)
	}
	return field, nil
}
