package translation

import "context"

// ContentTranslatorLogger is the logging interface used by ContentTranslator.
type ContentTranslatorLogger interface {
	Warn(event string, ctx map[string]interface{})
}

// ContentTranslator overlays translated field values onto entities.
type ContentTranslator struct {
	repo ContentTranslationRepository
	log  ContentTranslatorLogger
}

// NewContentTranslator creates a ContentTranslator backed by the given repository.
// log is optional; pass nil to disable warning logs.
func NewContentTranslator(repo ContentTranslationRepository, log ContentTranslatorLogger) *ContentTranslator {
	return &ContentTranslator{repo: repo, log: log}
}

// TranslateFields returns a map of field→value for the given entity in the
// language resolved from ctx via LanguageFromContext. Returns nil when no
// translations are found or when the repository returns an error.
func (ct *ContentTranslator) TranslateFields(ctx context.Context, entityID string) map[string]string {
	lang := LanguageFromContext(ctx)
	translations, err := ct.repo.FindByEntityAndLanguage(ctx, entityID, lang)
	if err != nil {
		if ct.log != nil {
			ct.log.Warn("content_translator.translate_fields.error", map[string]interface{}{
				"entity_id": entityID,
				"language":  lang,
				"error":     err.Error(),
			})
		}
		return nil
	}
	if len(translations) == 0 {
		return nil
	}
	fields := make(map[string]string, len(translations))
	for _, t := range translations {
		fields[t.Field] = t.Value
	}
	return fields
}
