package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/translation"
)

// Compile-time check that TranslationRepo implements translation.TranslationRepository.
var _ translation.TranslationRepository = (*TranslationRepo)(nil)

// TranslationRepo implements translation.TranslationRepository using PostgreSQL.
type TranslationRepo struct {
	db *sql.DB
}

// NewTranslationRepo returns a new TranslationRepo backed by db.
func NewTranslationRepo(db *sql.DB) (*TranslationRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewTranslationRepo: nil *sql.DB")
	}
	return &TranslationRepo{db: db}, nil
}

// FindByKeyAndLanguage returns a single translation.
// Returns (nil, nil) when not found.
func (r *TranslationRepo) FindByKeyAndLanguage(ctx context.Context, key, language string) (*translation.Translation, error) {
	const q = `SELECT key, language, value FROM translations WHERE key = $1 AND language = $2`

	var t translation.Translation
	err := r.db.QueryRowContext(ctx, q, key, language).Scan(&t.Key, &t.Language, &t.Value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("translation_repo: find by key and language: %w", err)
	}
	return &t, nil
}

// ListByLanguage returns all translations for a language, ordered by key.
func (r *TranslationRepo) ListByLanguage(ctx context.Context, language string) ([]translation.Translation, error) {
	const q = `SELECT key, language, value FROM translations WHERE language = $1 ORDER BY key`

	rows, err := r.db.QueryContext(ctx, q, language)
	if err != nil {
		return nil, fmt.Errorf("translation_repo: list by language: %w", err)
	}
	defer rows.Close()

	var translations []translation.Translation
	for rows.Next() {
		var t translation.Translation
		if err := rows.Scan(&t.Key, &t.Language, &t.Value); err != nil {
			return nil, fmt.Errorf("translation_repo: list scan: %w", err)
		}
		translations = append(translations, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("translation_repo: list rows: %w", err)
	}
	return translations, nil
}

// Upsert creates or updates a translation for a key+language pair.
func (r *TranslationRepo) Upsert(ctx context.Context, t *translation.Translation) error {
	if t == nil {
		return fmt.Errorf("translation_repo: upsert: translation must not be nil")
	}
	const q = `INSERT INTO translations (key, language, value)
		VALUES ($1, $2, $3)
		ON CONFLICT (key, language) DO UPDATE
		SET value = EXCLUDED.value`

	_, err := r.db.ExecContext(ctx, q, t.Key, t.Language, t.Value)
	if err != nil {
		return fmt.Errorf("translation_repo: upsert: %w", err)
	}
	return nil
}

// Delete removes a translation by key and language.
func (r *TranslationRepo) Delete(ctx context.Context, key, language string) error {
	const q = `DELETE FROM translations WHERE key = $1 AND language = $2`
	res, err := r.db.ExecContext(ctx, q, key, language)
	if err != nil {
		return fmt.Errorf("translation_repo: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("translation_repo: delete rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("translation_repo: delete: translation %s/%s not found", key, language)
	}
	return nil
}
