package http

import (
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

// PromotionAdminHandler serves promotion admin endpoints.
type PromotionAdminHandler struct {
	promotions promotion.PromotionRepository
	auditor    *admin.Auditor
}

// NewPromotionAdminHandler creates a PromotionAdminHandler with a default auditor.
func NewPromotionAdminHandler(promotions promotion.PromotionRepository) *PromotionAdminHandler {
	return NewPromotionAdminHandlerWithAuditor(promotions, admin.NewAuditor(logger.New("info")))
}

// NewPromotionAdminHandlerWithAuditor creates a PromotionAdminHandler with a custom auditor.
func NewPromotionAdminHandlerWithAuditor(promotions promotion.PromotionRepository, auditor *admin.Auditor) *PromotionAdminHandler {
	if promotions == nil {
		panic("PromotionAdminHandler: promotions repository must not be nil")
	}
	if auditor == nil {
		panic("PromotionAdminHandler: auditor must not be nil")
	}
	return &PromotionAdminHandler{promotions: promotions, auditor: auditor}
}

type promotionWriteRequest struct {
	Name             string                    `json:"name"`
	Type             string                    `json:"type"`
	Priority         *int                      `json:"priority"`
	Active           *bool                     `json:"active"`
	CouponBound      *bool                     `json:"coupon_bound"`
	StartAt          *string                   `json:"start_at"`
	EndAt            *string                   `json:"end_at"`
	ConditionType    string                    `json:"condition_type"`
	ConditionValue   int                       `json:"condition_value"`
	ActionType       string                    `json:"action_type"`
	ActionPercentage int                       `json:"action_percentage"`
	ActionAmount     int64                     `json:"action_amount"`
	ActionTiers      []admin.PromotionTierForm `json:"action_tiers"`
	ActionBuyQty     int                       `json:"action_buy_qty"`
	ActionGetQty     int                       `json:"action_get_qty"`
	RulesMode        string                    `json:"rules_mode"`
	Conditions       json.RawMessage           `json:"conditions"`
	Actions          json.RawMessage           `json:"actions"`
}

type adminPromotionTierResponse struct {
	MinQty     int `json:"min_qty"`
	Percentage int `json:"percentage"`
}

type adminPromotionResponse struct {
	ID               string                       `json:"id"`
	Name             string                       `json:"name"`
	Type             string                       `json:"type"`
	Priority         int                          `json:"priority"`
	Active           bool                         `json:"active"`
	CouponBound      bool                         `json:"coupon_bound"`
	StartAt          string                       `json:"start_at,omitempty"`
	EndAt            string                       `json:"end_at,omitempty"`
	ConditionType    string                       `json:"condition_type"`
	ConditionValue   int                          `json:"condition_value"`
	ActionType       string                       `json:"action_type"`
	ActionPercentage int                          `json:"action_percentage"`
	ActionAmount     int64                        `json:"action_amount"`
	ActionTiers      []adminPromotionTierResponse `json:"action_tiers,omitempty"`
	ActionBuyQty     int                          `json:"action_buy_qty,omitempty"`
	ActionGetQty     int                          `json:"action_get_qty,omitempty"`
	Conditions       json.RawMessage              `json:"conditions,omitempty"`
	Actions          json.RawMessage              `json:"actions,omitempty"`
	CreatedAt        string                       `json:"created_at"`
	UpdatedAt        string                       `json:"updated_at"`
}

func (h *PromotionAdminHandler) audit(r *http.Request, action admin.AuditAction, resourceID string, details map[string]interface{}, err error) {
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
		ResourceType: "promotion",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseOptionalTime(raw *string) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return nil, apperror.Validation("invalid datetime format; use RFC3339")
	}
	utc := parsed.UTC()
	return &utc, nil
}

