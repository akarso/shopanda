package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// AttributeAdminHandler serves attribute and attribute-group admin endpoints.
type AttributeAdminHandler struct {
	store     *admin.AttributeStore
	auditor   *admin.Auditor
	facetSync discoveryFacetSyncer
}

type discoveryFacetSyncer interface {
	Sync(context.Context) error
}

// NewAttributeAdminHandler creates an AttributeAdminHandler with a default auditor.
func NewAttributeAdminHandler(store *admin.AttributeStore) *AttributeAdminHandler {
	return NewAttributeAdminHandlerWithAuditor(store, admin.NewAuditor(logger.New("info")))
}

// NewAttributeAdminHandlerWithAuditor creates an AttributeAdminHandler with a custom auditor.
func NewAttributeAdminHandlerWithAuditor(store *admin.AttributeStore, auditor *admin.Auditor) *AttributeAdminHandler {
	if store == nil {
		panic("AttributeAdminHandler: attribute store must not be nil")
	}
	if auditor == nil {
		panic("AttributeAdminHandler: auditor must not be nil")
	}
	return &AttributeAdminHandler{store: store, auditor: auditor}
}

// WithDiscoveryFacetSync enables hot-reload of search-engine discovery attribute facets after mutations.
func (h *AttributeAdminHandler) WithDiscoveryFacetSync(sync discoveryFacetSyncer) *AttributeAdminHandler {
	h.facetSync = sync
	return h
}

func (h *AttributeAdminHandler) syncDiscoveryFacets(ctx context.Context) error {
	if h.facetSync == nil {
		return nil
	}
	return h.facetSync.Sync(ctx)
}

func searchFacetSyncError(err error) error {
	if err == nil {
		return nil
	}
	return apperror.Internal("attribute saved but search facet sync failed: " + err.Error())
}

func attributeNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not found")
}

type createAttributeRequest struct {
	Code                string   `json:"code"`
	Label               string   `json:"label"`
	Type                string   `json:"type"`
	Required            bool     `json:"required"`
	Options             []string `json:"options"`
	UseInAdvancedSearch bool     `json:"use_in_advanced_search"`
	UseInLayeredNav     bool     `json:"use_in_layered_nav"`
	UseInPromoRules     bool     `json:"use_in_promo_rules"`
}

type updateAttributeRequest struct {
	Label               string   `json:"label"`
	Type                string   `json:"type"`
	Required            bool     `json:"required"`
	Options             []string `json:"options"`
	UseInAdvancedSearch bool     `json:"use_in_advanced_search"`
	UseInLayeredNav     bool     `json:"use_in_layered_nav"`
	UseInPromoRules     bool     `json:"use_in_promo_rules"`
}

type createAttributeGroupRequest struct {
	Code       string   `json:"code"`
	Label      string   `json:"label"`
	Attributes []string `json:"attributes"`
}

type updateAttributeGroupRequest struct {
	Label      string   `json:"label"`
	Attributes []string `json:"attributes"`
}

type adminAttributeResponse struct {
	Code                string   `json:"code"`
	Label               string   `json:"label"`
	Type                string   `json:"type"`
	Required            bool     `json:"required"`
	Options             []string `json:"options,omitempty"`
	Groups              []string `json:"groups,omitempty"`
	UseInAdvancedSearch bool     `json:"use_in_advanced_search"`
	UseInLayeredNav     bool     `json:"use_in_layered_nav"`
	UseInPromoRules     bool     `json:"use_in_promo_rules"`
}

type adminAttributeGroupResponse struct {
	Code       string   `json:"code"`
	Label      string   `json:"label"`
	Attributes []string `json:"attributes"`
}

func (h *AttributeAdminHandler) auditAttribute(r *http.Request, action admin.AuditAction, resourceID string, details map[string]interface{}, err error) {
	h.audit(r, action, "attribute", resourceID, details, err)
}

func (h *AttributeAdminHandler) auditGroup(r *http.Request, action admin.AuditAction, resourceID string, details map[string]interface{}, err error) {
	h.audit(r, action, "attribute_group", resourceID, details, err)
}

