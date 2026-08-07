package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	domainwebhook "github.com/akarso/shopanda/internal/domain/webhook"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// Dispatcher enqueues outbound webhook deliveries for subscribed endpoints.
type Dispatcher struct {
	repo  domainwebhook.Repository
	queue jobs.Queue
	log   logger.Logger
}

// NewDispatcher creates a webhook event dispatcher.
func NewDispatcher(repo domainwebhook.Repository, queue jobs.Queue, log logger.Logger) *Dispatcher {
	if repo == nil {
		panic("webhook.NewDispatcher: nil repo")
	}
	if queue == nil {
		panic("webhook.NewDispatcher: nil queue")
	}
	if log == nil {
		panic("webhook.NewDispatcher: nil log")
	}
	return &Dispatcher{repo: repo, queue: queue, log: log}
}

// Register attaches async handlers for supported domain events.
func (d *Dispatcher) Register(bus *event.Bus) {
	if bus == nil {
		return
	}
	for _, name := range domainwebhook.SupportedEvents {
		eventName := name
		bus.OnAsync(eventName, func(ctx context.Context, evt event.Event) error {
			return d.handle(ctx, evt)
		})
	}
}

func (d *Dispatcher) handle(ctx context.Context, evt event.Event) error {
	endpoints, err := d.repo.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("webhook dispatch: list active: %w", err)
	}
	if len(endpoints) == 0 {
		return nil
	}

	dataJSON, err := json.Marshal(evt.Data)
	if err != nil {
		return fmt.Errorf("webhook dispatch: marshal event data: %w", err)
	}

	for _, endpoint := range endpoints {
		if !endpoint.Subscribed(evt.Name) {
			continue
		}
		job, err := jobs.NewJob(id.New(), domainwebhook.DeliverJobType, map[string]interface{}{
			"endpoint_id":     endpoint.ID,
			"event_id":        evt.ID,
			"event_name":      evt.Name,
			"event_source":    evt.Source,
			"event_timestamp": evt.Timestamp.UTC().Format(time.RFC3339),
			"event_data_json": string(dataJSON),
		})
		if err != nil {
			return fmt.Errorf("webhook dispatch: new job: %w", err)
		}
		if err := d.queue.Enqueue(ctx, job); err != nil {
			return fmt.Errorf("webhook dispatch: enqueue: %w", err)
		}
	}
	return nil
}
