package http

import (
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/application/admin"
)

// resolveLanguageScopeID returns the active admin language from context, if any.
func resolveLanguageScopeID(r *http.Request) string {
	ac, err := admin.FromContext(r.Context())
	if err != nil || ac == nil {
		return ""
	}
	return strings.TrimSpace(ac.Language)
}

// ResolveCurrencyScopeID returns the active admin currency from context, if any.
func ResolveCurrencyScopeID(r *http.Request) string {
	return resolveCurrencyScopeID(r)
}

// resolveCurrencyScopeID returns the active admin currency from context, if any.
func resolveCurrencyScopeID(r *http.Request) string {
	ac, err := admin.FromContext(r.Context())
	if err != nil || ac == nil {
		return ""
	}
	return strings.TrimSpace(ac.Currency)
}

func fullAdminScopeDetailsFromRequest(r *http.Request) map[string]interface{} {
	details := make(map[string]interface{})
	ac, err := admin.FromContext(r.Context())
	if err != nil || ac == nil {
		return details
	}
	storeID := strings.TrimSpace(ac.StoreID)
	language := strings.TrimSpace(ac.Language)
	currency := strings.TrimSpace(ac.Currency)
	if storeID == "" || language == "" || currency == "" {
		return details
	}
	details["store_id"] = storeID
	details["language"] = language
	details["currency"] = currency
	return details
}
