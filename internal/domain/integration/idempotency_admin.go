package integration

import (
	"context"
	"time"
)

// IdempotencyAdminRecord is a read model for inbound integration idempotency admin APIs.
type IdempotencyAdminRecord struct {
	PluginSlug     string
	IdempotencyKey string
	Method         string
	Path           string
	RequestHash    string
	StatusCode     int
	ResponseBody   []byte
	Completed      bool
	CreatedAt      time.Time
	ExpiresAt      time.Time
}

// IdempotencyListFilter selects inbound idempotency rows for admin listing.
type IdempotencyListFilter struct {
	PluginSlug string
	Completed  *bool
	Offset     int
	Limit      int
}

// IdempotencyAdminRepository queries persisted inbound idempotency keys for admin inspection.
type IdempotencyAdminRepository interface {
	List(ctx context.Context, filter IdempotencyListFilter) ([]IdempotencyAdminRecord, error)
	Get(ctx context.Context, pluginSlug, key string) (*IdempotencyAdminRecord, error)
}
