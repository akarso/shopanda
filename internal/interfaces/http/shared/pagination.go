package shared

import (
	"net/http"

	"github.com/akarso/shopanda/internal/platform/httpx"
)

// ParsePagination extracts offset and limit query parameters for admin list endpoints.
func ParsePagination(r *http.Request) (offset, limit int, err error) {
	return httpx.ParsePagination(r)
}