func (h *PromotionAdminHandler) toResponse(p *promotion.Promotion) (adminPromotionResponse, error) {
	rules, err := admin.DecodePromotionRules(p.Type, p.Conditions, p.Actions)
	if err != nil {
		return adminPromotionResponse{}, apperror.Validation(err.Error())
	}
	return adminPromotionResponse{
		ID:               p.ID,
		Name:             p.Name,
		Type:             string(p.Type),
		Priority:         p.Priority,
		Active:           p.Active,
		CouponBound:      p.CouponBound,
		StartAt:          formatOptionalTime(p.StartAt),
		EndAt:            formatOptionalTime(p.EndAt),
		ConditionType:    rules.ConditionType,
		ConditionValue:   rules.ConditionValue,
		ActionType:       rules.ActionType,
		ActionPercentage: rules.ActionPercentage,
		ActionAmount:     rules.ActionAmount,
		ActionTiers:      toAdminPromotionTierResponses(rules.ActionTiers),
		ActionBuyQty:     rules.ActionBuyQty,
		ActionGetQty:     rules.ActionGetQty,
		Conditions:       cloneRawJSON(p.Conditions),
		Actions:          cloneRawJSON(p.Actions),
		CreatedAt:        p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        p.UpdatedAt.UTC().Format(time.RFC3339),
	}, nil
}

func toAdminPromotionTierResponses(tiers []admin.PromotionTierForm) []adminPromotionTierResponse {
	if len(tiers) == 0 {
		return nil
	}
	out := make([]adminPromotionTierResponse, len(tiers))
	for i, tier := range tiers {
		out[i] = adminPromotionTierResponse{
			MinQty:     tier.MinQty,
			Percentage: tier.Percentage,
		}
	}
	return out
}

func cloneRawJSON(data []byte) json.RawMessage {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	out := make(json.RawMessage, len(data))
	copy(out, data)
	return out
}

func applyAdvancedRules(p *promotion.Promotion, req promotionWriteRequest) error {
	if len(req.Conditions) == 0 || len(req.Actions) == 0 {
		return apperror.Validation("advanced rules require conditions and actions JSON")
	}
	if !json.Valid(req.Conditions) || !json.Valid(req.Actions) {
		return apperror.Validation("conditions and actions must be valid JSON")
	}
	rules, err := admin.DecodePromotionRules(p.Type, req.Conditions, req.Actions)
	if err != nil {
		return apperror.Validation(err.Error())
	}
	conditions, actions, err := admin.EncodePromotionRules(p.Type, rules)
	if err != nil {
		return apperror.Validation(err.Error())
	}
	p.Conditions = conditions
	p.Actions = actions
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func applyGuidedRules(p *promotion.Promotion, req promotionWriteRequest, mergeRules bool) error {
	if mergeRules && req.ConditionType == "" && req.ActionType == "" {
		p.UpdatedAt = time.Now().UTC()
		return nil
	}

	rules := admin.PromotionRuleForm{
		ConditionType:    req.ConditionType,
		ConditionValue:   req.ConditionValue,
		ActionType:       req.ActionType,
		ActionPercentage: req.ActionPercentage,
		ActionAmount:     req.ActionAmount,
		ActionTiers:      append([]admin.PromotionTierForm(nil), req.ActionTiers...),
		ActionBuyQty:     req.ActionBuyQty,
		ActionGetQty:     req.ActionGetQty,
	}
	if mergeRules {
		existing, err := admin.DecodePromotionRules(p.Type, p.Conditions, p.Actions)
		if err != nil {
			return apperror.Validation(err.Error())
		}
		if req.ConditionType == "" {
			rules.ConditionType = existing.ConditionType
			rules.ConditionValue = existing.ConditionValue
		}
		if req.ActionType == "" {
			rules.ActionType = existing.ActionType
			rules.ActionPercentage = existing.ActionPercentage
			rules.ActionAmount = existing.ActionAmount
			rules.ActionTiers = existing.ActionTiers
			rules.ActionBuyQty = existing.ActionBuyQty
			rules.ActionGetQty = existing.ActionGetQty
		}
	}
	conditions, actions, err := admin.EncodePromotionRules(p.Type, rules)
	if err != nil {
		return apperror.Validation(err.Error())
	}
	p.Conditions = conditions
	p.Actions = actions
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (h *PromotionAdminHandler) applyWriteRequest(p *promotion.Promotion, req promotionWriteRequest, mergeRules bool) error {
	if req.Name != "" {
		name := strings.TrimSpace(req.Name)
		if name == "" {
			return apperror.Validation("promotion name must not be empty")
		}
		p.Name = name
	}
	if req.Type != "" {
		typ := promotion.PromotionType(strings.TrimSpace(req.Type))
		if !typ.IsValid() {
			return apperror.Validation("promotion type must be catalog or cart")
		}
		p.Type = typ
	}
	if req.Priority != nil {
		p.Priority = *req.Priority
	}
	if req.Active != nil {
		p.Active = *req.Active
	}
	if req.CouponBound != nil {
		p.CouponBound = *req.CouponBound
	}
	if req.StartAt != nil {
		startAt, err := parseOptionalTime(req.StartAt)
		if err != nil {
			return err
		}
		p.StartAt = startAt
	}
	if req.EndAt != nil {
		endAt, err := parseOptionalTime(req.EndAt)
		if err != nil {
			return err
		}
		p.EndAt = endAt
	}
	if p.StartAt != nil && p.EndAt != nil && p.StartAt.After(*p.EndAt) {
		return apperror.Validation("start_at must be before end_at")
	}

	if strings.EqualFold(strings.TrimSpace(req.RulesMode), "advanced") {
		return applyAdvancedRules(p, req)
	}

	return applyGuidedRules(p, req, mergeRules)
}

// List handles GET /api/v1/admin/promotions.
func (h *PromotionAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := parsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}

		promotions, err := h.promotions.List(r.Context(), offset, limit)
		if err != nil {
			JSONError(w, err)
			return
		}

		result := make([]adminPromotionResponse, 0, len(promotions))
		for i := range promotions {
			resp, err := h.toResponse(&promotions[i])
			if err != nil {
				JSONError(w, err)
				return
			}
			result = append(result, resp)
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"promotions": result,
		})
	}
}

