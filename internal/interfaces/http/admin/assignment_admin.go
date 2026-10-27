package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	assignmentapp "github.com/akarso/shopanda/internal/application/assignment"
	"github.com/akarso/shopanda/internal/domain/assignment"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// AssignmentAdminHandler serves operator list/create/revoke for product
// assignments (PR-1071). Unit ids contain "/" so list and current use
// query parameters. GETs are assignments.read; create and revoke are
// assignments.write. Create and revoke are audited.
type AssignmentAdminHandler struct {
	flow    *assignmentapp.Flow
	svc     *assignmentapp.Service
	auditor *adminapp.Auditor
}

// NewAssignmentAdminHandler creates an AssignmentAdminHandler.
func NewAssignmentAdminHandler(flow *assignmentapp.Flow, svc *assignmentapp.Service, auditor *adminapp.Auditor) *AssignmentAdminHandler {
	if flow == nil {
		panic("http: assignment flow must not be nil")
	}
	if svc == nil {
		panic("http: assignment service must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &AssignmentAdminHandler{flow: flow, svc: svc, auditor: auditor}
}

func (h *AssignmentAdminHandler) audit(r *http.Request, action adminapp.AuditAction, resourceID string, details map[string]interface{}, err error) {
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
		ResourceType: "assignment",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

type assignmentAdminResponse struct {
	ID                  string  `json:"id"`
	ParentOrderItemID   string  `json:"parent_order_item_id,omitempty"`
	ChildOrderItemID    string  `json:"child_order_item_id"`
	Status              string  `json:"status"`
	CreatedAt           string  `json:"created_at"`
	RevokedAt           *string `json:"revoked_at,omitempty"`
	Reason              string  `json:"reason,omitempty"`
	Forced              bool    `json:"forced"`
	ForcedAt            *string `json:"forced_at,omitempty"`
	ChildCustomerID     string  `json:"child_customer_id"`
	ChildOrderStatus    string  `json:"child_order_status"`
	ChildPaymentStatus  string  `json:"child_payment_status"`
	ParentCustomerID    string  `json:"parent_customer_id"`
	ParentOrderStatus   string  `json:"parent_order_status"`
	ParentPaymentStatus string  `json:"parent_payment_status"`
}

type adminAssignRequest struct {
	ChildUnitID  string `json:"child_unit_id"`
	ParentUnitID string `json:"parent_unit_id"`
	Force        bool   `json:"force"`
	Reason       string `json:"reason"`
}

func toAssignmentAdminResponse(a *assignment.ProductAssignment) assignmentAdminResponse {
	resp := assignmentAdminResponse{
		ID:                a.ID,
		ParentOrderItemID: a.ParentOrderItemID,
		ChildOrderItemID:  a.ChildOrderItemID,
		Status:            string(a.Status()),
		CreatedAt:         a.CreatedAt.UTC().Format(time.RFC3339),
	}
	if a.RevokedAt != nil {
		s := a.RevokedAt.UTC().Format(time.RFC3339)
		resp.RevokedAt = &s
	}
	if a.Forced() {
		resp.Forced = true
		s := a.ForcedAt.UTC().Format(time.RFC3339)
		resp.ForcedAt = &s
		resp.Reason = a.ForcedReason
	}
	return resp
}

func applyUnitFacts(resp *assignmentAdminResponse, child, parent assignmentapp.UnitFacts) {
	resp.ChildCustomerID = child.CustomerID
	resp.ChildOrderStatus = child.OrderStatus
	resp.ChildPaymentStatus = child.PaymentStatus
	resp.ParentCustomerID = parent.CustomerID
	resp.ParentOrderStatus = parent.OrderStatus
	resp.ParentPaymentStatus = parent.PaymentStatus
}

func assignmentFactKey(unit string) string {
	unit = strings.TrimSpace(unit)
	if canon, ok := assignment.CanonicalUnitID(unit); ok {
		return canon
	}
	return unit
}

const (
	assignmentFactsRouteList    = "list"
	assignmentFactsRouteCurrent = "current"
	assignmentFactsRouteRevoke  = "revoke"
)

// responsesWithFacts attaches live unit facts. When that read fails, the
// rows are still returned with empty fact fields. List logs one warning
// for the page. Current and revoke log one warning for that assignment.
// The card is not a 500.
func (h *AssignmentAdminHandler) responsesWithFacts(ctx context.Context, route string, rows []assignment.ProductAssignment) []assignmentAdminResponse {
	ids := make([]string, 0, len(rows)*2)
	for i := range rows {
		if rows[i].ChildOrderItemID != "" {
			ids = append(ids, rows[i].ChildOrderItemID)
		}
		if rows[i].ParentOrderItemID != "" {
			ids = append(ids, rows[i].ParentOrderItemID)
		}
	}
	facts, err := h.flow.FactsForUnits(ctx, ids)
	if err != nil {
		h.warnFactsFailed(route, rows, err)
		facts = map[string]assignmentapp.UnitFacts{}
	}
	out := make([]assignmentAdminResponse, 0, len(rows))
	for i := range rows {
		resp := toAssignmentAdminResponse(&rows[i])
		applyUnitFacts(&resp,
			facts[assignmentFactKey(rows[i].ChildOrderItemID)],
			facts[assignmentFactKey(rows[i].ParentOrderItemID)],
		)
		out = append(out, resp)
	}
	return out
}

func (h *AssignmentAdminHandler) warnFactsFailed(route string, rows []assignment.ProductAssignment, err error) {
	fields := map[string]interface{}{
		"route": route,
		"error": err.Error(),
	}
	if len(rows) == 1 {
		fields["assignment_id"] = rows[0].ID
	}
	if route == assignmentFactsRouteList {
		fields["count"] = len(rows)
	}
	h.auditor.Warn("admin.assignment.facts_failed", fields)
}

func canonicalAdminUnit(raw, required, invalid string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", apperror.Validation(required)
	}
	unit, ok := assignment.CanonicalUnitID(raw)
	if !ok {
		return "", apperror.Validation(invalid)
	}
	return unit, nil
}

// ListByParent handles GET /api/v1/admin/assignments?parent_unit_id=.
// status defaults to assigned. limit defaults to 100 and is capped at 100.
func (h *AssignmentAdminHandler) ListByParent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		parentUnit, err := canonicalAdminUnit(
			r.URL.Query().Get("parent_unit_id"),
			"parent_unit_id is required",
			assignmentapp.ParentUnitInvalidMessage,
		)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		q, err := parentListQuery(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		rows, hasMore, err := h.svc.ListByParent(r.Context(), parentUnit, q)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		out := h.responsesWithFacts(r.Context(), assignmentFactsRouteList, rows)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"assignments": out,
			"has_more":    hasMore,
		})
	}
}