func (h *AttributeAdminHandler) audit(r *http.Request, action admin.AuditAction, resourceType, resourceID string, details map[string]interface{}, err error) {
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
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

func toAttributeResponse(attr catalog.Attribute, groups []string) adminAttributeResponse {
	if groups == nil {
		groups = []string{}
	}
	return adminAttributeResponse{
		Code:                attr.Code,
		Label:               attr.Label,
		Type:                string(attr.Type),
		Required:            attr.Required,
		Options:             attr.Options,
		Groups:              groups,
		UseInAdvancedSearch: attr.UseInAdvancedSearch,
		UseInLayeredNav:     attr.UseInLayeredNav,
		UseInPromoRules:     attr.UseInPromoRules,
	}
}

func toGroupResponse(group catalog.AttributeGroup) adminAttributeGroupResponse {
	attrs := group.Attributes
	if attrs == nil {
		attrs = []string{}
	}
	return adminAttributeGroupResponse{
		Code:       group.Code,
		Label:      group.Label,
		Attributes: attrs,
	}
}

func attributeFromCreateRequest(req createAttributeRequest) (catalog.Attribute, error) {
	code := strings.TrimSpace(req.Code)
	label := strings.TrimSpace(req.Label)
	attrType := catalog.AttributeType(strings.TrimSpace(strings.ToLower(req.Type)))
	attr, err := catalog.NewAttribute(code, label, attrType)
	if err != nil {
		return catalog.Attribute{}, err
	}
	attr.Required = req.Required
	attr.Options = req.Options
	attr.UseInAdvancedSearch = req.UseInAdvancedSearch
	attr.UseInLayeredNav = req.UseInLayeredNav
	attr.UseInPromoRules = req.UseInPromoRules
	return attr, nil
}

func attributeFromUpdateRequest(req updateAttributeRequest, code string) (catalog.Attribute, error) {
	label := strings.TrimSpace(req.Label)
	attrType := catalog.AttributeType(strings.TrimSpace(strings.ToLower(req.Type)))
	attr, err := catalog.NewAttribute(code, label, attrType)
	if err != nil {
		return catalog.Attribute{}, err
	}
	attr.Required = req.Required
	attr.Options = req.Options
	attr.UseInAdvancedSearch = req.UseInAdvancedSearch
	attr.UseInLayeredNav = req.UseInLayeredNav
	attr.UseInPromoRules = req.UseInPromoRules
	return attr, nil
}

func groupFromCreateRequest(req createAttributeGroupRequest) (catalog.AttributeGroup, error) {
	code := strings.TrimSpace(req.Code)
	label := strings.TrimSpace(req.Label)
	group, err := catalog.NewAttributeGroup(code, label)
	if err != nil {
		return catalog.AttributeGroup{}, err
	}
	group.Attributes = req.Attributes
	return group, nil
}

func groupFromUpdateRequest(req updateAttributeGroupRequest, code string) (catalog.AttributeGroup, error) {
	label := strings.TrimSpace(req.Label)
	group, err := catalog.NewAttributeGroup(code, label)
	if err != nil {
		return catalog.AttributeGroup{}, err
	}
	group.Attributes = req.Attributes
	return group, nil
}

func storeAPIError(err error) error {
	if err == nil {
		return nil
	}
	if admin.IsValidationError(err) {
		return apperror.Validation(err.Error())
	}
	msg := err.Error()
	if strings.Contains(msg, "not found") {
		return apperror.NotFound(msg)
	}
	if strings.Contains(msg, "already exists") {
		return apperror.Conflict(msg)
	}
	return apperror.Internal("attribute store operation failed")
}

// ListAttributes handles GET /api/v1/admin/attributes.
func (h *AttributeAdminHandler) ListAttributes() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupCode := strings.TrimSpace(r.URL.Query().Get("group"))
		attrs, err := h.store.ListAttributes(r.Context(), groupCode)
		if err != nil {
			verr := storeAPIError(err)
			h.auditAttribute(r, admin.AuditAttributeRead, "", map[string]interface{}{"group": groupCode}, verr)
			JSONError(w, verr)
			return
		}

		resp := make([]adminAttributeResponse, 0, len(attrs))
		for _, attr := range attrs {
			groups, gerr := h.store.GroupCodesForAttribute(r.Context(), attr.Code)
			if gerr != nil {
				apiErr := storeAPIError(gerr)
				h.auditAttribute(r, admin.AuditAttributeRead, "", nil, apiErr)
				JSONError(w, apiErr)
				return
			}
			resp = append(resp, toAttributeResponse(attr, groups))
		}
		h.auditAttribute(r, admin.AuditAttributeRead, "", map[string]interface{}{
			"group": groupCode,
			"count": len(resp),
		}, nil)
		JSON(w, http.StatusOK, map[string]interface{}{"attributes": resp})
	}
}

