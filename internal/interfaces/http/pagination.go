package http

import (
	"github.com/akarso/shopanda/internal/platform/httpx"
)

// ParsePagination extracts offset and limit query parameters for admin list endpoints.
var ParsePagination = httpx.ParsePagination
