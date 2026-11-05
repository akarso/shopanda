package admin

import (
	"encoding/json"
	"errors"
	"net/http"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/application/admin"
	shippingapp "github.com/akarso/shopanda/internal/application/shipping"
	domainShipping "github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// WithShipmentStatusService enables GET/PUT shipment endpoints on order detail.
func (h *OrderAdminHandler) WithShipmentStatusService(svc *shippingapp.StatusService) *OrderAdminHandler {
	if h == nil {
		return nil
	}
	h.shipments = svc
	return h
}

type updateShipmentRequest struct {
	Status         string `json:"status"`
	TrackingNumber string `json:"tracking_number,omitempty"`
	ProviderRef    string `json:"provider_ref,omitempty"`
}

const maxUpdateShipmentBodyBytes int64 = 1 << 20

// GetShipment handles GET /api/v1/admin/orders/{orderId}/shipment.
func (h *OrderAdminHandler) GetShipment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.shipments == nil {
			httpshared.JSONError(w, apperror.Internal("shipment status service not configured"))
			return
		}
		orderID := r.PathValue("orderId")
		if err := h.validator.ValidateOrderID(orderID); err != nil {
			httpshared.JSONError(w, err)
			return
		}
		adminID := h.getAdminID(r)
		sh, err := h.shipments.FindByOrderID(r.Context(), orderID)
		if err != nil {
			scopeDetails := adminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditOrderRead,
				ResourceType: "shipment",
				ResourceID:   orderID,
				Details:      scopeDetails,
				Result:       "error",
				Error:        err.Error(),
			})
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"shipment": shipmentResponse(sh),
		})
	}
}

// UpdateShipment handles PUT /api/v1/admin/orders/{orderId}/shipment.
func (h *OrderAdminHandler) UpdateShipment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.shipments == nil {
			httpshared.JSONError(w, apperror.Internal("shipment status service not configured"))
			return
		}
		orderID := r.PathValue("orderId")
		if err := h.validator.ValidateOrderID(orderID); err != nil {
			httpshared.JSONError(w, err)
			return
		}
		adminID := h.getAdminID(r)

		var req updateShipmentRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxUpdateShipmentBodyBytes))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				httpshared.JSONError(w, apperror.Validation("request body too large"))
				return
			}
			httpshared.JSONError(w, apperror.Validation("invalid JSON body"))
			return
		}

		target, err := shippingapp.NormalizeStatus(req.Status)
		if err != nil {
			httpshared.JSONError(w, apperror.Validation("status must be shipped, delivered, or cancelled"))
			return
		}

		var res shippingapp.StatusResult
		switch target {
		case domainShipping.StatusShipped:
			res, err = h.shipments.MarkShipped(r.Context(), orderID, req.TrackingNumber, req.ProviderRef)
		case domainShipping.StatusDelivered:
			res, err = h.shipments.MarkDelivered(r.Context(), orderID)
		case domainShipping.StatusCancelled:
			res, err = h.shipments.Cancel(r.Context(), orderID)
		default:
			httpshared.JSONError(w, apperror.Validation("unsupported shipment status"))
			return
		}
		if err != nil {
			scopeDetails := adminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditShipmentUpdate,
				ResourceType: "shipment",
				ResourceID:   orderID,
				Result:       "error",
				Error:        err.Error(),
				Details:      scopeDetails,
			})
			httpshared.JSONError(w, err)
			return
		}

		scopeDetails := adminScopeDetailsFromRequest(r)
		if scopeDetails == nil {
			scopeDetails = make(map[string]interface{})
		}
		scopeDetails["old_status"] = string(res.PreviousStatus)
		scopeDetails["new_status"] = string(res.Shipment.Status())
		scopeDetails["changed"] = res.Changed
		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditShipmentUpdate,
			ResourceType: "shipment",
			ResourceID:   orderID,
			Result:       "success",
			Details:      scopeDetails,
		})

		statusCode := http.StatusOK
		if res.Changed {
			statusCode = http.StatusAccepted
		}
		httpshared.JSON(w, statusCode, map[string]interface{}{
			"shipment": shipmentResponse(res.Shipment),
			"changed":  res.Changed,
		})
	}
}

func shipmentResponse(sh *domainShipping.Shipment) map[string]interface{} {
	if sh == nil {
		return nil
	}
	out := map[string]interface{}{
		"id":         sh.ID,
		"order_id":   sh.OrderID,
		"method":     string(sh.Method),
		"status":     string(sh.Status()),
		"cost":       sh.Cost.Amount(),
		"currency":   sh.Currency(),
		"created_at": sh.CreatedAt,
		"updated_at": sh.UpdatedAt,
	}
	if sh.TrackingNumber != "" {
		out["tracking_number"] = sh.TrackingNumber
	}
	if sh.ProviderRef != "" {
		out["provider_ref"] = sh.ProviderRef
	}
	return out
}
