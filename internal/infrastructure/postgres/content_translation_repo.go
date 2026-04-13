package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/translation"
)

// Compile-time check that ContentTranslationRepo implements translation.ContentTranslationRepository.
var _ translation.ContentTranslationRepository = (*ContentTranslationRepo)(nil)

// ContentTranslationRepo implements translation.ContentTranslationRepository using PostgreSQL.
type ContentTranslationRepo struct {
	db *sql.DB
}

// NewContentTranslationRepo returns a new ContentTranslationRepo backed by db.
func NewContentTranslationRepo(db *sql.DB) (*ContentTranslationRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewContentTranslationRepo: nil *sql.DB")
	}
	return &ContentTranslationRepo{db: db}, nil
}

// FindByEntityAndLanguage returns all translated fields for an entity in a language.
// Returns an empty slice (not nil) when no translations exist.
func (r *ContentTranslationRepo) FindByEntityAndLanguage(ctx context.Context, entityID, language string) ([]translation.ContentTranslation, error) {
	const q = `SELECT entity_id, language, field, value
		FROM content_translations
		WHERE entity_id = $1 AND language = $2
		ORDER BY field`

	rows, err := r.db.QueryContext(ctx, q, entityID, language)
	if err != nil {
		return nil, fmt.Errorf("content_translation_repo: find by entity and language: %w", err)
	}
	defer rows.Close()

	var result []translation.ContentTranslation
	for rows.Next() {
		var ct translation.ContentTranslation
		if err := rows.Scan(&ct.EntityID, &ct.Language, &ct.Field, &ct.Value); err != nil {
			return nil, fmt.Errorf("content_translation_repo: scan: %w", err)
		}
		result = append(result, ct)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("content_translation_repo: rows: %w", err)
	}
	if result == nil {
		result = []translation.ContentTranslation{}
	}
	return result, nil
}

// FindFieldValue returns the translated value for a specific entity+language+field.
// Returns (nil, nil) when not found.
func (r *ContentTranslationRepo) FindFieldValue(ctx context.Context, entityID, language, field string) (*translation.ContentTranslation, error) {
	const q = `SELECT entity_id, language, field, value
		FROM content_translations
		WHERE entity_id = $1 AND language = $2 AND field = $3`

	var ct translation.ContentTranslation
	err := r.db.QueryRowContext(ctx, q, entityID, language, field).Scan(
		&ct.EntityID, &ct.Language, &ct.Field, &ct.Value,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("content_translation_repo: find field value: %w", err)
	}
	return &ct, nil
}

// Upsert creates or updates a content translation for an entity+language+field tuple.
func (r *ContentTranslationRepo) Upsert(ctx context.Context, ct *translation.ContentTranslation) error {
	if ct == nil {
		return fmt.Errorf("content_translation_repo: upsert: content translation must not be nil")
	}
	if _, err := translation.NewContentTranslation(ct.EntityID, ct.Language, ct.Field, ct.Value); err != nil {
		return fmt.Errorf("content_translation_repo: upsert: %w", err)
	}
	const q = `INSERT INTO content_translations (entity_id, language, field, value)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (entity_id, language, field) DO UPDATE
		SET value = EXCLUDED.value`

	_, err := r.db.ExecContext(ctx, q, ct.EntityID, ct.Language, ct.Field, ct.Value)
	if err != nil {
		return fmt.Errorf("content_translation_repo: upsert: %w", err)
	}
	return nil
}

// DeleteByEntity removes all translations for an entity.
func (r *ContentTranslationRepo) DeleteByEntity(ctx context.Context, entityID string) error {
	const q = `DELETE FROM content_translations WHERE entity_id = $1`
	_, err := r.db.ExecContext(ctx, q, entityID)
	if err != nil {
		return fmt.Errorf("content_translation_repo: delete by entity: %w", err)
	}
	return nil
}
