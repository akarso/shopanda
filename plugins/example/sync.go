package example

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

const (
	integrationSlug = "example"
	orderCreatedJob = "demo.order_created"
)

// registerOrderCreatedSync registers an event-triggered sync job. The
// composition root (serve) wires Bus.OnAsync → queue via
// RegisterSyncJobEventTriggers; do not hand-roll OnAsync+Enqueue here or
// every run is stamped trigger=manual.
func registerOrderCreatedSync(app *plugin.App) error {
	sdk := pluginsdk.New(app, "example/demo")
	return sdk.Integration(integrationSlug).RegisterOnEvent(
		orderCreatedJob,
		order.EventOrderCreated,
		orderCreatedSyncHandler(app.Logger),
	)
}

func orderCreatedSyncHandler(log logger.Logger) extapi.SyncJobHandler {
	return func(_ context.Context, job extapi.SyncJobContext) error {
		if log == nil {
			return nil
		}
		fields := map[string]interface{}{
			"job_id":  job.JobID,
			"trigger": job.Trigger,
			"payload": job.Payload,
		}
		if orderID, itemCount, ok := orderCreatedFields(job.Payload); ok {
			fields["order_id"] = orderID
			fields["item_count"] = itemCount
		}
		log.Info("example.order.created", fields)
		return nil
	}
}

// orderCreatedFields reads eventPayload's {"data": …} after a real queue
// JSON round-trip (map[string]interface{}). Also accepts the in-process
// typed struct used by in-memory test queues.
func orderCreatedFields(payload map[string]interface{}) (orderID string, itemCount int, ok bool) {
	if payload == nil {
		return "", 0, false
	}
	raw, exists := payload["data"]
	if !exists {
		return "", 0, false
	}
	switch data := raw.(type) {
	case order.OrderCreatedData:
		return data.OrderID, data.ItemCount, true
	case map[string]interface{}:
		id, _ := data["order_id"].(string)
		if id == "" {
			return "", 0, false
		}
		return id, jsonInt(data["item_count"]), true
	default:
		return "", 0, false
	}
}

func jsonInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