// GetCurrent handles GET /api/v1/admin/assignments/current?child_unit_id=.
func (h *AssignmentAdminHandler) GetCurrent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		childUnit, err := canonicalAdminUnit(
			r.URL.Query().Get("child_unit_id"),
			"child_unit_id is required",
			assignmentapp.ChildUnitInvalidMessage,
		)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		a, err := h.svc.CurrentByChild(r.Context(), childUnit)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		if a == nil {
			httpshared.JSON(w, http.StatusOK, map[string]interface{}{"assignment": nil})
			return
		}
		out := h.responsesWithFacts(r.Context(), assignmentFactsRouteCurrent, []assignment.ProductAssignment{*a})
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"assignment": out[0]})
	}
}

// Create handles POST /api/v1/admin/assignments.
func (h *AssignmentAdminHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req adminAssignRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		if strings.TrimSpace(req.ChildUnitID) == "" {
			httpshared.JSONError(w, apperror.Validation("child_unit_id is required"))
			return
		}
		if strings.TrimSpace(req.ParentUnitID) == "" {
			httpshared.JSONError(w, apperror.Validation("parent_unit_id is required"))
			return
		}
		// Reason is stored only when force is set. A long unused reason
		// with force false is not a 422.
		if req.Force && utf8.RuneCountInString(strings.TrimSpace(req.Reason)) > assignment.MaxForcedReasonLen {
			httpshared.JSONError(w, apperror.Validation(assignment.ForcedReasonTooLongMessage))
			return
		}
		attemptedChild := strings.TrimSpace(req.ChildUnitID)
		if unit, ok := assignment.CanonicalUnitID(req.ChildUnitID); ok {
			attemptedChild = unit
		}
		result, err := h.flow.AdminAssign(r.Context(), assignmentapp.AdminAssignRequest{
			ChildUnitID:  req.ChildUnitID,
			ParentUnitID: req.ParentUnitID,
			Force:        req.Force,
			Reason:       req.Reason,
		})
		if err != nil {
			h.audit(r, adminapp.AuditAssignmentCreate, attemptedChild, map[string]interface{}{
				"child_unit_id":  attemptedChild,
				"parent_unit_id": strings.TrimSpace(req.ParentUnitID),
				"force":          req.Force,
				"reason":         strings.TrimSpace(req.Reason),
			}, err)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r, adminapp.AuditAssignmentCreate, result.Assignment.ID, map[string]interface{}{
			"child_unit_id":  result.Assignment.ChildOrderItemID,
			"parent_unit_id": result.Assignment.ParentOrderItemID,
			"force":          req.Force,
			"reason":         result.Reason,
		}, nil)
		status := http.StatusOK
		if result.Created {
			status = http.StatusCreated
		}
		httpshared.JSON(w, status, map[string]interface{}{"assignment": toAssignmentCreateResponse(result)})
	}
}

