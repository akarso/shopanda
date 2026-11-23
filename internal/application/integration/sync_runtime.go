package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/infrastructure/cron"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/internal/platform/requestctx"
	"github.com/akarso/shopanda/pkg/extapi"
)

// RegisterSyncJobHandlers registers queue handlers for plugin sync jobs on worker.
func RegisterSyncJobHandlers(app *plugin.App, worker *jobs.Worker) error {
	if app == nil || worker == nil {
		return nil
	}
	if app.Bootstrap == nil || app.Bootstrap.DB == nil {
		return fmt.Errorf("integration sync jobs: database bootstrap not configured")
	}
	for _, reg := range app.SyncJobs() {
		worker.Register(newSyncJobHandler(reg, app.Bootstrap.DB, app.Logger))
	}
	return nil
}

// RegisterSyncJobEventTriggers wires event-triggered sync jobs on the API process.
func RegisterSyncJobEventTriggers(app *plugin.App, bus *event.Bus, queue jobs.Queue, log logger.Logger) error {
	if app == nil || bus == nil || queue == nil {
		return nil
	}
	for _, reg := range app.SyncJobs() {
		if reg.Job.Trigger.Kind != extapi.SyncTriggerEvent {
			continue
		}
		eventName := reg.Job.Trigger.EventName
		jobReg := reg
		bus.OnAsync(eventName, func(ctx context.Context, evt event.Event) error {
			// Flatten typed domain payloads (e.g. order.OrderCreatedData) to a
			// public JSON-tag map before EventJobID / enqueue — same shape the
			// queue round-trip would produce, with order_id at the top level.
			payload := eventPayload(evt.Data)
			return enqueueSyncJob(ctx, queue, jobReg, extapi.SyncTriggerEvent, payload, log, app, evt.Meta["request_id"])
		})
		if log != nil {
			log.Info("integration.sync_job.event_registered", map[string]interface{}{
				"job_type":   jobReg.JobType,
				"event_name": eventName,
			})
		}
	}
	return nil
}

// RegisterSyncJobCronTriggers wires cron-triggered sync jobs on the scheduler process.
func RegisterSyncJobCronTriggers(app *plugin.App, queue jobs.Queue, sched scheduler.Scheduler, log logger.Logger) error {
	if app == nil || queue == nil || sched == nil {
		return nil
	}
	for _, reg := range app.SyncJobs() {
		if reg.Job.Trigger.Kind != extapi.SyncTriggerCron {
			continue
		}
		spec, err := cron.NormalizeSpec(reg.Job.Trigger.CronSpec)
		if err != nil {
			return fmt.Errorf("integration sync job %q: %w", reg.Job.Name, err)
		}
		jobReg := reg
		sched.Register(jobReg.JobType, spec, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := enqueueSyncJob(ctx, queue, jobReg, extapi.SyncTriggerCron, nil, log, nil, ""); err != nil && log != nil {
				log.Error("integration.sync_job.cron_enqueue", err, map[string]interface{}{
					"job_type": jobReg.JobType,
				})
			}
		})
		if log != nil {
			log.Info("integration.sync_job.cron_registered", map[string]interface{}{
				"job_type":  jobReg.JobType,
				"cron_spec": spec,
			})
		}
	}
	return nil
}

