package httpx

import (
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/application/admin"
)

// ResolveCurrencyScopeID returns the active admin currency from context, if any.
func ResolveCurrencyScopeID(r *http.Request) string {
	ac, err := admin.FromContext(r.Context())
	if err != nil || ac == nil {
		return ""
	}
	return strings.TrimSpace(ac.Currency)
}

// ResolveStoreScopeID returns the active admin store scope from context or query.
func ResolveStoreScopeID(r *http.Request) string {
	explicit := strings.TrimSpace(r.URL.Query().Get("store_id"))
	ac, err := admin.FromContext(r.Context())
	if err == nil && ac != nil {
		contextStoreID := strings.TrimSpace(ac.StoreID)
		if contextStoreID != "" {
			// Keep tenant boundary deterministic: context scope wins if query conflicts.
			if explicit != "" && explicit != contextStoreID {
				return contextStoreID
			}
			return contextStoreID
		}
	}
	return explicit
}
