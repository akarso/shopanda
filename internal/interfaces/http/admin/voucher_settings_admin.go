package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	voucherapp "github.com/akarso/shopanda/internal/application/voucher"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domain "github.com/akarso/shopanda/internal/domain/voucher"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// VoucherSettingsAdminHandler manages product voucher_settings (products.write).
type VoucherSettingsAdminHandler struct {
	svc      *voucherapp.Service
	products catalog.ProductRepository
	auditor  *adminapp.Auditor
}

// NewVoucherSettingsAdminHandler creates a settings handler.
func NewVoucherSettingsAdminHandler(svc *voucherapp.Service, products catalog.ProductRepository, auditor *adminapp.Auditor) *VoucherSettingsAdminHandler {
	if svc == nil {
		panic("http: voucher settings handler: service must not be nil")
	}
	if products == nil {
		panic("http: voucher settings handler: products must not be nil")
	}
	if auditor == nil {
		panic("http: voucher settings handler: auditor must not be nil")
	}
	return &VoucherSettingsAdminHandler{svc: svc, products: products, auditor: auditor}
}

type voucherSettingsDTO struct {
	ProductID      string `json:"product_id"`
	Enabled        bool   `json:"enabled"`
	CodesPerUnit   int    `json:"codes_per_unit"`
	MaxRedemptions int    `json:"max_redemptions"`
}

type putVoucherSettingsRequest struct {
	Enabled        *bool `json:"enabled"`
	CodesPerUnit   *int  `json:"codes_per_unit"`
	MaxRedemptions *int  `json:"max_redemptions"`
}

func (h *VoucherSettingsAdminHandler) audit(r *http.Request, action adminapp.AuditAction, productID string, details map[string]interface{}, err error) {
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
		ResourceType: "product",
		ResourceID:   productID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

func (h *VoucherSettingsAdminHandler) requireVirtual(r *http.Request, productID string) (*catalog.Product, error) {
	p, err := h.products.FindByID(r.Context(), productID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, apperror.NotFound("product not found")
	}
	if p.Type != catalog.TypeVirtual {
		return nil, apperror.Validation("voucher settings require a virtual product")
	}
	return p, nil
}

// Get handles GET /api/v1/admin/products/{id}/voucher-settings.
func (h *VoucherSettingsAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		productID := strings.TrimSpace(r.PathValue("id"))
		if _, err := h.requireVirtual(r, productID); err != nil {
			httpshared.JSONError(w, err)
			return
		}
		row, err := h.svc.GetSettings(r.Context(), productID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		if row == nil {
			httpshared.JSON(w, http.StatusOK, voucherSettingsDTO{
				ProductID:      productID,
				Enabled:        false,
				CodesPerUnit:   1,
				MaxRedemptions: 1,
			})
			return
		}
		httpshared.JSON(w, http.StatusOK, voucherSettingsDTO{
			ProductID:      row.ProductID,
			Enabled:        row.Enabled,
			CodesPerUnit:   row.CodesPerUnit,
			MaxRedemptions: row.MaxRedemptions,
		})
	}
}

// Put handles PUT /api/v1/admin/products/{id}/voucher-settings.
func (h *VoucherSettingsAdminHandler) Put() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		productID := strings.TrimSpace(r.PathValue("id"))
		if _, err := h.requireVirtual(r, productID); err != nil {
			httpshared.JSONError(w, err)
			return
		}
		var req putVoucherSettingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid JSON body"))
			return
		}
		if req.Enabled == nil {
			httpshared.JSONError(w, apperror.Validation("enabled is required"))
			return
		}
		// Start disabled so an omitted enabled cannot silently turn issuance on.
		s := domain.Settings{
			ProductID:      productID,
			Enabled:        *req.Enabled,
			CodesPerUnit:   1,
			MaxRedemptions: 1,
		}
		if req.CodesPerUnit != nil {
			s.CodesPerUnit = *req.CodesPerUnit
		}
		if req.MaxRedemptions != nil {
			s.MaxRedemptions = *req.MaxRedemptions
		}
		err := h.svc.UpsertSettings(r.Context(), &s)
		h.audit(r, adminapp.AuditVoucherSettingsPut, productID, map[string]interface{}{
			"enabled": s.Enabled, "codes_per_unit": s.CodesPerUnit, "max_redemptions": s.MaxRedemptions,
		}, err)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, voucherSettingsDTO{
			ProductID:      s.ProductID,
			Enabled:        s.Enabled,
			CodesPerUnit:   s.CodesPerUnit,
			MaxRedemptions: s.MaxRedemptions,
		})
	}
}

// Delete handles DELETE /api/v1/admin/products/{id}/voucher-settings.
func (h *VoucherSettingsAdminHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		productID := strings.TrimSpace(r.PathValue("id"))
		if _, err := h.requireVirtual(r, productID); err != nil {
			httpshared.JSONError(w, err)
			return
		}
		err := h.svc.RemoveSettings(r.Context(), productID)
		h.audit(r, adminapp.AuditVoucherSettingsDelete, productID, nil, err)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
