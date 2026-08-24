package admin

import (
	"encoding/json"
	"errors"
	"net/http"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"

	"github.com/akarso/shopanda/internal/application/admin"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	orderApp "github.com/akarso/shopanda/internal/application/order"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// OrderAdminHandler serves admin order endpoints with Track 3 hardening.
type OrderAdminHandler struct {
	orders     order.OrderRepository
	extensions *extensionapp.ValueService
	auditor    *admin.Auditor
	validator  admin.OrderValidator
}

// NewOrderAdminHandler creates an OrderAdminHandler.
func NewOrderAdminHandler(orders order.OrderRepository, log logger.Logger, extensions *extensionapp.ValueService) *OrderAdminHandler {
	if orders == nil {
		panic("http: order repository must not be nil")
	}
	if log == nil {
		panic("http: logger must not be nil")
	}
	return &OrderAdminHandler{
		orders:     orders,
		extensions: extensions,
		auditor:    admin.NewAuditor(log),
		validator:  admin.NewOrderValidator(),
	}
}

// NewOrderAdminHandlerWithAuditor creates an OrderAdminHandler with custom auditor (for testing).
func NewOrderAdminHandlerWithAuditor(orders order.OrderRepository, auditor *admin.Auditor, extensions *extensionapp.ValueService) *OrderAdminHandler {
	if orders == nil {
		panic("http: order repository must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &OrderAdminHandler{
		orders:     orders,
		extensions: extensions,
		auditor:    auditor,
		validator:  admin.NewOrderValidator(),
	}
}

func (h *OrderAdminHandler) getAdminID(r *http.Request) string {
	ac, err := admin.FromContext(r.Context())
	if err != nil || ac == nil || ac.AdminID == "" {
		return "system"
	}
	return ac.AdminID
}

func adminScopeDetailsFromRequest(r *http.Request) map[string]interface{} {
	return fullAdminScopeDetailsFromRequest(r)
}

// List handles GET /api/v1/admin/orders with Track 3 audit logging.
func (h *OrderAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := httpshared.ParsePagination(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}

		// Validate pagination to prevent resource exhaustion.
		if err := h.validator.ValidatePagination(offset, limit); err != nil {
			httpshared.JSONError(w, err)
			return
		}

		// Extract admin context for auditing (populated by auth middleware).
		adminID := h.getAdminID(r)

		orders, err := h.orders.List(r.Context(), offset, limit)
		if err != nil {
			scopeDetails := adminScopeDetailsFromRequest(r)
			scopeDetails["offset"] = offset
			scopeDetails["limit"] = limit
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditOrderList,
				ResourceType: "orders",
				Details:      scopeDetails,
				Result:       "error",
				Error:        err.Error(),
			})
			httpshared.JSONError(w, err)
			return
		}

		// Log successful list operation for compliance.
		out := make([]storefront.OrderResponse, 0, len(orders))
		for i := range orders {
			resp, err := storefront.ToOrderResponse(r.Context(), h.extensions, &orders[i])
			if err != nil {
				httpshared.JSONError(w, extensionapp.MapValueError(err))
				return
			}
			out = append(out, resp)
		}

		scopeDetails := adminScopeDetailsFromRequest(r)
		if scopeDetails == nil {
			scopeDetails = make(map[string]interface{})
		}
		scopeDetails["offset"] = offset
		scopeDetails["limit"] = limit
		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditOrderList,
			ResourceType: "orders",
			Result:       "success",
			Details:      scopeDetails,
		})

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"orders": out,
		})
	}
}

// Get handles GET /api/v1/admin/orders/{orderId} with Track 3 audit logging.
func (h *OrderAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := r.PathValue("orderId")
		if err := h.validator.ValidateOrderID(orderID); err != nil {
			httpshared.JSONError(w, err)
			return
		}

		// Extract admin context for auditing.
		adminID := h.getAdminID(r)

		o, err := h.orders.FindByID(r.Context(), orderID)
		if err != nil {
			scopeDetails := adminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditOrderRead,
				ResourceType: "order",
				ResourceID:   orderID,
				Details:      scopeDetails,
				Result:       "error",
				Error:        err.Error(),
			})
			httpshared.JSONError(w, err)
			return
		}
		if o == nil {
			// Do not leak that order exists/doesn't exist in the audit log to prevent enumeration.
			httpshared.JSONError(w, apperror.NotFound("order not found"))
			return
		}

		resp, err := storefront.ToOrderResponse(r.Context(), h.extensions, o)
		if err != nil {
			httpshared.JSONError(w, extensionapp.MapValueError(err))
			return
		}

		// Log successful read access for compliance.
		scopeDetails := adminScopeDetailsFromRequest(r)
		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditOrderRead,
			ResourceType: "order",
			ResourceID:   orderID,
			Result:       "success",
			Details:      scopeDetails,
		})

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"order": resp,
		})
	}
}

