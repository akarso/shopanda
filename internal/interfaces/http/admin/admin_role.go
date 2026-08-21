package admin

import (
	"context"
	"encoding/json"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"net/http"

	"github.com/akarso/shopanda/internal/application/admin"
	adminroleApp "github.com/akarso/shopanda/internal/application/adminrole"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// AdminRoleHandler serves admin role permission endpoints.
type AdminRoleHandler struct {
	roles   *adminroleApp.Service
	auditor *admin.Auditor
}

// NewAdminRoleHandler creates an AdminRoleHandler.
func NewAdminRoleHandler(roles *adminroleApp.Service, auditor *admin.Auditor) *AdminRoleHandler {
	if roles == nil {
		panic("http: admin role service must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &AdminRoleHandler{roles: roles, auditor: auditor}
}

// Catalog handles GET /api/v1/admin/permissions.
func (h *AdminRoleHandler) Catalog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminID := adminIDFromRequest(r)
		catalog := h.roles.Catalog()
		h.audit(r.Context(), adminID, admin.AuditPermissionCatalogRead, "", "success", "", map[string]interface{}{
			"count": len(catalog),
		})
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"permissions": catalog,
		})
	}
}

// List handles GET /api/v1/admin/roles.
func (h *AdminRoleHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminID := adminIDFromRequest(r)
		roles, err := h.roles.ListRoles(r.Context())
		if err != nil {
			h.audit(r.Context(), adminID, admin.AuditRoleList, "", "error", err.Error(), nil)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r.Context(), adminID, admin.AuditRoleList, "", "success", "", map[string]interface{}{
			"count": len(roles),
		})
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"roles": roles,
		})
	}
}

// Get handles GET /api/v1/admin/roles/{role}.
func (h *AdminRoleHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roleName := r.PathValue("role")
		adminID := adminIDFromRequest(r)

		role, err := parseAdminRole(roleName)
		if err != nil {
			h.audit(r.Context(), adminID, admin.AuditRoleRead, roleName, "error", err.Error(), nil)
			httpshared.JSONError(w, err)
			return
		}

		resp, err := h.roles.GetRole(r.Context(), role)
		if err != nil {
			h.audit(r.Context(), adminID, admin.AuditRoleRead, roleName, "error", err.Error(), nil)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r.Context(), adminID, admin.AuditRoleRead, roleName, "success", "", nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"role": resp,
		})
	}
}

type updateRoleBody struct {
	Permissions []string `json:"permissions"`
}

// Update handles PUT /api/v1/admin/roles/{role}.
func (h *AdminRoleHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roleName := r.PathValue("role")
		adminID := adminIDFromRequest(r)

		role, err := parseAdminRole(roleName)
		if err != nil {
			h.audit(r.Context(), adminID, admin.AuditRoleUpdate, roleName, "error", err.Error(), nil)
			httpshared.JSONError(w, err)
			return
		}

		var req updateRoleBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.audit(r.Context(), adminID, admin.AuditRoleUpdate, roleName, "error", "invalid request body", nil)
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		resp, err := h.roles.UpdateRole(r.Context(), role, req.Permissions)
		if err != nil {
			h.audit(r.Context(), adminID, admin.AuditRoleUpdate, roleName, "error", err.Error(), nil)
			httpshared.JSONError(w, err)
			return
		}

		h.audit(r.Context(), adminID, admin.AuditRoleUpdate, roleName, "success", "", map[string]interface{}{
			"permission_count": len(resp.Permissions),
		})
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"role": resp,
		})
	}
}

func (h *AdminRoleHandler) audit(ctx context.Context, adminID string, action admin.AuditAction, resourceID, result, errMsg string, details map[string]interface{}) {
	entry := admin.AuditEntry{
		AdminID:      adminID,
		Action:       action,
		ResourceType: "admin_role",
		ResourceID:   resourceID,
		Result:       result,
		Details:      details,
	}
	if errMsg != "" {
		entry.Error = errMsg
	}
	h.auditor.LogAction(ctx, entry)
}

func parseAdminRole(raw string) (identity.Role, error) {
	role := identity.Role(raw)
	if !rbac.IsAdminRole(role) {
		return "", apperror.Validation("invalid admin role")
	}
	return role, nil
}
