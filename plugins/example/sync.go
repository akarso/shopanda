package example

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

const (
	integrationSlug = "example"
	orderCreatedJob = "demo.order_created"
)

func registerOrderCreatedSync(app *plugin.App) error {
	sdk := pluginsdk.New(app, "example/demo")
	if err := sdk.Integration(integrationSlug).RegisterManual(orderCreatedJob, orderCreatedSyncHandler(app.Logger)); err != nil {
		return err
	}
	if app.Bus == nil {
		return nil
	}
	// Keep OnAsync tiny: enqueue only. Slow work belongs in the sync job
	// (queue retry + jobs admin). Enqueue requires BindQueue after InitAll.
	app.Bus.OnAsync(order.EventOrderCreated, enqueueOrderCreated(sdk, app.Logger))
	return nil
}

func orderCreatedSyncHandler(log logger.Logger) extapi.SyncJobHandler {
	return func(_ context.Context, job extapi.SyncJobContext) error {
		fields := map[string]interface{}{
			"job_id":  job.JobID,
			"trigger": job.Trigger,
		}
		for k, v := range job.Payload {
			fields[k] = v
		}
		if log != nil {
			log.Info("example.order.created", fields)
		}
		return nil
	}
}

func enqueueOrderCreated(sdk *pluginsdk.SDK, log logger.Logger) event.Handler {
	return func(ctx context.Context, evt event.Event) error {
		payload := map[string]interface{}{"event_id": evt.ID}
		if data, ok := evt.Data.(order.OrderCreatedData); ok {
			payload["order_id"] = data.OrderID
			payload["item_count"] = data.ItemCount
		} else {
			payload["note"] = "unexpected payload type"
		}
		if err := sdk.Integration(integrationSlug).Enqueue(ctx, orderCreatedJob, payload); err != nil {
			if log != nil {
				log.Error("example.order.created.enqueue", err, map[string]interface{}{
					"event_id": evt.ID,
				})
			}
			return err
		}
		return nil
	}
}
