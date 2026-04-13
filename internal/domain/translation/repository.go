package translation

import (
	"context"
	"errors"
)

// ErrNotFound is returned when a translation does not exist.
var ErrNotFound = errors.New("translation not found")

// TranslationRepository defines persistence operations for system translations.
type TranslationRepository interface {
	// FindByKeyAndLanguage returns a single translation.
	// Returns (nil, nil) when not found.
	FindByKeyAndLanguage(ctx context.Context, key, language string) (*Translation, error)

	// ListByLanguage returns all translations for a language, ordered by key.
	ListByLanguage(ctx context.Context, language string) ([]Translation, error)

	// Upsert creates or updates a translation for a key+language pair.
	Upsert(ctx context.Context, t *Translation) error

	// Delete removes a translation by key and language.
	// Returns ErrNotFound if no translation exists for the given key and language.
	Delete(ctx context.Context, key, language string) error
}
