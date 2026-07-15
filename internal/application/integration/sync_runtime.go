package integration

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/infrastructure/cron"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
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
			return enqueueSyncJob(ctx, queue, jobReg, extapi.SyncTriggerEvent, eventPayload(evt.Data), log)
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
			if err := enqueueSyncJob(ctx, queue, jobReg, extapi.SyncTriggerCron, nil, log); err != nil && log != nil {
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

func enqueueSyncJob(ctx context.Context, queue jobs.Queue, reg plugin.SyncJobRegistration, trigger string, payload map[string]interface{}, log logger.Logger) error {
	if payload == nil {
		payload = map[string]interface{}{}
	}
	job, err := jobs.NewJob(id.New(), reg.JobType, map[string]interface{}{
		"plugin":  reg.PluginSlug,
		"name":    reg.Job.Name,
		"trigger": trigger,
		"payload": payload,
	})
	if err != nil {
		return err
	}
	if reg.Job.MaxRetries > 0 {
		job.MaxRetries = reg.Job.MaxRetries
	}
	if err := queue.Enqueue(ctx, job); err != nil {
		return err
	}
	if log != nil {
		log.Info("integration.sync_job.enqueued", map[string]interface{}{
			"job_id":   job.ID,
			"job_type": job.Type,
			"trigger":  trigger,
		})
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

	req := extapi.SyncJobContext{
		JobID:   job.ID,
		Plugin:  pluginSlug,
		Name:    name,
		Trigger: trigger,
		Payload: payload,
		DB:      h.db,
		Logger:  h.log,
	}
	return h.handler(ctx, req)
}

func eventPayload(data interface{}) map[string]interface{} {
	if data == nil {
		return map[string]interface{}{}
	}
	if payload, ok := data.(map[string]interface{}); ok {
		return payload
	}
	return map[string]interface{}{"data": data}
}