// GetAttribute handles GET /api/v1/admin/attributes/{code}.
func (h *AttributeAdminHandler) GetAttribute() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimSpace(r.PathValue("code"))
		attr, err := h.store.GetAttribute(r.Context(), code)
		if err != nil {
			verr := storeAPIError(err)
			h.auditAttribute(r, admin.AuditAttributeRead, code, nil, verr)
			JSONError(w, verr)
			return
		}
		groups, gerr := h.store.GroupCodesForAttribute(r.Context(), attr.Code)
		if gerr != nil {
			apiErr := storeAPIError(gerr)
			h.auditAttribute(r, admin.AuditAttributeRead, code, nil, apiErr)
			JSONError(w, apiErr)
			return
		}
		h.auditAttribute(r, admin.AuditAttributeRead, code, nil, nil)
		JSON(w, http.StatusOK, map[string]interface{}{"attribute": toAttributeResponse(attr, groups)})
	}
}

// CreateAttribute handles POST /api/v1/admin/attributes.
func (h *AttributeAdminHandler) CreateAttribute() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createAttributeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			verr := apperror.Validation("invalid request body")
			h.auditAttribute(r, admin.AuditAttributeCreate, "", nil, verr)
			JSONError(w, verr)
			return
		}

		attr, err := attributeFromCreateRequest(req)
		if err != nil {
			verr := apperror.Validation(err.Error())
			h.auditAttribute(r, admin.AuditAttributeCreate, strings.TrimSpace(req.Code), nil, verr)
			JSONError(w, verr)
			return
		}

		if err := h.store.CreateAttribute(r.Context(), attr); err != nil {
			verr := storeAPIError(err)
			h.auditAttribute(r, admin.AuditAttributeCreate, attr.Code, nil, verr)
			JSONError(w, verr)
			return
		}

		if err := h.syncDiscoveryFacets(r.Context()); err != nil {
			verr := searchFacetSyncError(err)
			h.auditAttribute(r, admin.AuditAttributeCreate, attr.Code, nil, verr)
			JSONError(w, verr)
			return
		}

		groups, gerr := h.store.GroupCodesForAttribute(r.Context(), attr.Code)
		if gerr != nil {
			apiErr := storeAPIError(gerr)
			h.auditAttribute(r, admin.AuditAttributeCreate, attr.Code, nil, apiErr)
			JSONError(w, apiErr)
			return
		}
		h.auditAttribute(r, admin.AuditAttributeCreate, attr.Code, nil, nil)
		JSON(w, http.StatusCreated, map[string]interface{}{"attribute": toAttributeResponse(attr, groups)})
	}
}

// UpdateAttribute handles PUT /api/v1/admin/attributes/{code}.
func (h *AttributeAdminHandler) UpdateAttribute() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimSpace(r.PathValue("code"))
		var req updateAttributeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			verr := apperror.Validation("invalid request body")
			h.auditAttribute(r, admin.AuditAttributeUpdate, code, nil, verr)
			JSONError(w, verr)
			return
		}

		attr, err := attributeFromUpdateRequest(req, code)
		if err != nil {
			verr := apperror.Validation(err.Error())
			h.auditAttribute(r, admin.AuditAttributeUpdate, code, nil, verr)
			JSONError(w, verr)
			return
		}

		if err := h.store.UpdateAttribute(r.Context(), code, attr); err != nil {
			verr := storeAPIError(err)
			h.auditAttribute(r, admin.AuditAttributeUpdate, code, nil, verr)
			JSONError(w, verr)
			return
		}

		if err := h.syncDiscoveryFacets(r.Context()); err != nil {
			verr := searchFacetSyncError(err)
			h.auditAttribute(r, admin.AuditAttributeUpdate, code, nil, verr)
			JSONError(w, verr)
			return
		}

		groups, gerr := h.store.GroupCodesForAttribute(r.Context(), attr.Code)
		if gerr != nil {
			apiErr := storeAPIError(gerr)
			h.auditAttribute(r, admin.AuditAttributeUpdate, code, nil, apiErr)
			JSONError(w, apiErr)
			return
		}
		h.auditAttribute(r, admin.AuditAttributeUpdate, code, nil, nil)
		JSON(w, http.StatusOK, map[string]interface{}{"attribute": toAttributeResponse(attr, groups)})
	}
}

// DeleteAttribute handles DELETE /api/v1/admin/attributes/{code}.
func (h *AttributeAdminHandler) DeleteAttribute() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimSpace(r.PathValue("code"))
		if err := h.store.DeleteAttribute(r.Context(), code); err != nil {
			if attributeNotFound(err) {
				if syncErr := h.syncDiscoveryFacets(r.Context()); syncErr != nil {
					verr := searchFacetSyncError(syncErr)
					h.auditAttribute(r, admin.AuditAttributeDelete, code, nil, verr)
					JSONError(w, verr)
					return
				}
			}
			verr := storeAPIError(err)
			h.auditAttribute(r, admin.AuditAttributeDelete, code, nil, verr)
			JSONError(w, verr)
			return
		}
		if err := h.syncDiscoveryFacets(r.Context()); err != nil {
			verr := searchFacetSyncError(err)
			h.auditAttribute(r, admin.AuditAttributeDelete, code, nil, verr)
			JSONError(w, verr)
			return
		}
		h.auditAttribute(r, admin.AuditAttributeDelete, code, nil, nil)
		JSON(w, http.StatusOK, map[string]interface{}{"deleted": true})
	}
}

