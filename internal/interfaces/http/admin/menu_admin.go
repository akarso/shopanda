package admin

import (
	"encoding/json"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"net/http"
	"time"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// MenuAdminHandler serves menu write endpoints.
type MenuAdminHandler struct {
	menus   cms.MenuRepository
	auditor *admin.Auditor
}

// NewMenuAdminHandler creates a MenuAdminHandler.
func NewMenuAdminHandler(menus cms.MenuRepository, auditor *admin.Auditor) *MenuAdminHandler {
	if menus == nil {
		panic("MenuAdminHandler: menus repository must not be nil")
	}
	if auditor == nil {
		panic("MenuAdminHandler: auditor must not be nil")
	}
	return &MenuAdminHandler{menus: menus, auditor: auditor}
}

type adminMenuSummaryResponse struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Title     string `json:"title"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type adminMenuItemResponse struct {
	ID         string `json:"id"`
	ParentID   string `json:"parent_id"`
	Label      string `json:"label"`
	LinkType   string `json:"link_type"`
	LinkTarget string `json:"link_target"`
	Position   int    `json:"position"`
	IsActive   bool   `json:"is_active"`
}

type adminMenuDetailResponse struct {
	adminMenuSummaryResponse
	Items []adminMenuItemResponse `json:"items"`
}

type updateMenuRequest struct {
	Title    string                  `json:"title"`
	IsActive *bool                   `json:"is_active"`
	Items    []updateMenuItemRequest `json:"items"`
}

type updateMenuItemRequest struct {
	ID         string `json:"id"`
	ParentID   string `json:"parent_id"`
	Label      string `json:"label"`
	LinkType   string `json:"link_type"`
	LinkTarget string `json:"link_target"`
	Position   int    `json:"position"`
	IsActive   *bool  `json:"is_active"`
}

func toAdminMenuSummary(m *cms.Menu) adminMenuSummaryResponse {
	return adminMenuSummaryResponse{
		ID:        m.ID(),
		Code:      m.Code(),
		Title:     m.Title(),
		IsActive:  m.IsActive(),
		CreatedAt: m.CreatedAt().UTC().Format(time.RFC3339),
		UpdatedAt: m.UpdatedAt().UTC().Format(time.RFC3339),
	}
}

func toAdminMenuDetail(data *cms.MenuWithItems) adminMenuDetailResponse {
	resp := adminMenuDetailResponse{
		adminMenuSummaryResponse: toAdminMenuSummary(data.Menu),
		Items:                    make([]adminMenuItemResponse, 0, len(data.Items)),
	}
	for _, item := range data.Items {
		resp.Items = append(resp.Items, adminMenuItemResponse{
			ID:         item.ID(),
			ParentID:   item.ParentID(),
			Label:      item.Label(),
			LinkType:   string(item.LinkType()),
			LinkTarget: item.LinkTarget(),
			Position:   item.Position(),
			IsActive:   item.IsActive(),
		})
	}
	return resp
}

func (h *MenuAdminHandler) audit(r *http.Request, action admin.AuditAction, resourceID string, details map[string]interface{}, err error) {
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
		ResourceType: "menu",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

// List handles GET /api/v1/admin/menus.
func (h *MenuAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		menus, err := h.menus.List(r.Context())
		if err != nil {
			h.audit(r, admin.AuditMenuRead, "", nil, err)
			httpshared.JSONError(w, err)
			return
		}
		result := make([]adminMenuSummaryResponse, 0, len(menus))
		for _, menu := range menus {
			result = append(result, toAdminMenuSummary(menu))
		}
		h.audit(r, admin.AuditMenuRead, "", map[string]interface{}{"count": len(result)}, nil)
		httpshared.JSON(w, http.StatusOK, result)
	}
}

// Get handles GET /api/v1/admin/menus/{id}.
func (h *MenuAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		menuID := r.PathValue("id")
		if menuID == "" {
			httpshared.JSONError(w, apperror.Validation("menu id is required"))
			return
		}
		data, err := h.menus.FindByID(r.Context(), menuID)
		if err != nil {
			h.audit(r, admin.AuditMenuRead, menuID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		if data == nil {
			h.audit(r, admin.AuditMenuRead, menuID, nil, apperror.NotFound("menu not found"))
			httpshared.JSONError(w, apperror.NotFound("menu not found"))
			return
		}
		h.audit(r, admin.AuditMenuRead, menuID, map[string]interface{}{"code": data.Menu.Code()}, nil)
		httpshared.JSON(w, http.StatusOK, toAdminMenuDetail(data))
	}
}

// Update handles PUT /api/v1/admin/menus/{id}.
func (h *MenuAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		menuID := r.PathValue("id")
		if menuID == "" {
			httpshared.JSONError(w, apperror.Validation("menu id is required"))
			return
		}

		var req updateMenuRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid JSON body"))
			return
		}

		existing, err := h.menus.FindByID(r.Context(), menuID)
		if err != nil {
			h.audit(r, admin.AuditMenuUpdate, menuID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		if existing == nil {
			h.audit(r, admin.AuditMenuUpdate, menuID, nil, apperror.NotFound("menu not found"))
			httpshared.JSONError(w, apperror.NotFound("menu not found"))
			return
		}

		menu := existing.Menu
		if err := menu.SetTitle(req.Title); err != nil {
			h.audit(r, admin.AuditMenuUpdate, menuID, nil, err)
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		if req.IsActive != nil {
			menu.SetActive(*req.IsActive)
		}

		items := make([]*cms.MenuItem, 0, len(req.Items))
		for _, raw := range req.Items {
			itemID := raw.ID
			if itemID == "" {
				itemID = id.New()
			}
			active := true
			if raw.IsActive != nil {
				active = *raw.IsActive
			}
			item, err := cms.NewMenuItem(
				itemID,
				menu.ID(),
				raw.ParentID,
				raw.Label,
				cms.LinkType(raw.LinkType),
				raw.LinkTarget,
				raw.Position,
			)
			if err != nil {
				h.audit(r, admin.AuditMenuUpdate, menuID, nil, err)
				httpshared.JSONError(w, apperror.Validation(err.Error()))
				return
			}
			if !active {
				item = cms.NewMenuItemFromDB(
					item.ID(), item.MenuID(), item.ParentID(), item.Label(),
					item.LinkType(), item.LinkTarget(), item.Position(),
					false, item.CreatedAt(), item.UpdatedAt(),
				)
			}
			items = append(items, item)
		}
		if err := cms.ValidateMenuItems(items); err != nil {
			h.audit(r, admin.AuditMenuUpdate, menuID, nil, err)
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}

		data := &cms.MenuWithItems{Menu: menu, Items: items}
		if err := h.menus.Save(r.Context(), data); err != nil {
			h.audit(r, admin.AuditMenuUpdate, menuID, map[string]interface{}{"code": menu.Code()}, err)
			httpshared.JSONError(w, err)
			return
		}

		saved, err := h.menus.FindByID(r.Context(), menuID)
		if err != nil {
			h.audit(r, admin.AuditMenuUpdate, menuID, map[string]interface{}{"code": menu.Code()}, err)
			httpshared.JSONError(w, err)
			return
		}
		if saved == nil {
			notFound := apperror.NotFound("menu not found")
			h.audit(r, admin.AuditMenuUpdate, menuID, map[string]interface{}{"code": menu.Code()}, notFound)
			httpshared.JSONError(w, notFound)
			return
		}
		h.audit(r, admin.AuditMenuUpdate, menuID, map[string]interface{}{
			"code":        menu.Code(),
			"items_count": len(saved.Items),
		}, nil)
		httpshared.JSON(w, http.StatusOK, toAdminMenuDetail(saved))
	}
}
