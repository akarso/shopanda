package extapi

import (
	"context"
	"database/sql"
	"strings"
)

// Sync job trigger kinds.
const (
	SyncTriggerCron  = "cron"
	SyncTriggerEvent = "event"
)

// SyncLogger is the minimal logger surface passed to sync job handlers.
type SyncLogger interface {
	Info(msg string, fields map[string]interface{})
	Error(msg string, err error, fields map[string]interface{})
}

// SyncJobContext is passed to outbound integration sync handlers at execution time.
type SyncJobContext struct {
	JobID   string
	Plugin  string
	Name    string
	Trigger string
	Payload map[string]interface{}
	DB      *sql.DB
	Logger  SyncLogger
}

// SyncJobHandler executes one queued sync job. Implementations must be idempotent.
type SyncJobHandler func(ctx context.Context, job SyncJobContext) error

// SyncTrigger describes when a sync job should enqueue work.
type SyncTrigger struct {
	Kind      string
	CronSpec  string
	EventName string
}

// SyncJob registers an outbound integration sync handler.
type SyncJob struct {
	Name       string
	Trigger    SyncTrigger
	Handler    SyncJobHandler
	MaxRetries int
}

// OnEvent returns an event trigger for RegisterSyncJob.
func OnEvent(eventName string) SyncTrigger {
	return SyncTrigger{Kind: SyncTriggerEvent, EventName: strings.TrimSpace(eventName)}
}

// Cron returns a cron trigger for RegisterSyncJob.
// Accepts 5-field cron syntax or "@every 5m" / "@every 1h" shorthands.
func Cron(spec string) SyncTrigger {
	return SyncTrigger{Kind: SyncTriggerCron, CronSpec: strings.TrimSpace(spec)}
}

// SyncJobType builds the queue job type for a plugin slug and job name.
func SyncJobType(pluginSlug, jobName string) (string, error) {
	slug, err := NormalizePluginSlug(pluginSlug)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(jobName)
	if name == "" {
		return "", errSyncJobNameRequired
	}
	if strings.Contains(name, " ") {
		return "", errSyncJobNameInvalid
	}
	return "integration.sync." + slug + "." + name, nil
}