func enqueueSyncJob(ctx context.Context, queue jobs.Queue, reg plugin.SyncJobRegistration, trigger string, payload map[string]interface{}, log logger.Logger, app *plugin.App, metaRequestID string) error {
	// EventJobID sees the stable public payload before request_id is injected
	// so a callback that hashes the whole map still dedupes across requests.
	var (
		job     jobs.Job
		err     error
		fixedID bool
	)
	if trigger == extapi.SyncTriggerEvent && reg.Job.EventJobID != nil {
		derived := strings.TrimSpace(reg.Job.EventJobID(payload))
		job, err = plugin.NewSyncJobEnvelopeWithID(reg, trigger, payload, derived)
		fixedID = true
	} else {
		job, err = plugin.NewSyncJobEnvelope(reg, trigger, payload)
	}
	if err != nil {
		return err
	}
	// Inject request_id into the stored envelope payload after id derivation.
	enriched := plugin.InjectRequestID(ctx, payload)
	if metaRequestID != "" {
		if existing, ok := enriched["request_id"].(string); !ok || existing == "" {
			enriched["request_id"] = metaRequestID
		}
	}
	job.Payload["payload"] = enriched
	if err := queue.Enqueue(ctx, job); err != nil {
		if fixedID && app != nil && apperror.Is(err, apperror.CodeConflict) {
			return app.ResolveEnqueueConflict(ctx, reg.Job.Name, job.ID, job.Type, err)
		}
		return err
	}
	if log != nil {
		fields := map[string]interface{}{
			"job_id":   job.ID,
			"job_type": job.Type,
			"trigger":  trigger,
		}
		if rid, _ := enriched["request_id"].(string); rid != "" {
			fields["request_id"] = rid
		}
		log.Info("integration.sync_job.enqueued", fields)
	}
	return nil
}

type syncJobHandler struct {
	reg     plugin.SyncJobRegistration
	db      *sql.DB
	handler extapi.SyncJobHandler
	log     logger.Logger
}

func newSyncJobHandler(reg plugin.SyncJobRegistration, db *sql.DB, log logger.Logger) jobs.Handler {
	return &syncJobHandler{
		reg:     reg,
		db:      db,
		handler: reg.Job.Handler,
		log:     log,
	}
}

func (h *syncJobHandler) Type() string { return h.reg.JobType }

func (h *syncJobHandler) Handle(ctx context.Context, job jobs.Job) error {
	payload, _ := job.Payload["payload"].(map[string]interface{})
	trigger, _ := job.Payload["trigger"].(string)
	pluginSlug, _ := job.Payload["plugin"].(string)
	name, _ := job.Payload["name"].(string)

	// Reinject request_id from the sync envelope onto ctx so handlers that
	// use requestctx.RequestID(ctx) match HTTP/worker correlation (payload
	// already carries it from InjectRequestID on enqueue).
	ctx = withPayloadRequestID(ctx, job.Payload, payload)

	req := extapi.SyncJobContext{
		JobID:    job.ID,
		Plugin:   pluginSlug,
		Name:     name,
		Trigger:  trigger,
		Attempts: job.Attempts,
		Payload:  payload,
		DB:       h.db,
		Logger:   h.log,
	}
	return h.handler(ctx, req)
}

// withPayloadRequestID sets requestctx from the job envelope when ctx has no
// request ID yet. Prefers top-level request_id, then inner payload.request_id
// (same order as jobs.mergeJobRequestID).
func withPayloadRequestID(ctx context.Context, envelope, inner map[string]interface{}) context.Context {
	if requestctx.RequestID(ctx) != "" {
		return ctx
	}
	rid := requestIDFromMaps(envelope, inner)
	if rid == "" {
		return ctx
	}
	return requestctx.WithRequestID(ctx, rid)
}

func requestIDFromMaps(envelope, inner map[string]interface{}) string {
	if rid, _ := envelope["request_id"].(string); strings.TrimSpace(rid) != "" {
		return strings.TrimSpace(rid)
	}
	if inner != nil {
		if rid, _ := inner["request_id"].(string); strings.TrimSpace(rid) != "" {
			return strings.TrimSpace(rid)
		}
	}
	return ""
}

// eventPayload returns a public JSON-object map for sync enqueue / EventJobID.
// Typed structs with json tags (order.OrderCreatedData, …) are flattened via
// a JSON round-trip so callbacks see order_id at the top level — the same
// shape handlers get after a queue JSON round-trip. Maps are shallow-copied.
// Non-object values stay under "data" as a last resort.
func eventPayload(data interface{}) map[string]interface{} {
	if data == nil {
		return map[string]interface{}{}
	}
	if payload, ok := data.(map[string]interface{}); ok {
		return plugin.ShallowCopyStringMap(payload)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return map[string]interface{}{"data": data}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]interface{}{"data": data}
	}
	return out
}
