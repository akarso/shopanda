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
		// eventPayload wraps non-map Data as {"data": <value>}.
		if data, ok := job.Payload["data"].(order.OrderCreatedData); ok {
			fields["order_id"] = data.OrderID
			fields["item_count"] = data.ItemCount
		}
		log.Info("example.order.created", fields)
		return nil
	}
}
