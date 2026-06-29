package admin

import (
	"context"
	"time"

	"github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/jobs"
)

// RetentionJobType is the job type string for audit log retention cleanup.
const RetentionJobType = "audit.retention"

// AuditRetentionDeleter removes audit rows older than a cutoff.
type AuditRetentionDeleter interface {
	DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// RetentionLogger logs retention job outcomes.
type RetentionLogger interface {
	Info(msg string, fields map[string]interface{})
}

// RetentionHandler processes audit.retention jobs.
type RetentionHandler struct {
	repo   AuditRetentionDeleter
	config admin.ConfigGetter
	clock  func() time.Time
	log    RetentionLogger
}

// NewRetentionHandler creates a handler for audit.retention jobs.
func NewRetentionHandler(repo AuditRetentionDeleter, config admin.ConfigGetter, log RetentionLogger) *RetentionHandler {
	if repo == nil {
		panic("admin.NewRetentionHandler: nil repo")
	}
	if log == nil {
		panic("admin.NewRetentionHandler: nil logger")
	}
	return &RetentionHandler{
		repo:   repo,
		config: config,
		clock:  time.Now,
		log:    log,
	}
}

// Type returns the job type this handler processes.
func (h *RetentionHandler) Type() string { return RetentionJobType }

// Handle deletes audit rows older than the configured retention window.
func (h *RetentionHandler) Handle(ctx context.Context, _ jobs.Job) error {
	days, err := admin.RetentionDays(ctx, h.config)
	if err != nil {
		return err
	}
	if days <= 0 {
		h.log.Info("audit.retention.skipped", map[string]interface{}{
			"reason": "retention disabled",
		})
		return nil
	}
	cutoff := h.clock().UTC().AddDate(0, 0, -days)
	deleted, err := h.repo.DeleteBefore(ctx, cutoff)
	if err != nil {
		return err
	}
	h.log.Info("audit.retention.complete", map[string]interface{}{
		"retention_days": days,
		"cutoff":         cutoff.Format(time.RFC3339),
		"deleted":        deleted,
	})
	return nil
}
