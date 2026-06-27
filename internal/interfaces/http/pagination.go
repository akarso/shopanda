package http

import (
	"net/http"
	"strconv"

	"github.com/akarso/shopanda/internal/platform/apperror"
)

const (
	defaultPaginationLimit = 20
	maxPaginationLimit     = 100
)

// ParsePagination extracts offset and limit query parameters for admin list endpoints.
func ParsePagination(r *http.Request) (offset, limit int, err error) {
	offset = 0
	limit = defaultPaginationLimit

	if v := r.URL.Query().Get("offset"); v != "" {
		n, convErr := strconv.Atoi(v)
		if convErr != nil || n < 0 {
			return 0, 0, apperror.Validation("offset must be a non-negative integer")
		}
		offset = n
	}

	if v := r.URL.Query().Get("limit"); v != "" {
		n, convErr := strconv.Atoi(v)
		if convErr != nil || n < 1 {
			return 0, 0, apperror.Validation("limit must be a positive integer")
		}
		if n > maxPaginationLimit {
			n = maxPaginationLimit
		}
		limit = n
	}

	return offset, limit, nil
}

func parsePagination(r *http.Request) (int, int, error) {
	return ParsePagination(r)
}
