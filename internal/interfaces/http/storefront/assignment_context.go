package storefront

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func assignmentContextFromFields(parentProductID, parentUnitID string, deferred bool) catalog.AssignmentContext {
	return catalog.AssignmentContext{
		ParentProductID: strings.TrimSpace(parentProductID),
		ParentUnitID:    strings.TrimSpace(parentUnitID),
		Deferred:        deferred,
	}
}

func assignmentDeferredFromForm(raw string) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	switch raw {
	case "1", "true", "on", "yes":
		return true
	}
	if parsed, err := strconv.ParseBool(raw); err == nil {
		return parsed
	}
	return false
}

func withRequestAssignment(r *http.Request, ac catalog.AssignmentContext) *http.Request {
	if ac.Empty() {
		return r
	}
	return r.WithContext(catalog.WithAssignmentContext(r.Context(), ac))
}
