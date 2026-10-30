package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	downloadApp "github.com/akarso/shopanda/internal/application/download"
	domainDownload "github.com/akarso/shopanda/internal/domain/download"
	"github.com/akarso/shopanda/internal/domain/order"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// DownloadGrantAdminHandler lists, revokes, and re-issues DownloadGrant rows
// for support (PR-1078). Reads use downloads.read; writes use downloads.write.
type DownloadGrantAdminHandler struct {
	svc     *downloadApp.Service
	orders  order.OrderRepository
	auditor *adminapp.Auditor
	now     func() time.Time
}

// NewDownloadGrantAdminHandler creates a DownloadGrantAdminHandler.
func NewDownloadGrantAdminHandler(svc *downloadApp.Service, orders order.OrderRepository, auditor *adminapp.Auditor) *DownloadGrantAdminHandler {
	if svc == nil {
		panic("http: download grant handler: service must not be nil")
	}
	if orders == nil {
		panic("http: download grant handler: order repository must not be nil")
	}
	if auditor == nil {
		panic("http: download grant handler: auditor must not be nil")
	}
	return &DownloadGrantAdminHandler{
		svc:     svc,
		orders:  orders,
		auditor: auditor,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

type downloadGrantDTO struct {
	ID            string `json:"id"`
	OrderItemID   string `json:"order_item_id"`
	CustomerID    string `json:"customer_id"`
	AssetID       string `json:"asset_id"`
	ExpiresAt     string `json:"expires_at"`
	DownloadLimit int    `json:"download_limit"`
	DownloadCount int    `json:"download_count"`
	CreatedAt     string `json:"created_at"`
	Expired       bool   `json:"expired"`
	Exhausted     bool   `json:"exhausted"`
}

type revokeDownloadGrantRequest struct {
	Reason string `json:"reason"`
}

type reissueDownloadGrantRequest struct {
	ExpiresAt     string `json:"expires_at"`
	ExpiryDays    *int   `json:"expiry_days"`
	DownloadLimit *int   `json:"download_limit"`
	ResetCount    *bool  `json:"reset_count"`
	Reason        string `json:"reason"`
}

func toDownloadGrantDTO(g *domainDownload.DownloadGrant, now time.Time) downloadGrantDTO {
	if g == nil {
		return downloadGrantDTO{}
	}
	return downloadGrantDTO{
		ID:            g.ID,
		OrderItemID:   g.OrderItemID,
		CustomerID:    g.CustomerID,
		AssetID:       g.AssetID,
		ExpiresAt:     g.ExpiresAt.UTC().Format(time.RFC3339),
		DownloadLimit: g.DownloadLimit,
		DownloadCount: g.DownloadCount,
		CreatedAt:     g.CreatedAt.UTC().Format(time.RFC3339),
		Expired:       g.Expired(now),
		Exhausted:     g.Exhausted(),
	}
}

func (h *DownloadGrantAdminHandler) audit(r *http.Request, action adminapp.AuditAction, grantID string, details map[string]interface{}, err error) {
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
		ResourceType: "download_grant",
		ResourceID:   grantID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

// ListByCustomer handles GET /api/v1/admin/download-grants?customer_id=.
// Successful GETs are not audited (writes only, like assignments).
func (h *DownloadGrantAdminHandler) ListByCustomer() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := strings.TrimSpace(r.URL.Query().Get("customer_id"))
		if customerID == "" {
			httpshared.JSONError(w, apperror.Validation("customer_id is required"))
			return
		}
		rows, err := h.svc.ListByCustomer(r.Context(), customerID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		now := h.now()
		out := make([]downloadGrantDTO, 0, len(rows))
		for i := range rows {
			out = append(out, toDownloadGrantDTO(&rows[i], now))
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"grants": out})
	}
}

// ListByOrder handles GET /api/v1/admin/orders/{orderId}/download-grants.
// Walks order lines and lists grants per order_item_id (orderID/variantID).
func (h *DownloadGrantAdminHandler) ListByOrder() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := strings.TrimSpace(r.PathValue("orderId"))
		if orderID == "" {
			httpshared.JSONError(w, apperror.Validation("order id must not be empty"))
			return
		}
		ord, err := h.orders.FindByID(r.Context(), orderID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		if ord == nil {
			httpshared.JSONError(w, apperror.NotFound("order not found"))
			return
		}
		now := h.now()
		out := make([]downloadGrantDTO, 0)
		seen := map[string]struct{}{}
		for _, item := range ord.Items() {
			unit := domainDownload.OrderItemID(ord.ID, item.VariantID)
			if unit == "" {
				continue
			}
			if _, ok := seen[unit]; ok {
				continue
			}
			seen[unit] = struct{}{}
			rows, listErr := h.svc.ListByOrderItem(r.Context(), unit)
			if listErr != nil {
				httpshared.JSONError(w, listErr)
				return
			}
			for i := range rows {
				out = append(out, toDownloadGrantDTO(&rows[i], now))
			}
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"grants": out})
	}
}