// Get handles GET /api/v1/admin/promotions/{id}.
func (h *PromotionAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		promotionID := r.PathValue("id")
		if promotionID == "" {
			verr := apperror.Validation("promotion id is required")
			h.audit(r, admin.AuditPromotionRead, "", nil, verr)
			JSONError(w, verr)
			return
		}

		p, err := h.promotions.FindByID(r.Context(), promotionID)
		if err != nil {
			h.audit(r, admin.AuditPromotionRead, promotionID, nil, err)
			JSONError(w, err)
			return
		}
		if p == nil {
			nf := apperror.NotFound("promotion not found")
			h.audit(r, admin.AuditPromotionRead, promotionID, nil, nf)
			JSONError(w, nf)
			return
		}

		resp, err := h.toResponse(p)
		if err != nil {
			h.audit(r, admin.AuditPromotionRead, promotionID, nil, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditPromotionRead, promotionID, map[string]interface{}{"name": p.Name}, nil)
		JSON(w, http.StatusOK, map[string]interface{}{
			"promotion": resp,
		})
	}
}

// Create handles POST /api/v1/admin/promotions.
func (h *PromotionAdminHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req promotionWriteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			verr := apperror.Validation("invalid request body")
			h.audit(r, admin.AuditPromotionCreate, "", nil, verr)
			JSONError(w, verr)
			return
		}

		typ := promotion.PromotionType(strings.TrimSpace(req.Type))
		if !typ.IsValid() {
			verr := apperror.Validation("promotion type must be catalog or cart")
			h.audit(r, admin.AuditPromotionCreate, "", nil, verr)
			JSONError(w, verr)
			return
		}

		p, err := promotion.NewPromotion(id.New(), strings.TrimSpace(req.Name), typ)
		if err != nil {
			verr := apperror.Validation(err.Error())
			h.audit(r, admin.AuditPromotionCreate, "", nil, verr)
			JSONError(w, verr)
			return
		}
		if req.Priority != nil {
			p.Priority = *req.Priority
		}
		if req.Active != nil {
			p.Active = *req.Active
		}
		if req.CouponBound != nil {
			p.CouponBound = *req.CouponBound
		}

		if err := h.applyWriteRequest(&p, req, false); err != nil {
			h.audit(r, admin.AuditPromotionCreate, "", nil, err)
			JSONError(w, err)
			return
		}

		if err := h.promotions.Save(r.Context(), &p); err != nil {
			h.audit(r, admin.AuditPromotionCreate, p.ID, nil, err)
			JSONError(w, err)
			return
		}

		resp, err := h.toResponse(&p)
		if err != nil {
			h.audit(r, admin.AuditPromotionCreate, p.ID, nil, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditPromotionCreate, p.ID, map[string]interface{}{"name": p.Name, "type": p.Type}, nil)
		JSON(w, http.StatusCreated, map[string]interface{}{
			"promotion": resp,
		})
	}
}

