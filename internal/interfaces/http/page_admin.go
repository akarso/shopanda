package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// PageAdminHandler serves page write endpoints.
type PageAdminHandler struct {
	pages   cms.PageRepository
	bus     *event.Bus
	auditor *admin.Auditor
}

// NewPageAdminHandler creates a PageAdminHandler with a default auditor.
func NewPageAdminHandler(pages cms.PageRepository, bus *event.Bus) *PageAdminHandler {
	return NewPageAdminHandlerWithAuditor(pages, bus, admin.NewAuditor(logger.New("info")))
}

// NewPageAdminHandlerWithAuditor creates a PageAdminHandler with a custom auditor.
func NewPageAdminHandlerWithAuditor(pages cms.PageRepository, bus *event.Bus, auditor *admin.Auditor) *PageAdminHandler {
	if pages == nil {
		panic("PageAdminHandler: pages repository must not be nil")
	}
	if bus == nil {
		panic("PageAdminHandler: event bus must not be nil")
	}
	if auditor == nil {
		panic("PageAdminHandler: auditor must not be nil")
	}
	return &PageAdminHandler{pages: pages, bus: bus, auditor: auditor}
}

type createPageRequest struct {
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	Language string `json:"language"`
	IsActive *bool  `json:"is_active"`
}

type updatePageRequest struct {
	Slug     *string `json:"slug"`
	Title    *string `json:"title"`
	Content  *string `json:"content"`
	Language *string `json:"language"`
	IsActive *bool   `json:"is_active"`
}

// adminPageResponse includes all fields for admin views.
type adminPageResponse struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Language  string `json:"language"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toAdminPageResponse(p *cms.Page) adminPageResponse {
	return adminPageResponse{
		ID:        p.ID(),
		Slug:      p.Slug(),
		Title:     p.Title(),
		Content:   p.Content(),
		Language:  p.Language(),
		IsActive:  p.IsActive(),
		CreatedAt: p.CreatedAt().UTC().Format(time.RFC3339),
		UpdatedAt: p.UpdatedAt().UTC().Format(time.RFC3339),
	}
}

func (h *PageAdminHandler) audit(r *http.Request, action admin.AuditAction, resourceID string, details map[string]interface{}, err error) {
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
		ResourceType: "page",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

// List handles GET /api/v1/admin/pages.
func (h *PageAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := ParsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}

		pages, err := h.pages.List(r.Context(), offset, limit)
		if err != nil {
			JSONError(w, err)
			return
		}

		result := make([]adminPageResponse, 0, len(pages))
		for _, p := range pages {
			result = append(result, toAdminPageResponse(p))
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"pages": result,
		})
	}
}

// Create handles POST /api/v1/admin/pages.
func (h *PageAdminHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createPageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			err := apperror.Validation("invalid request body")
			h.audit(r, admin.AuditPageCreate, "", nil, err)
			JSONError(w, err)
			return
		}

		page, err := cms.NewPage(id.New(), req.Slug, req.Title, req.Content)
		if err != nil {
			verr := apperror.Validation(err.Error())
			h.audit(r, admin.AuditPageCreate, "", nil, verr)
			JSONError(w, verr)
			return
		}

		page.SetLanguage(req.Language)
		if req.IsActive != nil {
			page.SetActive(*req.IsActive)
		}

		if err := h.pages.Create(r.Context(), page); err != nil {
			h.audit(r, admin.AuditPageCreate, page.ID(), nil, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditPageCreate, page.ID(), map[string]interface{}{"slug": page.Slug(), "page_language": page.Language()}, nil)

		_ = h.bus.Publish(r.Context(), event.New(cms.EventPageCreated, "page.admin", cms.PageCreatedData{
			PageID: page.ID(),
			Slug:   page.Slug(),
			Title:  page.Title(),
			Active: page.IsActive(),
		}))

		JSON(w, http.StatusCreated, map[string]interface{}{
			"page": toAdminPageResponse(page),
		})
	}
}

// Update handles PUT /api/v1/admin/pages/{id}.
func (h *PageAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := r.PathValue("id")
		if pid == "" {
			err := apperror.Validation("page id is required")
			h.audit(r, admin.AuditPageUpdate, "", nil, err)
			JSONError(w, err)
			return
		}

		var req updatePageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			verr := apperror.Validation("invalid request body")
			h.audit(r, admin.AuditPageUpdate, pid, nil, verr)
			JSONError(w, verr)
			return
		}

		page, err := h.pages.FindByID(r.Context(), pid)
		if err != nil {
			h.audit(r, admin.AuditPageUpdate, pid, nil, err)
			JSONError(w, err)
			return
		}
		if page == nil {
			nf := apperror.NotFound("page not found")
			h.audit(r, admin.AuditPageUpdate, pid, nil, nf)
			JSONError(w, nf)
			return
		}

		oldSlug := page.Slug()

		if req.Slug != nil {
			if err := page.SetSlug(*req.Slug); err != nil {
				verr := apperror.Validation(err.Error())
				h.audit(r, admin.AuditPageUpdate, pid, nil, verr)
				JSONError(w, verr)
				return
			}
		}
		if req.Title != nil {
			if err := page.SetTitle(*req.Title); err != nil {
				verr := apperror.Validation(err.Error())
				h.audit(r, admin.AuditPageUpdate, pid, nil, verr)
				JSONError(w, verr)
				return
			}
		}
		if req.Content != nil {
			page.SetContent(*req.Content)
		}
		if req.Language != nil {
			page.SetLanguage(*req.Language)
		}
		if req.IsActive != nil {
			page.SetActive(*req.IsActive)
		}

		if err := h.pages.Update(r.Context(), page); err != nil {
			h.audit(r, admin.AuditPageUpdate, pid, nil, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditPageUpdate, pid, map[string]interface{}{"slug": page.Slug(), "page_language": page.Language()}, nil)

		var publishedOldSlug string
		if oldSlug != page.Slug() {
			publishedOldSlug = oldSlug
		}

		_ = h.bus.Publish(r.Context(), event.New(cms.EventPageUpdated, "page.admin", cms.PageUpdatedData{
			PageID:  page.ID(),
			Slug:    page.Slug(),
			OldSlug: publishedOldSlug,
			Title:   page.Title(),
			Active:  page.IsActive(),
		}))

		JSON(w, http.StatusOK, map[string]interface{}{
			"page": toAdminPageResponse(page),
		})
	}
}

// Delete handles DELETE /api/v1/admin/pages/{id}.
func (h *PageAdminHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := r.PathValue("id")
		if pid == "" {
			err := apperror.Validation("page id is required")
			h.audit(r, admin.AuditPageDelete, "", nil, err)
			JSONError(w, err)
			return
		}

		page, err := h.pages.FindByID(r.Context(), pid)
		if err != nil {
			h.audit(r, admin.AuditPageDelete, pid, nil, err)
			JSONError(w, err)
			return
		}
		if page == nil {
			nf := apperror.NotFound("page not found")
			h.audit(r, admin.AuditPageDelete, pid, nil, nf)
			JSONError(w, nf)
			return
		}

		if err := h.pages.Delete(r.Context(), pid); err != nil {
			h.audit(r, admin.AuditPageDelete, pid, nil, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditPageDelete, pid, map[string]interface{}{"slug": page.Slug()}, nil)

		_ = h.bus.Publish(r.Context(), event.New(cms.EventPageDeleted, "page.admin", cms.PageDeletedData{
			PageID: page.ID(),
			Slug:   page.Slug(),
		}))

		JSON(w, http.StatusOK, map[string]interface{}{
			"deleted": true,
		})
	}
}
