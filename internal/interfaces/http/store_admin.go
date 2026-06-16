package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// StoreAdminHandler serves store write endpoints.
type StoreAdminHandler struct {
	repo    store.StoreRepository
	bus     *event.Bus
	auditor *admin.Auditor
}

// NewStoreAdminHandler creates a StoreAdminHandler with a default info-level
// auditor. It is a convenience constructor for tests and simple wiring;
// production should use NewStoreAdminHandlerWithAuditor to pass an auditor built
// from the application's configured logger (see cmd/api/main.go).
func NewStoreAdminHandler(repo store.StoreRepository, bus *event.Bus) *StoreAdminHandler {
	return NewStoreAdminHandlerWithAuditor(repo, bus, admin.NewAuditor(logger.New("info")))
}

// NewStoreAdminHandlerWithAuditor creates a StoreAdminHandler with a custom auditor.
func NewStoreAdminHandlerWithAuditor(repo store.StoreRepository, bus *event.Bus, auditor *admin.Auditor) *StoreAdminHandler {
	if auditor == nil {
		panic("StoreAdminHandler: auditor must not be nil")
	}
	return &StoreAdminHandler{repo: repo, bus: bus, auditor: auditor}
}

func (h *StoreAdminHandler) audit(r *http.Request, action admin.AuditAction, resourceID string, details map[string]interface{}, err error) {
	merged := mergeAuditDetails(details, fullAdminScopeDetailsFromRequest(r))
	result := "success"
	errMsg := ""
	if err != nil {
		result = "error"
		errMsg = err.Error()
	}
	h.auditor.LogAction(r.Context(), admin.AuditEntry{
		AdminID:      adminIDFromRequest(r),
		Action:       action,
		ResourceType: "store",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
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
			verr := apperror.Validation("invalid request body")
			h.audit(r, admin.AuditStoreCreate, "", nil, verr)
			JSONError(w, verr)
			return
		}

		s, err := store.NewStore(id.New(), req.Code, req.Name, req.Currency, req.Country, req.Language, req.Domain)
		if err != nil {
			verr := apperror.Validation(err.Error())
			h.audit(r, admin.AuditStoreCreate, "", nil, verr)
			JSONError(w, verr)
			return
		}
		if req.IsDefault != nil && *req.IsDefault {
			s.IsDefault = true
		}

		if err := h.repo.Create(r.Context(), &s); err != nil {
			h.audit(r, admin.AuditStoreCreate, s.ID, nil, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditStoreCreate, s.ID, map[string]interface{}{"code": s.Code}, nil)

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
			verr := apperror.Validation("store id is required")
			h.audit(r, admin.AuditStoreUpdate, "", nil, verr)
			JSONError(w, verr)
			return
		}

		var req updateStoreRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			verr := apperror.Validation("invalid request body")
			h.audit(r, admin.AuditStoreUpdate, sid, nil, verr)
			JSONError(w, verr)
			return
		}

		s, err := h.repo.FindByID(r.Context(), sid)
		if err != nil {
			h.audit(r, admin.AuditStoreUpdate, sid, nil, err)
			JSONError(w, err)
			return
		}
		if s == nil {
			nf := apperror.NotFound("store not found")
			h.audit(r, admin.AuditStoreUpdate, sid, nil, nf)
			JSONError(w, nf)
			return
		}

		if req.Code != nil {
			c := strings.TrimSpace(*req.Code)
			if c == "" {
				verr := apperror.Validation("code must not be empty")
				h.audit(r, admin.AuditStoreUpdate, sid, nil, verr)
				JSONError(w, verr)
				return
			}
			s.Code = c
		}
		if req.Name != nil {
			n := strings.TrimSpace(*req.Name)
			if n == "" {
				verr := apperror.Validation("name must not be empty")
				h.audit(r, admin.AuditStoreUpdate, sid, nil, verr)
				JSONError(w, verr)
				return
			}
			s.Name = n
		}
		if req.Currency != nil {
			cur, curErr := store.NormalizeCurrency(*req.Currency)
			if curErr != nil {
				verr := apperror.Validation(curErr.Error())
				h.audit(r, admin.AuditStoreUpdate, sid, nil, verr)
				JSONError(w, verr)
				return
			}
			s.Currency = cur
		}
		if req.Country != nil {
			cty, ctyErr := store.NormalizeCountry(*req.Country)
			if ctyErr != nil {
				verr := apperror.Validation(ctyErr.Error())
				h.audit(r, admin.AuditStoreUpdate, sid, nil, verr)
				JSONError(w, verr)
				return
			}
			s.Country = cty
		}
		if req.Language != nil {
			lng, lngErr := store.NormalizeLanguage(*req.Language)
			if lngErr != nil {
				verr := apperror.Validation(lngErr.Error())
				h.audit(r, admin.AuditStoreUpdate, sid, nil, verr)
				JSONError(w, verr)
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
			h.audit(r, admin.AuditStoreUpdate, sid, nil, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditStoreUpdate, sid, map[string]interface{}{"code": s.Code}, nil)

		_ = h.bus.Publish(r.Context(), event.New("store.updated", "store.admin", map[string]interface{}{
			"store_id": s.ID,
			"code":     s.Code,
		}))

		JSON(w, http.StatusOK, map[string]interface{}{
			"store": s,
		})
	}
}
