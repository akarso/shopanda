package storefront

import (
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// parseVisibilityAfter reads and decodes the optional opaque `after` resume
// cursor from meta.next_after. Empty means start from the beginning (subject
// to offset). When after is set, the offset query parameter is ignored.
func parseVisibilityAfter(r *http.Request) (offset, skip int, set bool, err error) {
	v := strings.TrimSpace(r.URL.Query().Get("after"))
	if v == "" {
		return 0, 0, false, nil
	}
	o, s, err := catalog.DecodeListCursor(v)
	if err != nil {
		return 0, 0, false, apperror.Validation(err.Error())
	}
	return o, s, true, nil
}

// parseListingPagination decodes after first. When a cursor is present,
// offset is ignored (including invalid offset values) and only limit is
// parsed. Matches the OpenAPI contract that after supersedes offset.
func parseListingPagination(r *http.Request) (offset, limit, afterOff, afterSkip int, afterSet bool, err error) {
	afterOff, afterSkip, afterSet, err = parseVisibilityAfter(r)
	if err != nil {
		return 0, 0, 0, 0, false, err
	}
	if afterSet {
		limit, err = httpshared.ParseLimit(r)
		if err != nil {
			return 0, 0, 0, 0, false, err
		}
		return 0, limit, afterOff, afterSkip, true, nil
	}
	offset, limit, err = httpshared.ParsePagination(r)
	if err != nil {
		return 0, 0, 0, 0, false, err
	}
	return offset, limit, 0, 0, false, nil
}

func visibilityListMeta(page catalog.VisiblePage) map[string]interface{} {
	meta := map[string]interface{}{
		"truncated":     page.Truncated,
		"has_more":      page.HasMore,
		"depth_limited": page.DepthLimited,
	}
	if page.HasMore && page.NextAfter != "" {
		meta["next_after"] = page.NextAfter
	}
	return meta
}
