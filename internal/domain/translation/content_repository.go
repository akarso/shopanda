package translation

import "context"

// ContentTranslationRepository defines persistence operations for content translations.
type ContentTranslationRepository interface {
	// FindByEntityAndLanguage returns all translated fields for an entity in a language.
	// Returns an empty slice (not nil) when no translations exist.
	FindByEntityAndLanguage(ctx context.Context, entityID, language string) ([]ContentTranslation, error)

	// FindFieldValue returns the translated value for a specific entity+language+field.
	// Returns (nil, nil) when not found.
	FindFieldValue(ctx context.Context, entityID, language, field string) (*ContentTranslation, error)

	// Upsert creates or updates a content translation for an entity+language+field tuple.
	Upsert(ctx context.Context, ct *ContentTranslation) error

	// DeleteByEntity removes all translations for an entity.
	DeleteByEntity(ctx context.Context, entityID string) error
}
