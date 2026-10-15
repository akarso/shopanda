package storefront

import (
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
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