// Update handles PUT /api/v1/admin/promotions/{id}.
func (h *PromotionAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		promotionID := r.PathValue("id")
		if promotionID == "" {
			verr := apperror.Validation("promotion id is required")
			h.audit(r, admin.AuditPromotionUpdate, "", nil, verr)
			JSONError(w, verr)
			return
		}

		var req promotionWriteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			verr := apperror.Validation("invalid request body")
			h.audit(r, admin.AuditPromotionUpdate, promotionID, nil, verr)
			JSONError(w, verr)
			return
		}

		p, err := h.promotions.FindByID(r.Context(), promotionID)
		if err != nil {
			h.audit(r, admin.AuditPromotionUpdate, promotionID, nil, err)
			JSONError(w, err)
			return
		}
		if p == nil {
			nf := apperror.NotFound("promotion not found")
			h.audit(r, admin.AuditPromotionUpdate, promotionID, nil, nf)
			JSONError(w, nf)
			return
		}

		if err := h.applyWriteRequest(p, req, true); err != nil {
			h.audit(r, admin.AuditPromotionUpdate, promotionID, nil, err)
			JSONError(w, err)
			return
		}

		if err := h.promotions.Save(r.Context(), p); err != nil {
			h.audit(r, admin.AuditPromotionUpdate, promotionID, nil, err)
			JSONError(w, err)
			return
		}

		resp, err := h.toResponse(p)
		if err != nil {
			h.audit(r, admin.AuditPromotionUpdate, promotionID, nil, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditPromotionUpdate, promotionID, map[string]interface{}{"name": p.Name}, nil)
		JSON(w, http.StatusOK, map[string]interface{}{
			"promotion": resp,
		})
	}
}

// Delete handles DELETE /api/v1/admin/promotions/{id}.
func (h *PromotionAdminHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		promotionID := r.PathValue("id")
		if promotionID == "" {
			verr := apperror.Validation("promotion id is required")
			h.audit(r, admin.AuditPromotionDelete, "", nil, verr)
			JSONError(w, verr)
			return
		}

		p, err := h.promotions.FindByID(r.Context(), promotionID)
		if err != nil {
			h.audit(r, admin.AuditPromotionDelete, promotionID, nil, err)
			JSONError(w, err)
			return
		}
		if p == nil {
			nf := apperror.NotFound("promotion not found")
			h.audit(r, admin.AuditPromotionDelete, promotionID, nil, nf)
			JSONError(w, nf)
			return
		}

		if err := h.promotions.Delete(r.Context(), promotionID); err != nil {
			h.audit(r, admin.AuditPromotionDelete, promotionID, nil, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditPromotionDelete, promotionID, map[string]interface{}{"name": p.Name}, nil)
		JSON(w, http.StatusOK, map[string]interface{}{
			"deleted": true,
		})
	}
}
