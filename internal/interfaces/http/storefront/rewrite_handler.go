package storefront

import (
	"net/http"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/domain/routing"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// RewriteHandler serves catch-all requests that have been resolved by
// ResolverMiddleware. It reads the URLRewrite from the context and
// returns the entity type and ID, allowing upstream consumers (API, frontend)
// to dispatch to the appropriate entity handler.
type RewriteHandler struct{}

// NewRewriteHandler returns a new RewriteHandler.
func NewRewriteHandler() *RewriteHandler {
	return &RewriteHandler{}
}

// Resolve returns the entity type and ID for a resolved URL rewrite.
func (h *RewriteHandler) Resolve() http.HandlerFunc {
	type resolveResponse struct {
		Type     string `json:"type"`
		EntityID string `json:"entity_id"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		rw := routing.RewriteFrom(r.Context())
		if rw == nil {
			httpshared.JSONError(w, apperror.NotFound("page not found"))
			return
		}
		httpshared.JSON(w, http.StatusOK, resolveResponse{
			Type:     rw.Type(),
			EntityID: rw.EntityID(),
		})
	}
}
