package translation

import "context"

type ctxKey string

const languageKey ctxKey = "language"

// WithLanguage stores the resolved language in the context.
func WithLanguage(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, languageKey, lang)
}

// LanguageFromContext extracts the language from the context.
// Returns "en" if no language is present.
func LanguageFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(languageKey).(string); ok && v != "" {
		return v
	}
	return "en"
}
