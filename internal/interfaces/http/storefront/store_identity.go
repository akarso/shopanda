package storefront

import (
	"net/http"
	"strings"

	storeidentity "github.com/akarso/shopanda/internal/application/storeidentity"
	"github.com/akarso/shopanda/internal/domain/store"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// StoreIdentityHandler serves public store identity.
type StoreIdentityHandler struct {
	svc *storeidentity.Service
}

// NewStoreIdentityHandler creates a StoreIdentityHandler.
func NewStoreIdentityHandler(svc *storeidentity.Service) *StoreIdentityHandler {
	if svc == nil {
		panic("StoreIdentityHandler: nil service")
	}
	return &StoreIdentityHandler{svc: svc}
}

// Get handles GET /api/v1/store/identity.
func (h *StoreIdentityHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Host-scoped legal data must not be shared across caches keyed only on path.
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Vary", "Host")

		st := store.FromContext(r.Context())
		if st == nil || strings.TrimSpace(st.ID) == "" {
			httpshared.JSONError(w, apperror.NotFound("store not found"))
			return
		}
		view, err := h.svc.GetPublicIdentity(r.Context(), st.ID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, view)
	}
}