// Revoke handles POST /api/v1/admin/assignments/{id}/revoke.
func (h *AssignmentAdminHandler) Revoke() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			httpshared.JSONError(w, apperror.Validation("assignment id is required"))
			return
		}
		a, idempotent, err := h.svc.Revoke(r.Context(), id)
		if err != nil {
			h.audit(r, adminapp.AuditAssignmentRevoke, id, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		if !idempotent {
			h.audit(r, adminapp.AuditAssignmentRevoke, a.ID, map[string]interface{}{
				"child_unit_id":  a.ChildOrderItemID,
				"parent_unit_id": a.ParentOrderItemID,
			}, nil)
		}
		out := h.responsesWithFacts(r.Context(), assignmentFactsRouteRevoke, []assignment.ProductAssignment{*a})
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"assignment": out[0]})
	}
}

func toAssignmentCreateResponse(result *assignmentapp.AdminAssignResult) assignmentAdminResponse {
	resp := toAssignmentAdminResponse(result.Assignment)
	resp.ChildCustomerID = result.Child.CustomerID
	resp.ChildOrderStatus = result.Child.OrderStatus
	resp.ChildPaymentStatus = result.Child.PaymentStatus
	resp.ParentCustomerID = result.Parent.CustomerID
	resp.ParentOrderStatus = result.Parent.OrderStatus
	resp.ParentPaymentStatus = result.Parent.PaymentStatus
	return resp
}

func parentListQuery(r *http.Request) (assignmentapp.ParentListQuery, error) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	switch status {
	case "", assignmentapp.ParentListStatusAssigned, assignmentapp.ParentListStatusRevoked, assignmentapp.ParentListStatusAll:
	default:
		return assignmentapp.ParentListQuery{}, apperror.Validation("status must be assigned, revoked, or all")
	}
	limit := assignmentapp.ParentListDefaultLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return assignmentapp.ParentListQuery{}, apperror.Validation("limit must be a positive integer")
		}
		if n > assignmentapp.ParentListMaxLimit {
			n = assignmentapp.ParentListMaxLimit
		}
		limit = n
	}
	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return assignmentapp.ParentListQuery{}, apperror.Validation("offset must be a non-negative integer")
		}
		offset = n
	}
	return assignmentapp.ParentListQuery{Status: status, Limit: limit, Offset: offset}, nil
}
