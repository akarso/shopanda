package translation

import "context"

// ContentTranslator overlays translated field values onto entities.
type ContentTranslator struct {
	repo ContentTranslationRepository
}

// NewContentTranslator creates a ContentTranslator backed by the given repository.
func NewContentTranslator(repo ContentTranslationRepository) *ContentTranslator {
	return &ContentTranslator{repo: repo}
}

// TranslateFields returns a map of field→value for the given entity in the
// language resolved from ctx. Returns an empty map when no translations exist.
func (ct *ContentTranslator) TranslateFields(ctx context.Context, entityID string) map[string]string {
	lang := LanguageFromContext(ctx)
	translations, err := ct.repo.FindByEntityAndLanguage(ctx, entityID, lang)
	if err != nil || len(translations) == 0 {
		return nil
	}
	fields := make(map[string]string, len(translations))
	for _, t := range translations {
		fields[t.Field] = t.Value
	}
	return fields
}
