package shared

import (
	"net/http"

	"github.com/akarso/shopanda/internal/platform/httpx"
)

// ParsePagination extracts offset and limit query parameters for admin list endpoints.
func ParsePagination(r *http.Request) (offset, limit int, err error) {
	return httpx.ParsePagination(r)
}

// ParseLimit extracts the limit query parameter (default 20, max 100).
func ParseLimit(r *http.Request) (limit int, err error) {
	return httpx.ParseLimit(r)
}
