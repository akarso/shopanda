package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// IntegrationIdempotencyAdminRecord is a read model for admin list/detail APIs.
type IntegrationIdempotencyAdminRecord struct {
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

// IntegrationIdempotencyListFilter selects inbound idempotency rows for admin listing.
type IntegrationIdempotencyListFilter struct {
	PluginSlug string
	Completed  *bool
	Offset     int
	Limit      int
}

// List returns idempotency records ordered by newest first.
func (r *IntegrationIdempotencyRepo) List(ctx context.Context, filter IntegrationIdempotencyListFilter) ([]IntegrationIdempotencyAdminRecord, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("integration idempotency: repository not configured")
	}
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	plugin := strings.TrimSpace(filter.PluginSlug)
	args := []interface{}{plugin, filter.Completed, filter.Offset, filter.Limit}
	const q = `SELECT plugin_slug, idempotency_key, method, path, request_hash, status_code, response_body, completed, created_at, expires_at
		FROM integration_idempotency
		WHERE ($1 = '' OR plugin_slug = $1)
		  AND ($2::boolean IS NULL OR completed = $2)
		ORDER BY created_at DESC
		OFFSET $3 LIMIT $4`

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("integration idempotency list: %w", err)
	}
	defer rows.Close()

	out := make([]IntegrationIdempotencyAdminRecord, 0, filter.Limit)
	for rows.Next() {
		item, err := scanIntegrationIdempotencyAdminRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("integration idempotency list rows: %w", err)
	}
	return out, nil
}

// Get returns one idempotency record by plugin slug and key.
func (r *IntegrationIdempotencyRepo) Get(ctx context.Context, pluginSlug, key string) (*IntegrationIdempotencyAdminRecord, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("integration idempotency: repository not configured")
	}
	pluginSlug = strings.TrimSpace(pluginSlug)
	key = strings.TrimSpace(key)
	if pluginSlug == "" || key == "" {
		return nil, fmt.Errorf("integration idempotency: plugin and key required")
	}

	const q = `SELECT plugin_slug, idempotency_key, method, path, request_hash, status_code, response_body, completed, created_at, expires_at
		FROM integration_idempotency
		WHERE plugin_slug = $1 AND idempotency_key = $2`
	row := r.db.QueryRowContext(ctx, q, pluginSlug, key)
	item, err := scanIntegrationIdempotencyAdminRecord(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("integration idempotency get: %w", err)
	}
	return &item, nil
}

type idempotencyAdminScanner interface {
	Scan(dest ...interface{}) error
}

func scanIntegrationIdempotencyAdminRecord(row idempotencyAdminScanner) (IntegrationIdempotencyAdminRecord, error) {
	var item IntegrationIdempotencyAdminRecord
	var body []byte
	err := row.Scan(
		&item.PluginSlug,
		&item.IdempotencyKey,
		&item.Method,
		&item.Path,
		&item.RequestHash,
		&item.StatusCode,
		&body,
		&item.Completed,
		&item.CreatedAt,
		&item.ExpiresAt,
	)
	if err != nil {
		return IntegrationIdempotencyAdminRecord{}, err
	}
	item.ResponseBody = append([]byte(nil), body...)
	return item, nil
}
