package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
)

// StoreAdminHandler serves store write endpoints.
type StoreAdminHandler struct {
	repo store.StoreRepository
	bus  *event.Bus
}

// NewStoreAdminHandler creates a StoreAdminHandler.
func NewStoreAdminHandler(repo store.StoreRepository, bus *event.Bus) *StoreAdminHandler {
	return &StoreAdminHandler{repo: repo, bus: bus}
}

type createStoreRequest struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Currency  string `json:"currency"`
	Country   string `json:"country"`
	Language  string `json:"language"`
	Domain    string `json:"domain"`
	IsDefault *bool  `json:"is_default"`
}

type updateStoreRequest struct {
	Code      *string `json:"code"`
	Name      *string `json:"name"`
	Currency  *string `json:"currency"`
	Country   *string `json:"country"`
	Language  *string `json:"language"`
	Domain    *string `json:"domain"`
	IsDefault *bool   `json:"is_default"`
}

// List handles GET /api/v1/admin/stores.
func (h *StoreAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stores, err := h.repo.FindAll(r.Context())
		if err != nil {
			JSONError(w, err)
			return
		}
		if stores == nil {
			stores = []store.Store{}
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"stores": stores,
		})
	}
}

// Create handles POST /api/v1/admin/stores.
func (h *StoreAdminHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createStoreRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		s, err := store.NewStore(id.New(), req.Code, req.Name, req.Currency, req.Country, req.Language, req.Domain)
		if err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}
		if req.IsDefault != nil && *req.IsDefault {
			s.IsDefault = true
		}

		if err := h.repo.Create(r.Context(), &s); err != nil {
			JSONError(w, err)
			return
		}

		_ = h.bus.Publish(r.Context(), event.New("store.created", "store.admin", map[string]interface{}{
			"store_id": s.ID,
			"code":     s.Code,
		}))

		JSON(w, http.StatusCreated, map[string]interface{}{
			"store": s,
		})
	}
}

// Update handles PUT /api/v1/admin/stores/{id}.
func (h *StoreAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sid := r.PathValue("id")
		if sid == "" {
			JSONError(w, apperror.Validation("store id is required"))
			return
		}

		var req updateStoreRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		s, err := h.repo.FindByID(r.Context(), sid)
		if err != nil {
			JSONError(w, err)
			return
		}
		if s == nil {
			JSONError(w, apperror.NotFound("store not found"))
			return
		}

		if req.Code != nil {
			c := strings.TrimSpace(*req.Code)
			if c == "" {
				JSONError(w, apperror.Validation("code must not be empty"))
				return
			}
			s.Code = c
		}
		if req.Name != nil {
			n := strings.TrimSpace(*req.Name)
			if n == "" {
				JSONError(w, apperror.Validation("name must not be empty"))
				return
			}
			s.Name = n
		}
		if req.Currency != nil {
			cur, curErr := store.NormalizeCurrency(*req.Currency)
			if curErr != nil {
				JSONError(w, apperror.Validation(curErr.Error()))
				return
			}
			s.Currency = cur
		}
		if req.Country != nil {
			cty, ctyErr := store.NormalizeCountry(*req.Country)
			if ctyErr != nil {
				JSONError(w, apperror.Validation(ctyErr.Error()))
				return
			}
			s.Country = cty
		}
		if req.Language != nil {
			lng, lngErr := store.NormalizeLanguage(*req.Language)
			if lngErr != nil {
				JSONError(w, apperror.Validation(lngErr.Error()))
				return
			}
			s.Language = lng
		}
		if req.Domain != nil {
			s.Domain = strings.TrimSpace(*req.Domain)
		}
		if req.IsDefault != nil {
			s.IsDefault = *req.IsDefault
		}

		if err := h.repo.Update(r.Context(), s); err != nil {
			JSONError(w, err)
			return
		}

		_ = h.bus.Publish(r.Context(), event.New("store.updated", "store.admin", map[string]interface{}{
			"store_id": s.ID,
			"code":     s.Code,
		}))

		JSON(w, http.StatusOK, map[string]interface{}{
			"store": s,
		})
	}
}
