package translation

import (
	"context"
	"strings"

	"golang.org/x/text/language"
)

type ctxKey string

const languageKey ctxKey = "language"

// WithLanguage stores the resolved language in the context.
func WithLanguage(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, languageKey, lang)
}

// LanguageFromContext extracts the language from the context.
// Returns "en" if no language is present.
func LanguageFromContext(ctx context.Context) string {
	if v, ok := LanguageOK(ctx); ok {
		return v
	}
	return "en"
}

// LanguageOK returns the language stored by WithLanguage, or ("", false) when
// absent. Unlike LanguageFromContext it does not invent "en".
func LanguageOK(ctx context.Context) (string, bool) {
	if v, ok := ctx.Value(languageKey).(string); ok {
		v = strings.TrimSpace(v)
		if v != "" {
			return v, true
		}
	}
	return "", false
}

// PrimaryLanguage returns the BCP 47 primary language subtag (e.g. "pt" from
// "pt-BR"), or "" when lang is empty/invalid/undetermined. Confidence below
// High (including und/root → Low) is rejected so callers do not store "en"
// for an undetermined tag. Callers that need a hard default should use
// DisplayLanguage.
func PrimaryLanguage(lang string) string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return ""
	}
	tag, err := language.Parse(lang)
	if err != nil {
		return ""
	}
	base, conf := tag.Base()
	if conf < language.High {
		return ""
	}
	return base.String()
}

// DisplayLanguage returns PrimaryLanguage(preferred), else PrimaryLanguage(fallback),
// else "en". Used for order.line.display (request or order language, store fallback).
func DisplayLanguage(preferred, fallback string) string {
	if p := PrimaryLanguage(preferred); p != "" {
		return p
	}
	if p := PrimaryLanguage(fallback); p != "" {
		return p
	}
	return "en"
}