type updateOrderRequest struct {
	Status string `json:"status"`
}

const maxUpdateOrderBodyBytes int64 = 1 << 20

// Update handles PUT /api/v1/admin/orders/{orderId} with Track 3 audit logging and hardening.
func (h *OrderAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := r.PathValue("orderId")
		if err := h.validator.ValidateOrderID(orderID); err != nil {
			httpshared.JSONError(w, err)
			return
		}

		// Extract admin context for auditing.
		adminID := h.getAdminID(r)

		var req updateOrderRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxUpdateOrderBodyBytes))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			scopeDetails := adminScopeDetailsFromRequest(r)
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				h.auditor.LogAction(r.Context(), admin.AuditEntry{
					AdminID:      adminID,
					Action:       admin.AuditOrderUpdate,
					ResourceType: "order",
					ResourceID:   orderID,
					Result:       "error",
					Error:        "request body too large",
					Details:      scopeDetails,
				})
				httpshared.JSONError(w, apperror.Validation("request body too large"))
				return
			}
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditOrderUpdate,
				ResourceType: "order",
				ResourceID:   orderID,
				Result:       "error",
				Error:        "invalid request body",
				Details:      scopeDetails,
			})
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		next := order.OrderStatus(req.Status)
		switch next {
		case order.OrderStatusConfirmed, order.OrderStatusPaid, order.OrderStatusCancelled, order.OrderStatusFailed:
		case order.OrderStatusPending:
			scopeDetails := adminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditOrderUpdate,
				ResourceType: "order",
				ResourceID:   orderID,
				Result:       "error",
				Error:        "transition to pending not allowed",
				Details:      scopeDetails,
			})
			httpshared.JSONError(w, apperror.Validation("transition to pending not allowed"))
			return
		default:
			scopeDetails := adminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditOrderUpdate,
				ResourceType: "order",
				ResourceID:   orderID,
				Result:       "error",
				Error:        "invalid target status",
				Details:      scopeDetails,
			})
			httpshared.JSONError(w, apperror.Validation("invalid target status"))
			return
		}

		o, err := h.orders.FindByID(r.Context(), orderID)
		if err != nil {
			scopeDetails := adminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditOrderUpdate,
				ResourceType: "order",
				ResourceID:   orderID,
				Result:       "error",
				Error:        err.Error(),
				Details:      scopeDetails,
			})
			httpshared.JSONError(w, err)
			return
		}
		if o == nil {
			// Do not audit order not-found attempts (prevents enumeration attacks).
			httpshared.JSONError(w, apperror.NotFound("order not found"))
			return
		}

		if o.Status() == next {
			resp, err := storefront.ToOrderResponse(r.Context(), h.extensions, o)
			if err != nil {
				httpshared.JSONError(w, extensionapp.MapValueError(err))
				return
			}
			httpshared.JSON(w, http.StatusOK, map[string]interface{}{"order": resp})
			return
		}

		oldStatus := string(o.Status())

		if err := orderApp.ApplyStatusTransition(o, next); err != nil {
			scopeDetails := adminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditOrderUpdate,
				ResourceType: "order",
				ResourceID:   orderID,
				Result:       "error",
				Error:        err.Error(),
				Details:      scopeDetails,
			})
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}

		if err := h.orders.UpdateStatus(r.Context(), o); err != nil {
			scopeDetails := adminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditOrderUpdate,
				ResourceType: "order",
				ResourceID:   orderID,
				Result:       "error",
				Error:        err.Error(),
				Details:      scopeDetails,
			})
			httpshared.JSONError(w, err)
			return
		}

		resp, err := storefront.ToOrderResponse(r.Context(), h.extensions, o)
		if err != nil {
			httpshared.JSONError(w, extensionapp.MapValueError(err))
			return
		}

		// Log successful status transition for compliance and audit trail.
		scopeDetails := adminScopeDetailsFromRequest(r)
		if scopeDetails == nil {
			scopeDetails = make(map[string]interface{})
		}
		scopeDetails["old_status"] = oldStatus
		scopeDetails["new_status"] = string(next)
		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditOrderStatusChange,
			ResourceType: "order",
			ResourceID:   orderID,
			Result:       "success",
			Details:      scopeDetails,
		})

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"order": resp})
	}
}
