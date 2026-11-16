package pluginsdk

import (
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/httpx"
)

// JSON writes the standard API JSON response envelope ({"data": ...}).
func JSON(w http.ResponseWriter, status int, data interface{}) {
	httpx.JSON(w, status, data)
}

// JSONError writes a JSON error response using the standard code/status
// mapping. Pass errors built with ValidationError / NotFoundError / etc. (or
// any error — unrecognized errors render as a generic 500).
func JSONError(w http.ResponseWriter, err error) {
	httpx.JSONError(w, err)
}

// ParsePagination extracts offset and limit query parameters for list
// endpoints. Returns a *JSONError-renderable error on invalid input.
func ParsePagination(r *http.Request) (offset, limit int, err error) {
	return httpx.ParsePagination(r)
}

// AdminCurrencyScope returns the active admin currency from the request
// context, or "" when no admin scope is present.
func AdminCurrencyScope(r *http.Request) string {
	ac, err := admin.FromContext(r.Context())
	if err != nil || ac == nil {
		return ""
	}
	return strings.TrimSpace(ac.Currency)
}

// AdminStoreScope returns the active admin store scope from context, falling
// back to the "store_id" query parameter when the admin context has none.
func AdminStoreScope(r *http.Request) string {
	explicit := strings.TrimSpace(r.URL.Query().Get("store_id"))
	ac, err := admin.FromContext(r.Context())
	if err == nil && ac != nil {
		if s := strings.TrimSpace(ac.StoreID); s != "" {
			return s
		}
	}
	return explicit
}

// ValidationError creates a validation (422) error renderable via JSONError.
func ValidationError(message string) error {
	return apperror.Validation(message)
}

// NotFoundError creates a not_found (404) error renderable via JSONError.
func NotFoundError(message string) error {
	return apperror.NotFound(message)
}

// ConflictError creates a conflict (409) error renderable via JSONError.
func ConflictError(message string) error {
	return apperror.Conflict(message)
}

// InternalError wraps err as an internal (500) error renderable via JSONError.
func InternalError(message string, err error) error {
	return apperror.Wrap(apperror.CodeInternal, message, err)
}
