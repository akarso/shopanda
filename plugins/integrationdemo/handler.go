package integrationdemo

import (
	"errors"
	"net/http"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/integrationhttp"
)

// NewOrderStatusHandler handles inbound ERP order status callbacks.
func NewOrderStatusHandler(updater extapi.IntegrationOrderStatusUpdater, log logger.Logger) http.Handler {
	if updater == nil {
		panic("integrationdemo: order status updater must not be nil")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload OrderStatusPayload
		if err := integrationhttp.DecodeJSON(r, 0, &payload); err != nil {
			integrationhttp.WriteError(w, http.StatusBadRequest, "invalid_payload", "invalid JSON body", nil)
			return
		}
		orderID, status, externalRef, err := payload.Normalize()
		if err != nil {
			integrationhttp.WriteError(w, http.StatusBadRequest, "invalid_payload", err.Error(), nil)
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
			log.Info("integration.order_status.received", fields)
		}

		result, err := updater.ApplyOrderStatus(r.Context(), orderID, status)
		if err != nil {
			writeOrderStatusError(w, err)
			return
		}
		if externalRef != "" {
			result.ExternalRef = externalRef
		}

		statusCode := http.StatusOK
		if result.Changed {
			statusCode = http.StatusAccepted
		}
		integrationhttp.WriteJSON(w, statusCode, map[string]interface{}{"order_status": result})
	})
}

func writeOrderStatusError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, extapi.ErrIntegrationOrderNotFound):
		integrationhttp.WriteError(w, http.StatusNotFound, extapi.IntegrationErrorOrderNotFound, "order not found", nil)
	case errors.Is(err, extapi.ErrIntegrationOrderInvalidStatus):
		integrationhttp.WriteError(w, http.StatusBadRequest, extapi.IntegrationErrorOrderInvalidStatus, "unsupported order status", nil)
	case errors.Is(err, extapi.ErrIntegrationOrderInvalidTransition):
		integrationhttp.WriteError(w, http.StatusUnprocessableEntity, extapi.IntegrationErrorOrderInvalidTransition, err.Error(), nil)
	default:
		integrationhttp.WriteError(w, http.StatusInternalServerError, "internal", "order status update failed", nil)
	}
}
