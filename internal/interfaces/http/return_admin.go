package http

import (
	"context"
	"net/http"

	"github.com/akarso/shopanda/internal/application/admin"
	returnsApp "github.com/akarso/shopanda/internal/application/returns"
	domainReturns "github.com/akarso/shopanda/internal/domain/returns"
)

// ReturnAdminHandler serves admin return (RMA) endpoints.
type ReturnAdminHandler struct {
	returns *returnsApp.Service
	auditor *admin.Auditor
}

// NewReturnAdminHandler creates a ReturnAdminHandler.
func NewReturnAdminHandler(returns *returnsApp.Service, auditor *admin.Auditor) *ReturnAdminHandler {
	if returns == nil {
		panic("http: return service must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &ReturnAdminHandler{returns: returns, auditor: auditor}
}

// List handles GET /api/v1/admin/returns.
func (h *ReturnAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := parsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}

		adminID := adminIDFromRequest(r)
		list, err := h.returns.List(r.Context(), offset, limit)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditReturnList,
				ResourceType: "returns",
				Result:       "error",
				Error:        err.Error(),
			})
			JSONError(w, err)
			return
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditReturnList,
			ResourceType: "returns",
			Result:       "success",
			Details: map[string]interface{}{
				"offset": offset,
				"limit":  limit,
				"count":  len(list),
			},
		})

		JSON(w, http.StatusOK, map[string]interface{}{
			"returns": toReturnResponses(list),
			"offset":  offset,
			"limit":   limit,
		})
	}
}

// Get handles GET /api/v1/admin/returns/{returnId}.
func (h *ReturnAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		returnID := r.PathValue("returnId")
		adminID := adminIDFromRequest(r)

		ret, err := h.returns.Get(r.Context(), returnID)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditReturnRead,
				ResourceType: "return",
				ResourceID:   returnID,
				Result:       "error",
				Error:        err.Error(),
			})
			JSONError(w, err)
			return
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditReturnRead,
			ResourceType: "return",
			ResourceID:   returnID,
			Result:       "success",
		})

		JSON(w, http.StatusOK, map[string]interface{}{
			"return": toReturnResponse(ret),
		})
	}
}

func (h *ReturnAdminHandler) transition(action admin.AuditAction, fn func(context.Context, string) (*domainReturns.Return, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		returnID := r.PathValue("returnId")
		adminID := adminIDFromRequest(r)

		ret, err := fn(r.Context(), returnID)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       action,
				ResourceType: "return",
				ResourceID:   returnID,
				Result:       "error",
				Error:        err.Error(),
			})
			JSONError(w, err)
			return
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       action,
			ResourceType: "return",
			ResourceID:   returnID,
			Result:       "success",
			Details: map[string]interface{}{
				"status": string(ret.Status()),
			},
		})

		JSON(w, http.StatusOK, map[string]interface{}{
			"return": toReturnResponse(ret),
		})
	}
}

// Approve handles POST /api/v1/admin/returns/{returnId}/approve.
func (h *ReturnAdminHandler) Approve() http.HandlerFunc {
	return h.transition(admin.AuditReturnApprove, h.returns.Approve)
}

// Reject handles POST /api/v1/admin/returns/{returnId}/reject.
func (h *ReturnAdminHandler) Reject() http.HandlerFunc {
	return h.transition(admin.AuditReturnReject, h.returns.Reject)
}

// Receive handles POST /api/v1/admin/returns/{returnId}/receive.
func (h *ReturnAdminHandler) Receive() http.HandlerFunc {
	return h.transition(admin.AuditReturnReceive, h.returns.Receive)
}

// Refund handles POST /api/v1/admin/returns/{returnId}/refund.
func (h *ReturnAdminHandler) Refund() http.HandlerFunc {
	return h.transition(admin.AuditReturnRefund, h.returns.Refund)
}
