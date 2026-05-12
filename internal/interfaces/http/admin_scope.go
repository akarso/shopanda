package http

import (
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/application/admin"
)

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
