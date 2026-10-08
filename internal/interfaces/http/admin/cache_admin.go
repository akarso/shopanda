package admin

import (
	"encoding/json"
	"net/http"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	cacheapp "github.com/akarso/shopanda/internal/application/cache"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/domain/store"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// CacheAdminHandler serves GET /admin/cache/stats, POST /admin/cache/clear,
// and POST /admin/cache/purge-url on top of application/cache.AdminService
// (PR-1042 / PR-1046). CLI uses the same service methods — no separate logic path.
type CacheAdminHandler struct {
	svc     *cacheapp.AdminService
	auditor *adminapp.Auditor
	stores  store.StoreRepository
	hasPerm func(identity.Role, rbac.Permission) bool
}

// NewCacheAdminHandler creates a CacheAdminHandler.
func NewCacheAdminHandler(svc *cacheapp.AdminService, auditor *adminapp.Auditor) *CacheAdminHandler {
	if svc == nil {
		panic("http: cache admin service must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &CacheAdminHandler{svc: svc, auditor: auditor, hasPerm: rbac.HasPermission}
}

// WithStores enables purge-url vary expansion across every configured store.
func (h *CacheAdminHandler) WithStores(stores store.StoreRepository) *CacheAdminHandler {
	if h != nil {
		h.stores = stores
	}
	return h
}

func (h *CacheAdminHandler) checkPermission(role identity.Role, perm rbac.Permission) bool {
	fn := h.hasPerm
	if fn == nil {
		fn = rbac.HasPermission
	}
	return fn(role, perm)
}

func (h *CacheAdminHandler) audit(r *http.Request, action adminapp.AuditAction, resourceID string, details map[string]interface{}, err error) {
	merged := mergeAuditDetails(details, fullAdminScopeDetailsFromRequest(r))
	result := "success"
	errMsg := ""
	if err != nil {
		result = "error"
		errMsg = err.Error()
	}
	h.auditor.LogAction(r.Context(), adminapp.AuditEntry{
		AdminID:      adminIDFromRequest(r),
		Action:       action,
		ResourceType: "cache",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

// Stats handles GET /api/v1/admin/cache/stats.
func (h *CacheAdminHandler) Stats() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snap, err := h.svc.Stats(r.Context())
		if err != nil {
			h.audit(r, adminapp.AuditCacheStats, "", nil, err)
			httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "cache stats failed", err))
			return
		}
		// Success is not audit-logged: stats are polled. Failures still are.
		httpshared.JSON(w, http.StatusOK, snap)
	}
}

type cacheClearBody struct {
	Prefix *string `json:"prefix"`
	Tag    *string `json:"tag"`
	Key    *string `json:"key"`
	All    *bool   `json:"all"`
}

// Clear handles POST /api/v1/admin/cache/clear. Targeted clears require
// cache.write; {"all": true} requires cache.clear_all. The route is
// gated by RequireAnyPermission of those two; this handler picks which.
func (h *CacheAdminHandler) Clear() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body cacheClearBody
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			err := apperror.Validation("invalid request body")
			h.audit(r, adminapp.AuditCacheClear, "", nil, err)
			httpshared.JSONError(w, err)
			return
		}
		if err := requireSingleJSONValue(dec); err != nil {
			err := apperror.Validation(err.Error())
			h.audit(r, adminapp.AuditCacheClear, "", nil, err)
			httpshared.JSONError(w, err)
			return
		}

		req := cacheapp.ClearRequest{
			Prefix: derefString(body.Prefix),
			Tag:    derefString(body.Tag),
			Key:    derefString(body.Key),
			All:    body.All != nil && *body.All,
		}
		mode, target, err := req.Normalize()
		if err != nil {
			h.audit(r, adminapp.AuditCacheClear, "", nil, err)
			httpshared.JSONError(w, err)
			return
		}

		role := auth.IdentityFrom(r.Context()).Role
		if mode == cacheapp.ClearAll {
			if !h.checkPermission(role, rbac.CacheClearAll) {
				err := apperror.Forbidden("cache.clear_all permission required")
				h.audit(r, adminapp.AuditCacheClear, "all", map[string]interface{}{"mode": "all"}, err)
				httpshared.JSONError(w, err)
				return
			}
		} else if !h.checkPermission(role, rbac.CacheWrite) {
			err := apperror.Forbidden("cache.write permission required")
			details := map[string]interface{}{"mode": string(mode)}
			if target != "" {
				details["target"] = target
			}
			h.audit(r, adminapp.AuditCacheClear, target, details, err)
			httpshared.JSONError(w, err)
			return
		}

		result, err := h.svc.Clear(r.Context(), req)
		details := map[string]interface{}{"mode": string(result.Mode)}
		if result.Target != "" {
			details["target"] = result.Target
		}
		if result.Deleted != nil {
			details["deleted"] = *result.Deleted
		}
		resourceID := result.Target
		if mode == cacheapp.ClearAll {
			resourceID = "all"
		}
		if err != nil {
			h.audit(r, adminapp.AuditCacheClear, resourceID, details, err)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r, adminapp.AuditCacheClear, resourceID, details, nil)
		httpshared.JSON(w, http.StatusOK, result)
	}
}

type cachePurgeURLBody struct {
	Path string `json:"path"`
}

// PurgeURL handles POST /api/v1/admin/cache/purge-url (PR-1046).
// Requires cache.purge_url. Deletes concrete FPC keys for path × store × auth.
func (h *CacheAdminHandler) PurgeURL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body cachePurgeURLBody
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			err := apperror.Validation("invalid request body")
			h.audit(r, adminapp.AuditCachePurgeURL, "", nil, err)
			httpshared.JSONError(w, err)
			return
		}
		if err := requireSingleJSONValue(dec); err != nil {
			err := apperror.Validation(err.Error())
			h.audit(r, adminapp.AuditCachePurgeURL, "", nil, err)
			httpshared.JSONError(w, err)
			return
		}

		role := auth.IdentityFrom(r.Context()).Role
		if !h.checkPermission(role, rbac.CachePurgeURL) {
			err := apperror.Forbidden("cache.purge_url permission required")
			h.audit(r, adminapp.AuditCachePurgeURL, body.Path, map[string]interface{}{"path": body.Path}, err)
			httpshared.JSONError(w, err)
			return
		}

		var varies []cacheapp.StoreVary
		if h.stores != nil {
			all, err := h.stores.FindAll(r.Context())
			if err != nil {
				err := apperror.Wrap(apperror.CodeInternal, "list stores for purge-url failed", err)
				h.audit(r, adminapp.AuditCachePurgeURL, body.Path, map[string]interface{}{"path": body.Path}, err)
				httpshared.JSONError(w, err)
				return
			}
			varies = make([]cacheapp.StoreVary, 0, len(all))
			for _, st := range all {
				varies = append(varies, cacheapp.StoreVary{
					ID:       st.ID,
					Language: st.Language,
					Currency: st.Currency,
				})
			}
		}

		result, err := h.svc.PurgeURL(r.Context(), body.Path, varies)
		details := map[string]interface{}{
			"path":    result.Path,
			"deleted": result.Deleted,
			"keys":    len(result.Keys),
		}
		if err != nil {
			h.audit(r, adminapp.AuditCachePurgeURL, body.Path, details, err)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r, adminapp.AuditCachePurgeURL, result.Path, details, nil)
		httpshared.JSON(w, http.StatusOK, result)
	}
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
