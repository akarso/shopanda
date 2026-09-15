package cache

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/event"
)

// JobType is the job type string for cache cleanup.
const JobType = "cache.cleanup"

// ExpiredDeleter removes cache entries whose TTL has elapsed.
type ExpiredDeleter interface {
	DeleteExpired(ctx context.Context) (int64, error)
}

// BusSetter is implemented by a cache.Cache backend that can publish
// cache.EventInvalidated when DeleteByTag/DeleteByPrefix removes entries
// (PR-1040) — see cache.EventInvalidated's own doc comment for what
// that enables and its real limits. Both core backends
// (postgres.CacheStore, redis.CacheStore) implement it; a plugin-
// provided cache.Cache is not required to.
type BusSetter interface {
	SetBus(bus *event.Bus)
}

// Logger is the logging interface used by cache application services.
type Logger interface {
	Info(msg string, fields map[string]interface{})
	Error(msg string, err error, fields map[string]interface{})
}

// CleanupHandler processes cache.cleanup jobs by removing expired entries.
type CleanupHandler struct {
	deleter ExpiredDeleter
	log     Logger
}

// NewCleanupHandler creates a handler for cache.cleanup jobs.
func NewCleanupHandler(deleter ExpiredDeleter, log Logger) *CleanupHandler {
	if deleter == nil {
		panic("cache.NewCleanupHandler: nil deleter")
	}
	if log == nil {
		panic("cache.NewCleanupHandler: nil logger")
	}
	return &CleanupHandler{deleter: deleter, log: log}
}

// Type returns the job type this handler processes.
func (h *CleanupHandler) Type() string { return JobType }

// Handle removes expired cache entries and logs the result.
func (h *CleanupHandler) Handle(ctx context.Context, _ jobs.Job) error {
	deleted, err := h.deleter.DeleteExpired(ctx)
	if err != nil {
		return err
	}
	h.log.Info("cache.cleanup.complete", map[string]interface{}{
		"deleted": deleted,
	})
	return nil
}
