package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// OrderAdminHandler serves admin order endpoints with Track 3 hardening.
type OrderAdminHandler struct {
	orders    order.OrderRepository
	auditor   *admin.Auditor
	validator *admin.OrderValidator
}

// NewOrderAdminHandler creates an OrderAdminHandler.
func NewOrderAdminHandler(orders order.OrderRepository) *OrderAdminHandler {
	if orders == nil {
		panic("http: order repository must not be nil")
	}
	return &OrderAdminHandler{
		orders:    orders,
		auditor:   admin.NewAuditor(logger.New("info")),
		validator: admin.NewOrderValidator(),
	}
}

// NewOrderAdminHandlerWithAuditor creates an OrderAdminHandler with custom auditor (for testing).
func NewOrderAdminHandlerWithAuditor(orders order.OrderRepository, auditor *admin.Auditor) *OrderAdminHandler {
	if orders == nil {
		panic("http: order repository must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &OrderAdminHandler{
		orders:    orders,
		auditor:   auditor,
		validator: admin.NewOrderValidator(),
	}
}

// List handles GET /api/v1/admin/orders with Track 3 audit logging.
func (h *OrderAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := parsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}

		// Validate pagination to prevent resource exhaustion.
		if err := h.validator.ValidatePagination(offset, limit); err != nil {
			JSONError(w, err)
			return
		}

		// Extract admin context for auditing (populated by auth middleware).
		adminID := "system"
		if ac, ok := r.Context().Value(admin.AdminContextKey{}).(*admin.AdminContext); ok && ac != nil {
			adminID = ac.AdminID
		}

		orders, err := h.orders.List(r.Context(), offset, limit)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditOrderList,
				ResourceType: "orders",
				Result:       "error",
				Error:        err.Error(),
			})
			JSONError(w, err)
			return
		}

		// Log successful list operation for compliance.
		h.auditor.LogOrderListAccess(r.Context(), adminID, offset, limit)

		out := make([]orderResponse, 0, len(orders))
		for i := range orders {
			out = append(out, toOrderResponse(&orders[i]))
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"orders": out,
		})
	}
}

// Get handles GET /api/v1/admin/orders/{orderId} with Track 3 audit logging.
func (h *OrderAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := r.PathValue("orderId")
		if err := h.validator.ValidateOrderID(orderID); err != nil {
			JSONError(w, err)
			return
		}

		// Extract admin context for auditing.
		adminID := "system"
		if ac, ok := r.Context().Value(admin.AdminContextKey{}).(*admin.AdminContext); ok && ac != nil {
			adminID = ac.AdminID
		}

		o, err := h.orders.FindByID(r.Context(), orderID)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditOrderRead,
				ResourceType: "order",
				ResourceID:   orderID,
				Result:       "error",
				Error:        err.Error(),
			})
			JSONError(w, err)
			return
		}
		if o == nil {
			// Do not leak that order exists/doesn't exist in the audit log to prevent enumeration.
			JSONError(w, apperror.NotFound("order not found"))
			return
		}

		// Log successful read access for compliance.
		h.auditor.LogOrderRead(r.Context(), adminID, orderID)

		JSON(w, http.StatusOK, map[string]interface{}{
			"order": toOrderResponse(o),
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
			JSONError(w, err)
			return
		}

		// Extract admin context for auditing.
		adminID := "system"
		if ac, ok := r.Context().Value(admin.AdminContextKey{}).(*admin.AdminContext); ok && ac != nil {
			adminID = ac.AdminID
		}

		var req updateOrderRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxUpdateOrderBodyBytes))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				h.auditor.LogOrderUpdateError(r.Context(), adminID, orderID, "request body too large")
				JSONError(w, apperror.Validation("request body too large"))
				return
			}
			h.auditor.LogOrderUpdateError(r.Context(), adminID, orderID, "invalid request body")
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		next := order.OrderStatus(req.Status)
		switch next {
		case order.OrderStatusConfirmed, order.OrderStatusPaid, order.OrderStatusCancelled, order.OrderStatusFailed:
		case order.OrderStatusPending:
			h.auditor.LogOrderUpdateError(r.Context(), adminID, orderID, "transition to pending not allowed")
			JSONError(w, apperror.Validation("transition to pending not allowed"))
			return
		default:
			h.auditor.LogOrderUpdateError(r.Context(), adminID, orderID, "invalid target status")
			JSONError(w, apperror.Validation("invalid target status"))
			return
		}

		o, err := h.orders.FindByID(r.Context(), orderID)
		if err != nil {
			h.auditor.LogOrderUpdateError(r.Context(), adminID, orderID, err.Error())
			JSONError(w, err)
			return
		}
		if o == nil {
			// Do not audit order not-found attempts (prevents enumeration attacks).
			JSONError(w, apperror.NotFound("order not found"))
			return
		}

		if o.Status() == next {
			JSON(w, http.StatusOK, map[string]interface{}{"order": toOrderResponse(o)})
			return
		}

		oldStatus := string(o.Status())

		if err := applyOrderStatusTransition(o, next); err != nil {
			h.auditor.LogOrderUpdateError(r.Context(), adminID, orderID, err.Error())
			JSONError(w, apperror.Validation(err.Error()))
			return
		}

		if err := h.orders.UpdateStatus(r.Context(), o); err != nil {
			h.auditor.LogOrderUpdateError(r.Context(), adminID, orderID, err.Error())
			JSONError(w, err)
			return
		}

		// Log successful status transition for compliance and audit trail.
		h.auditor.LogOrderUpdate(r.Context(), adminID, orderID, oldStatus, string(next))

		JSON(w, http.StatusOK, map[string]interface{}{"order": toOrderResponse(o)})
	}
}

func applyOrderStatusTransition(o *order.Order, next order.OrderStatus) error {
	switch next {
	case order.OrderStatusConfirmed:
		return o.Confirm()
	case order.OrderStatusPaid:
		return o.MarkPaid()
	case order.OrderStatusCancelled:
		return o.Cancel()
	case order.OrderStatusFailed:
		return o.Fail()
	default:
		return errors.New("unsupported target status")
	}
}
