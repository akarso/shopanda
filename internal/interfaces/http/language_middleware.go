package http

import (
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/domain/translation"
)

// LanguageMiddleware resolves the request language using:
//  1. ?lang= query parameter
//  2. Accept-Language header (first 2- or 5-char tag)
//  3. store.Language from context
//  4. "en" default
//
// The resolved language is stored in the context via translation.WithLanguage.
func LanguageMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lang := r.URL.Query().Get("lang")

			if lang == "" {
				lang = parseAcceptLanguage(r.Header.Get("Accept-Language"))
			}

			if lang == "" {
				if s := store.FromContext(r.Context()); s != nil && s.Language != "" {
					lang = s.Language
				}
			}

			if lang == "" {
				lang = "en"
			}

			lang = strings.ToLower(strings.TrimSpace(lang))
			ctx := translation.WithLanguage(r.Context(), lang)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// parseAcceptLanguage extracts the first language tag from the Accept-Language
// header value. Returns "" if the header is empty or unparseable.
func parseAcceptLanguage(header string) string {
	if header == "" {
		return ""
	}
	// Take the first entry (highest priority).
	if idx := strings.IndexByte(header, ','); idx >= 0 {
		header = header[:idx]
	}
	// Strip quality value (e.g. "de;q=0.9" → "de").
	if idx := strings.IndexByte(header, ';'); idx >= 0 {
		header = header[:idx]
	}
	tag := strings.ToLower(strings.TrimSpace(header))
	if len(tag) == 2 || len(tag) == 5 {
		return tag
	}
	return ""
}