// ListByOrderItem handles GET /api/v1/admin/download-grants?order_item_id=.
func (h *DownloadGrantAdminHandler) ListByOrderItem() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderItemID := strings.TrimSpace(r.URL.Query().Get("order_item_id"))
		if orderItemID == "" {
			httpshared.JSONError(w, apperror.Validation("order_item_id is required"))
			return
		}
		rows, err := h.svc.ListByOrderItem(r.Context(), orderItemID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		now := h.now()
		out := make([]downloadGrantDTO, 0, len(rows))
		for i := range rows {
			out = append(out, toDownloadGrantDTO(&rows[i], now))
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"grants": out})
	}
}

// List handles GET /api/v1/admin/download-grants with customer_id or order_item_id.
func (h *DownloadGrantAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.URL.Query().Get("customer_id")) != "" {
			h.ListByCustomer()(w, r)
			return
		}
		if strings.TrimSpace(r.URL.Query().Get("order_item_id")) != "" {
			h.ListByOrderItem()(w, r)
			return
		}
		httpshared.JSONError(w, apperror.Validation("customer_id or order_item_id is required"))
	}
}

// Revoke handles POST /api/v1/admin/download-grants/{id}/revoke.
// Idempotent when already expired. Reason is required for audit.
func (h *DownloadGrantAdminHandler) Revoke() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		grantID := strings.TrimSpace(r.PathValue("id"))
		var req revokeDownloadGrantRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && r.ContentLength != 0 {
			err = apperror.Validation("invalid request body")
			h.audit(r, adminapp.AuditDownloadGrantRevoke, grantID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		reason := strings.TrimSpace(req.Reason)
		if reason == "" {
			err := apperror.Validation("reason is required")
			h.audit(r, adminapp.AuditDownloadGrantRevoke, grantID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		g, idempotent, err := h.svc.Revoke(r.Context(), grantID, reason)
		details := map[string]interface{}{"reason": reason, "idempotent": idempotent}
		if err != nil {
			h.audit(r, adminapp.AuditDownloadGrantRevoke, grantID, details, err)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r, adminapp.AuditDownloadGrantRevoke, grantID, details, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"grant":      toDownloadGrantDTO(g, h.now()),
			"idempotent": idempotent,
		})
	}
}

// Reissue handles POST /api/v1/admin/download-grants/{id}/reissue.
// Extends expiry and by default resets download_count. Reason is required.
func (h *DownloadGrantAdminHandler) Reissue() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		grantID := strings.TrimSpace(r.PathValue("id"))
		var req reissueDownloadGrantRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			err = apperror.Validation("invalid request body")
			h.audit(r, adminapp.AuditDownloadGrantReissue, grantID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		reason := strings.TrimSpace(req.Reason)
		if reason == "" {
			err := apperror.Validation("reason is required")
			h.audit(r, adminapp.AuditDownloadGrantReissue, grantID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		expiresAt, err := resolveReissueExpiry(req, h.now())
		if err != nil {
			h.audit(r, adminapp.AuditDownloadGrantReissue, grantID, map[string]interface{}{"reason": reason}, err)
			httpshared.JSONError(w, err)
			return
		}
		resetCount := true
		if req.ResetCount != nil {
			resetCount = *req.ResetCount
		}
		g, err := h.svc.Reissue(r.Context(), grantID, expiresAt, req.DownloadLimit, resetCount, reason)
		details := map[string]interface{}{
			"reason":         reason,
			"expires_at":     expiresAt.UTC().Format(time.RFC3339),
			"reset_count":    resetCount,
			"download_limit": req.DownloadLimit,
		}
		if err != nil {
			h.audit(r, adminapp.AuditDownloadGrantReissue, grantID, details, err)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r, adminapp.AuditDownloadGrantReissue, grantID, details, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"grant": toDownloadGrantDTO(g, h.now()),
		})
	}
}

func resolveReissueExpiry(req reissueDownloadGrantRequest, now time.Time) (time.Time, error) {
	now = now.UTC()
	expiresRaw := strings.TrimSpace(req.ExpiresAt)
	if expiresRaw != "" {
		t, err := time.Parse(time.RFC3339, expiresRaw)
		if err != nil {
			return time.Time{}, apperror.Validation("expires_at must be RFC3339")
		}
		t = t.UTC()
		if !t.After(now) {
			return time.Time{}, apperror.Validation("expires_at must be strictly after now")
		}
		return t, nil
	}
	if req.ExpiryDays != nil {
		days := *req.ExpiryDays
		if days < 1 {
			return time.Time{}, apperror.Validation("expiry_days must be at least 1")
		}
		if days > domainDownload.MaxExpiryDays {
			return time.Time{}, apperror.Validation("expiry_days must not exceed 3650")
		}
		t := now.Add(time.Duration(days) * 24 * time.Hour)
		if !t.After(now) {
			return time.Time{}, apperror.Validation("expires_at must be strictly after now")
		}
		return t, nil
	}
	return time.Time{}, apperror.Validation("expires_at or expiry_days is required")
}
