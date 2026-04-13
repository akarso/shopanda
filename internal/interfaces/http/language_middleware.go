package http

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/domain/translation"
	"golang.org/x/text/language"
)

// localeRe matches BCP 47 primary language (2-3 alpha) optionally followed
// by a region subtag. This pre-filters candidates before parsing.
var localeRe = regexp.MustCompile(`(?i)^[a-z]{2,3}(-[a-z]{2})?$`)

// LanguageMiddleware resolves the request language using:
//  1. ?lang= query parameter
//  2. Accept-Language header (highest q-value tag)
//  3. store.Language from context
//  4. "en" default
//
// Each candidate is normalized and validated before acceptance.
// The resolved language is stored in the context via translation.WithLanguage.
func LanguageMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var lang string

			// 1. Query parameter.
			if candidate, ok := normalizeLocale(r.URL.Query().Get("lang")); ok {
				lang = candidate
			}

			// 2. Accept-Language header.
			if lang == "" {
				lang = parseAcceptLanguage(r.Header.Get("Accept-Language"))
			}

			// 3. Store default language.
			if lang == "" {
				if s := store.FromContext(r.Context()); s != nil {
					if candidate, ok := normalizeLocale(s.Language); ok {
						lang = candidate
					}
				}
			}

			// 4. Fallback.
			if lang == "" {
				lang = "en"
			}

			ctx := translation.WithLanguage(r.Context(), lang)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// normalizeLocale validates and canonicalizes a locale string using BCP 47
// parsing. Returns the canonical tag and true if valid, or ("", false).
func normalizeLocale(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || !localeRe.MatchString(s) {
		return "", false
	}
	tag, err := language.Parse(s)
	if err != nil {
		return "", false
	}
	return tag.String(), true
}

// parseAcceptLanguage parses the Accept-Language header, selects the tag with
// the highest q-value (ties broken by order), and returns its canonical form.
// Returns "" if the header is empty or contains no valid tags.
func parseAcceptLanguage(header string) string {
	if header == "" {
		return ""
	}

	var bestTag string
	bestQ := -1.0

	for _, entry := range strings.Split(header, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		tag := entry
		q := 1.0

		if idx := strings.IndexByte(entry, ';'); idx >= 0 {
			tag = entry[:idx]
			qPart := strings.TrimSpace(entry[idx+1:])
			if strings.HasPrefix(qPart, "q=") {
				parsed, err := strconv.ParseFloat(qPart[2:], 64)
				if err != nil || parsed <= 0 || parsed > 1 {
					continue // malformed or out-of-range q-value, skip entry
				}
				q = parsed
			}
		}

		canonical, ok := normalizeLocale(tag)
		if !ok {
			continue
		}

		if q > bestQ {
			bestTag = canonical
			bestQ = q
		}
	}

	return bestTag
}
