package example

import (
	"context"

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
func registerOrderCreatedSync(sdk *pluginsdk.SDK) error {
	return sdk.Integration(integrationSlug).RegisterOnEvent(
		orderCreatedJob,
		extapi.EventOrderCreated,
		orderCreatedSyncHandler(sdk.Host().Logger()),
	)
}

func orderCreatedSyncHandler(log pluginsdk.Logger) extapi.SyncJobHandler {
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

// orderCreatedFields reads a flattened order.created payload (order_id at the
// top level after eventPayload JSON round-trip). Falls back to legacy
// {"data": …} envelopes.
func orderCreatedFields(payload map[string]interface{}) (orderID string, itemCount int, ok bool) {
	if payload == nil {
		return "", 0, false
	}
	if id, _ := payload["order_id"].(string); id != "" {
		return id, jsonInt(payload["item_count"]), true
	}
	raw, exists := payload["data"]
	if !exists {
		return "", 0, false
	}
	data, ok := raw.(map[string]interface{})
	if !ok {
		return "", 0, false
	}
	id, _ := data["order_id"].(string)
	if id == "" {
		return "", 0, false
	}
	return id, jsonInt(data["item_count"]), true
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
