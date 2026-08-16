package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// CouponAdminHandler serves coupon admin endpoints.
type CouponAdminHandler struct {
	coupons    promotion.CouponRepository
	promotions promotion.PromotionRepository
	auditor    *admin.Auditor
}

// NewCouponAdminHandler creates a CouponAdminHandler with a default auditor.
func NewCouponAdminHandler(coupons promotion.CouponRepository, promotions promotion.PromotionRepository) *CouponAdminHandler {
	return NewCouponAdminHandlerWithAuditor(coupons, promotions, admin.NewAuditor(logger.New("info")))
}

// NewCouponAdminHandlerWithAuditor creates a CouponAdminHandler with a custom auditor.
func NewCouponAdminHandlerWithAuditor(coupons promotion.CouponRepository, promotions promotion.PromotionRepository, auditor *admin.Auditor) *CouponAdminHandler {
	if coupons == nil {
		panic("CouponAdminHandler: coupons repository must not be nil")
	}
	if promotions == nil {
		panic("CouponAdminHandler: promotions repository must not be nil")
	}
	if auditor == nil {
		panic("CouponAdminHandler: auditor must not be nil")
	}
	return &CouponAdminHandler{coupons: coupons, promotions: promotions, auditor: auditor}
}

type createCouponRequest struct {
	Code        string `json:"code"`
	PromotionID string `json:"promotion_id"`
	UsageLimit  int    `json:"usage_limit"`
	Active      *bool  `json:"active"`
}

type updateCouponRequest struct {
	Code        *string `json:"code"`
	PromotionID *string `json:"promotion_id"`
	UsageLimit  *int    `json:"usage_limit"`
	Active      *bool   `json:"active"`
}

