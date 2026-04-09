package cache

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/jobs"
)

// JobType is the job type string for cache cleanup.
const JobType = "cache.cleanup"

// ExpiredDeleter removes cache entries whose TTL has elapsed.
type ExpiredDeleter interface {
	DeleteExpired() (int64, error)
}

// Logger is the logging interface used by CleanupHandler.
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
func (h *CleanupHandler) Handle(_ context.Context, _ jobs.Job) error {
	deleted, err := h.deleter.DeleteExpired()
	if err != nil {
		return err
	}
	h.log.Info("cache.cleanup.complete", map[string]interface{}{
		"deleted": deleted,
	})
	return nil
}
