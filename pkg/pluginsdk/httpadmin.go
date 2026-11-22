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

// ErrorOption mutates an application error before it is returned from the
// helpers below (plugin-owned client code, structured details).
type ErrorOption func(*apperror.Error)

// WithClientCode sets a stable client-facing code (must be plugin.<slug>).
// Invalid codes are ignored. JSONError uses it as error.code; HTTP status still
// follows the error class (validation → 422, unavailable → 503).
func WithClientCode(code string) ErrorOption {
	return func(e *apperror.Error) {
		_ = e.WithClientCode(code)
	}
}

// WithDetails attaches a structured details payload (e.g. failing lines, retryable).
// Nested values must use map[string]interface{} / []interface{} trees — only those
// shapes are deep-copied; typed maps/slices stay aliased to the caller.
func WithDetails(details map[string]interface{}) ErrorOption {
	return func(e *apperror.Error) {
		_ = e.WithDetails(details)
	}
}

func applyErrorOptions(e *apperror.Error, opts []ErrorOption) error {
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	return e
}

// ValidationError creates a validation (422) error renderable via JSONError.
func ValidationError(message string, opts ...ErrorOption) error {
	return applyErrorOptions(apperror.Validation(message), opts)
}

// UnavailableError creates an unavailable (503) error renderable via JSONError.
// Use for deliberate dependency failures (cart port down); unknown 500s stay masked.
func UnavailableError(message string, opts ...ErrorOption) error {
	return applyErrorOptions(apperror.Unavailable(message), opts)
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
