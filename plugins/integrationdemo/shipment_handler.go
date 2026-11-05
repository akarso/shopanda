package integrationdemo

import (
	"errors"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/integrationhttp"
)

// NewShipmentStatusHandler handles inbound warehouse/ERP shipment status callbacks.
func NewShipmentStatusHandler(updater extapi.IntegrationShipmentStatusUpdater, log logger.Logger) http.Handler {
	if updater == nil {
		panic("integrationdemo: shipment status updater must not be nil")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ShipmentStatusPayload
		if err := integrationhttp.DecodeJSON(r, 0, &payload); err != nil {
			integrationhttp.WriteError(w, http.StatusBadRequest, "invalid_payload", "invalid JSON body", nil)
			return
		}
		orderID, status, tracking, providerRef, externalRef, err := payload.Normalize()
		if err != nil {
			integrationhttp.WriteError(w, http.StatusBadRequest, "invalid_payload", err.Error(), nil)
			return
		}
		if isShipStatus(status) && tracking == "" {
			integrationhttp.WriteError(w, http.StatusBadRequest, extapi.IntegrationErrorShipmentInvalidPayload, "tracking_number is required when status is shipped", nil)
			return
		}

		if log != nil {
			fields := map[string]interface{}{
				"plugin":   RouteSlug,
				"order_id": orderID,
				"status":   status,
			}
			if key := r.Header.Get(extapi.IntegrationHeaderIdempotencyKey); key != "" {
				fields["idempotency_key"] = key
			}
			if externalRef != "" {
				fields["external_ref"] = externalRef
			}
			log.Info("integration.shipment_status.received", fields)
		}

		result, err := updater.ApplyShipmentStatus(r.Context(), orderID, status, tracking, providerRef)
		if err != nil {
			writeShipmentStatusError(w, err)
			return
		}
		if externalRef != "" {
			result.ExternalRef = externalRef
		}

		statusCode := http.StatusOK
		if result.Changed {
			statusCode = http.StatusAccepted
		}
		integrationhttp.WriteJSON(w, statusCode, map[string]interface{}{"shipment_status": result})
	})
}

func isShipStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "SHIPPED", "SHIP":
		return true
	default:
		return strings.EqualFold(status, "shipped")
	}
}

func writeShipmentStatusError(w http.ResponseWriter, err error) {
	msg := shipmentErrorMessage(err)
	switch {
	case errors.Is(err, extapi.ErrIntegrationShipmentNotFound):
		integrationhttp.WriteError(w, http.StatusNotFound, extapi.IntegrationErrorShipmentNotFound, "shipment not found", nil)
	case errors.Is(err, extapi.ErrIntegrationShipmentInvalidStatus):
		integrationhttp.WriteError(w, http.StatusBadRequest, extapi.IntegrationErrorShipmentInvalidStatus, "unsupported shipment status", nil)
	case errors.Is(err, extapi.ErrIntegrationShipmentInvalidPayload):
		integrationhttp.WriteError(w, http.StatusBadRequest, extapi.IntegrationErrorShipmentInvalidPayload, msg, nil)
	case errors.Is(err, extapi.ErrIntegrationShipmentInvalidTransition):
		integrationhttp.WriteError(w, http.StatusUnprocessableEntity, extapi.IntegrationErrorShipmentInvalidTransition, msg, nil)
	case errors.Is(err, extapi.ErrIntegrationShipmentConflict):
		integrationhttp.WriteError(w, http.StatusConflict, extapi.IntegrationErrorShipmentConflict, msg, nil)
	default:
		integrationhttp.WriteError(w, http.StatusInternalServerError, "internal", "shipment status update failed", nil)
	}
}

func shipmentErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	for _, prefix := range []string{
		extapi.ErrIntegrationShipmentInvalidPayload.Error() + ": ",
		extapi.ErrIntegrationShipmentInvalidTransition.Error() + ": ",
		extapi.ErrIntegrationShipmentConflict.Error() + ": ",
	} {
		if strings.HasPrefix(s, prefix) {
			return strings.TrimPrefix(s, prefix)
		}
	}
	return s
}