type adminCouponResponse struct {
	ID            string `json:"id"`
	Code          string `json:"code"`
	PromotionID   string `json:"promotion_id"`
	PromotionName string `json:"promotion_name"`
	UsageLimit    int    `json:"usage_limit"`
	UsageCount    int    `json:"usage_count"`
	Active        bool   `json:"active"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func normalizeCouponCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func (h *CouponAdminHandler) audit(r *http.Request, action admin.AuditAction, resourceID string, details map[string]interface{}, err error) {
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
		ResourceType: "coupon",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

func (h *CouponAdminHandler) promotionName(ctx context.Context, promotionID string) string {
	if promotionID == "" {
		return ""
	}
	p, err := h.promotions.FindByID(ctx, promotionID)
	if err != nil || p == nil {
		return ""
	}
	return p.Name
}

func (h *CouponAdminHandler) buildPromotionNameCache(ctx context.Context, coupons []promotion.Coupon) map[string]string {
	names := make(map[string]string)
	seen := make(map[string]struct{}, len(coupons))
	for i := range coupons {
		promotionID := coupons[i].PromotionID
		if promotionID == "" {
			continue
		}
		if _, ok := seen[promotionID]; ok {
			continue
		}
		seen[promotionID] = struct{}{}
		names[promotionID] = h.promotionName(ctx, promotionID)
	}
	return names
}

func (h *CouponAdminHandler) toResponse(c *promotion.Coupon, promotionNames map[string]string) adminCouponResponse {
	return adminCouponResponse{
		ID:            c.ID,
		Code:          c.Code,
		PromotionID:   c.PromotionID,
		PromotionName: promotionNames[c.PromotionID],
		UsageLimit:    c.UsageLimit,
		UsageCount:    c.UsageCount,
		Active:        c.Active,
		CreatedAt:     c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     c.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (h *CouponAdminHandler) validatePromotion(ctx context.Context, promotionID string) error {
	if promotionID == "" {
		return apperror.Validation("promotion id is required")
	}
	p, err := h.promotions.FindByID(ctx, promotionID)
	if err != nil {
		return err
	}
	if p == nil {
		return apperror.NotFound("promotion not found")
	}
	return nil
}

func (h *CouponAdminHandler) ensureUniqueCode(ctx context.Context, code, excludeID string) error {
	existing, err := h.coupons.FindByCode(ctx, code)
	if err != nil {
		return err
	}
	if existing != nil && existing.ID != excludeID {
		return apperror.Validation("coupon code already exists")
	}
	return nil
}

// List handles GET /api/v1/admin/coupons.
func (h *CouponAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := ParsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}

		coupons, err := h.coupons.List(r.Context(), offset, limit)
		if err != nil {
			JSONError(w, err)
			return
		}

		result := make([]adminCouponResponse, 0, len(coupons))
		promotionNames := h.buildPromotionNameCache(r.Context(), coupons)
		for i := range coupons {
			result = append(result, h.toResponse(&coupons[i], promotionNames))
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"coupons": result,
		})
	}
}

// Get handles GET /api/v1/admin/coupons/{id}.
func (h *CouponAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		couponID := r.PathValue("id")
		if couponID == "" {
			verr := apperror.Validation("coupon id is required")
			h.audit(r, admin.AuditCouponRead, "", nil, verr)
			JSONError(w, verr)
			return
		}

		coupon, err := h.coupons.FindByID(r.Context(), couponID)
		if err != nil {
			h.audit(r, admin.AuditCouponRead, couponID, nil, err)
			JSONError(w, err)
			return
		}
		if coupon == nil {
			nf := apperror.NotFound("coupon not found")
			h.audit(r, admin.AuditCouponRead, couponID, nil, nf)
			JSONError(w, nf)
			return
		}

		h.audit(r, admin.AuditCouponRead, couponID, map[string]interface{}{"code": coupon.Code}, nil)
		names := map[string]string{coupon.PromotionID: h.promotionName(r.Context(), coupon.PromotionID)}
		JSON(w, http.StatusOK, map[string]interface{}{
			"coupon": h.toResponse(coupon, names),
		})
	}
}

// Create handles POST /api/v1/admin/coupons.
func (h *CouponAdminHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createCouponRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			verr := apperror.Validation("invalid request body")
			h.audit(r, admin.AuditCouponCreate, "", nil, verr)
			JSONError(w, verr)
			return
		}

		code := normalizeCouponCode(req.Code)
		if err := h.validatePromotion(r.Context(), strings.TrimSpace(req.PromotionID)); err != nil {
			h.audit(r, admin.AuditCouponCreate, "", nil, err)
			JSONError(w, err)
			return
		}
		if req.UsageLimit < 0 {
			verr := apperror.Validation("usage limit must be zero or greater")
			h.audit(r, admin.AuditCouponCreate, "", nil, verr)
			JSONError(w, verr)
			return
		}
		if err := h.ensureUniqueCode(r.Context(), code, ""); err != nil {
			h.audit(r, admin.AuditCouponCreate, "", nil, err)
			JSONError(w, err)
			return
		}

		coupon, err := promotion.NewCoupon(id.New(), code, strings.TrimSpace(req.PromotionID))
		if err != nil {
			verr := apperror.Validation(err.Error())
			h.audit(r, admin.AuditCouponCreate, "", nil, verr)
			JSONError(w, verr)
			return
		}
		coupon.UsageLimit = req.UsageLimit
		if req.Active != nil {
			coupon.Active = *req.Active
		}

		if err := h.coupons.Save(r.Context(), &coupon); err != nil {
			h.audit(r, admin.AuditCouponCreate, coupon.ID, nil, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditCouponCreate, coupon.ID, map[string]interface{}{"code": coupon.Code}, nil)
		names := map[string]string{coupon.PromotionID: h.promotionName(r.Context(), coupon.PromotionID)}
		JSON(w, http.StatusCreated, map[string]interface{}{
			"coupon": h.toResponse(&coupon, names),
		})
	}
}

// Update handles PUT /api/v1/admin/coupons/{id}.
func (h *CouponAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		couponID := r.PathValue("id")
		if couponID == "" {
			verr := apperror.Validation("coupon id is required")
			h.audit(r, admin.AuditCouponUpdate, "", nil, verr)
			JSONError(w, verr)
			return
		}

		var req updateCouponRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			verr := apperror.Validation("invalid request body")
			h.audit(r, admin.AuditCouponUpdate, couponID, nil, verr)
			JSONError(w, verr)
			return
		}

		coupon, err := h.coupons.FindByID(r.Context(), couponID)
		if err != nil {
			h.audit(r, admin.AuditCouponUpdate, couponID, nil, err)
			JSONError(w, err)
			return
		}
		if coupon == nil {
			nf := apperror.NotFound("coupon not found")
			h.audit(r, admin.AuditCouponUpdate, couponID, nil, nf)
			JSONError(w, nf)
			return
		}

		if req.Code != nil {
			code := normalizeCouponCode(*req.Code)
			if err := h.ensureUniqueCode(r.Context(), code, couponID); err != nil {
				h.audit(r, admin.AuditCouponUpdate, couponID, nil, err)
				JSONError(w, err)
				return
			}
			if !strings.EqualFold(coupon.Code, code) {
				rebuilt, err := promotion.NewCoupon(coupon.ID, code, coupon.PromotionID)
				if err != nil {
					verr := apperror.Validation(err.Error())
					h.audit(r, admin.AuditCouponUpdate, couponID, nil, verr)
					JSONError(w, verr)
					return
				}
				coupon.Code = rebuilt.Code
			}
		}
		if req.PromotionID != nil {
			promotionID := strings.TrimSpace(*req.PromotionID)
			if err := h.validatePromotion(r.Context(), promotionID); err != nil {
				h.audit(r, admin.AuditCouponUpdate, couponID, nil, err)
				JSONError(w, err)
				return
			}
			coupon.PromotionID = promotionID
		}
		if req.UsageLimit != nil {
			if *req.UsageLimit < 0 {
				verr := apperror.Validation("usage limit must be zero or greater")
				h.audit(r, admin.AuditCouponUpdate, couponID, nil, verr)
				JSONError(w, verr)
				return
			}
			coupon.UsageLimit = *req.UsageLimit
		}
		if req.Active != nil {
			coupon.Active = *req.Active
		}
		coupon.UpdatedAt = time.Now().UTC()

		if err := h.coupons.Save(r.Context(), coupon); err != nil {
			h.audit(r, admin.AuditCouponUpdate, couponID, nil, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditCouponUpdate, couponID, map[string]interface{}{"code": coupon.Code}, nil)
		names := map[string]string{coupon.PromotionID: h.promotionName(r.Context(), coupon.PromotionID)}
		JSON(w, http.StatusOK, map[string]interface{}{
			"coupon": h.toResponse(coupon, names),
		})
	}
}

// Delete handles DELETE /api/v1/admin/coupons/{id}.
func (h *CouponAdminHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		couponID := r.PathValue("id")
		if couponID == "" {
			verr := apperror.Validation("coupon id is required")
			h.audit(r, admin.AuditCouponDelete, "", nil, verr)
			JSONError(w, verr)
			return
		}

		coupon, err := h.coupons.FindByID(r.Context(), couponID)
		if err != nil {
			h.audit(r, admin.AuditCouponDelete, couponID, nil, err)
			JSONError(w, err)
			return
		}
		if coupon == nil {
			nf := apperror.NotFound("coupon not found")
			h.audit(r, admin.AuditCouponDelete, couponID, nil, nf)
			JSONError(w, nf)
			return
		}

		if err := h.coupons.Delete(r.Context(), couponID); err != nil {
			h.audit(r, admin.AuditCouponDelete, couponID, nil, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditCouponDelete, couponID, map[string]interface{}{"code": coupon.Code}, nil)
		JSON(w, http.StatusOK, map[string]interface{}{
			"deleted": true,
		})
	}
}
