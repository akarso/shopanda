package translation

import "context"

// Translator provides a t() function backed by a TranslationRepository.
type Translator struct {
	repo TranslationRepository
}

// NewTranslator creates a Translator backed by the given repository.
func NewTranslator(repo TranslationRepository) *Translator {
	return &Translator{repo: repo}
}

// T returns the translated value for the given key in the language
// resolved from ctx. If no translation is found, the key itself is returned.
func (t *Translator) T(ctx context.Context, key string) string {
	lang := LanguageFromContext(ctx)
	tr, err := t.repo.FindByKeyAndLanguage(ctx, key, lang)
	if err != nil || tr == nil {
		return key
	}
	return tr.Value
}
