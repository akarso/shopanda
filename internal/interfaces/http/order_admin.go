package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// OrderAdminHandler serves admin order endpoints.
type OrderAdminHandler struct {
	orders order.OrderRepository
}

// NewOrderAdminHandler creates an OrderAdminHandler.
func NewOrderAdminHandler(orders order.OrderRepository) *OrderAdminHandler {
	if orders == nil {
		panic("http: order repository must not be nil")
	}
	return &OrderAdminHandler{orders: orders}
}

// List handles GET /api/v1/admin/orders.
func (h *OrderAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := parsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}

		orders, err := h.orders.List(r.Context(), offset, limit)
		if err != nil {
			JSONError(w, err)
			return
		}

		out := make([]orderResponse, 0, len(orders))
		for i := range orders {
			out = append(out, toOrderResponse(&orders[i]))
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"orders": out,
		})
	}
}

// Get handles GET /api/v1/admin/orders/{orderId}.
func (h *OrderAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := r.PathValue("orderId")
		if orderID == "" {
			JSONError(w, apperror.Validation("order id is required"))
			return
		}

		o, err := h.orders.FindByID(r.Context(), orderID)
		if err != nil {
			JSONError(w, err)
			return
		}
		if o == nil {
			JSONError(w, apperror.NotFound("order not found"))
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"order": toOrderResponse(o),
		})
	}
}

type updateOrderRequest struct {
	Status string `json:"status"`
}

// Update handles PUT /api/v1/admin/orders/{orderId}.
func (h *OrderAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := r.PathValue("orderId")
		if orderID == "" {
			JSONError(w, apperror.Validation("order id is required"))
			return
		}

		var req updateOrderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		next := order.OrderStatus(req.Status)
		if !next.IsValid() {
			JSONError(w, apperror.Validation("invalid status"))
			return
		}

		o, err := h.orders.FindByID(r.Context(), orderID)
		if err != nil {
			JSONError(w, err)
			return
		}
		if o == nil {
			JSONError(w, apperror.NotFound("order not found"))
			return
		}

		if o.Status() == next {
			JSON(w, http.StatusOK, map[string]interface{}{"order": toOrderResponse(o)})
			return
		}

		if err := applyOrderStatusTransition(o, next); err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}

		if err := h.orders.UpdateStatus(r.Context(), o); err != nil {
			JSONError(w, err)
			return
		}

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