// ListGroups handles GET /api/v1/admin/attribute-groups.
func (h *AttributeAdminHandler) ListGroups() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groups, err := h.store.ListGroups(r.Context())
		if err != nil {
			apiErr := storeAPIError(err)
			h.auditGroup(r, admin.AuditAttributeGroupRead, "", nil, apiErr)
			JSONError(w, apiErr)
			return
		}
		resp := make([]adminAttributeGroupResponse, 0, len(groups))
		for _, group := range groups {
			resp = append(resp, toGroupResponse(group))
		}
		h.auditGroup(r, admin.AuditAttributeGroupRead, "", map[string]interface{}{"count": len(resp)}, nil)
		JSON(w, http.StatusOK, map[string]interface{}{"groups": resp})
	}
}

// GetGroup handles GET /api/v1/admin/attribute-groups/{code}.
func (h *AttributeAdminHandler) GetGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimSpace(r.PathValue("code"))
		group, err := h.store.GetGroup(r.Context(), code)
		if err != nil {
			verr := storeAPIError(err)
			h.auditGroup(r, admin.AuditAttributeGroupRead, code, nil, verr)
			JSONError(w, verr)
			return
		}
		h.auditGroup(r, admin.AuditAttributeGroupRead, code, nil, nil)
		JSON(w, http.StatusOK, map[string]interface{}{"group": toGroupResponse(group)})
	}
}

// CreateGroup handles POST /api/v1/admin/attribute-groups.
func (h *AttributeAdminHandler) CreateGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createAttributeGroupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			verr := apperror.Validation("invalid request body")
			h.auditGroup(r, admin.AuditAttributeGroupCreate, "", nil, verr)
			JSONError(w, verr)
			return
		}

		group, err := groupFromCreateRequest(req)
		if err != nil {
			verr := apperror.Validation(err.Error())
			h.auditGroup(r, admin.AuditAttributeGroupCreate, strings.TrimSpace(req.Code), nil, verr)
			JSONError(w, verr)
			return
		}

		if err := h.store.CreateGroup(r.Context(), group); err != nil {
			verr := storeAPIError(err)
			h.auditGroup(r, admin.AuditAttributeGroupCreate, group.Code, nil, verr)
			JSONError(w, verr)
			return
		}

		h.auditGroup(r, admin.AuditAttributeGroupCreate, group.Code, nil, nil)
		JSON(w, http.StatusCreated, map[string]interface{}{"group": toGroupResponse(group)})
	}
}

// UpdateGroup handles PUT /api/v1/admin/attribute-groups/{code}.
func (h *AttributeAdminHandler) UpdateGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimSpace(r.PathValue("code"))
		var req updateAttributeGroupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			verr := apperror.Validation("invalid request body")
			h.auditGroup(r, admin.AuditAttributeGroupUpdate, code, nil, verr)
			JSONError(w, verr)
			return
		}

		group, err := groupFromUpdateRequest(req, code)
		if err != nil {
			verr := apperror.Validation(err.Error())
			h.auditGroup(r, admin.AuditAttributeGroupUpdate, code, nil, verr)
			JSONError(w, verr)
			return
		}

		if err := h.store.UpdateGroup(r.Context(), code, group); err != nil {
			verr := storeAPIError(err)
			h.auditGroup(r, admin.AuditAttributeGroupUpdate, code, nil, verr)
			JSONError(w, verr)
			return
		}

		h.auditGroup(r, admin.AuditAttributeGroupUpdate, code, nil, nil)
		JSON(w, http.StatusOK, map[string]interface{}{"group": toGroupResponse(group)})
	}
}

// DeleteGroup handles DELETE /api/v1/admin/attribute-groups/{code}.
func (h *AttributeAdminHandler) DeleteGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimSpace(r.PathValue("code"))
		if err := h.store.DeleteGroup(r.Context(), code); err != nil {
			verr := storeAPIError(err)
			h.auditGroup(r, admin.AuditAttributeGroupDelete, code, nil, verr)
			JSONError(w, verr)
			return
		}
		h.auditGroup(r, admin.AuditAttributeGroupDelete, code, nil, nil)
		JSON(w, http.StatusOK, map[string]interface{}{"deleted": true})
	}
}
