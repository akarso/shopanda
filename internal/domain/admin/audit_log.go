package admin

import (
	"context"
	"time"
)

// AuditLogRecord is a persisted admin audit event.
type AuditLogRecord struct {
	ID           string
	CreatedAt    time.Time
	AdminID      string
	Action       string
	ResourceType string
	ResourceID   string
	Result       string
	ErrorMessage string
	StoreID      string
	Language     string
	Currency     string
	Metadata     map[string]interface{}
}

// AuditLogFilter selects audit log rows for admin listing.
type AuditLogFilter struct {
	Action       string
	ResourceType string
	ResourceID   string
	From         *time.Time
	To           *time.Time
	Offset       int
	Limit        int
}

// AuditLogRepository persists and queries admin audit entries.
type AuditLogRepository interface {
	Insert(ctx context.Context, record AuditLogRecord) error
	List(ctx context.Context, filter AuditLogFilter) ([]AuditLogRecord, error)
}
