package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/pkg/integrationhttp"
)

var _ integrationhttp.IdempotencyStore = (*IntegrationIdempotencyRepo)(nil)

// IntegrationIdempotencyRepo persists inbound integration idempotency keys in Postgres.
type IntegrationIdempotencyRepo struct {
	db *sql.DB
}

// NewIntegrationIdempotencyRepo returns an IntegrationIdempotencyRepo backed by db.
func NewIntegrationIdempotencyRepo(db *sql.DB) (*IntegrationIdempotencyRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewIntegrationIdempotencyRepo: nil *sql.DB")
	}
	return &IntegrationIdempotencyRepo{db: db}, nil
}

// Begin claims or loads an idempotency key.
func (r *IntegrationIdempotencyRepo) Begin(ctx context.Context, plugin, key, method, path, requestHash string, expiresAt time.Time) (*integrationhttp.IdempotencyRecord, bool, error) {
	plugin = strings.TrimSpace(plugin)
	key = strings.TrimSpace(key)
	if plugin == "" || key == "" {
		return nil, false, fmt.Errorf("integration idempotency: plugin and key required")
	}

	const insertQ = `INSERT INTO integration_idempotency
		(plugin_slug, idempotency_key, method, path, request_hash, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (plugin_slug, idempotency_key) DO NOTHING`
	res, err := r.db.ExecContext(ctx, insertQ, plugin, key, method, path, requestHash, expiresAt.UTC())
	if err != nil {
		return nil, false, fmt.Errorf("integration idempotency insert: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil, true, nil
	}

	const selectQ = `SELECT request_hash, status_code, response_body, completed, created_at
		FROM integration_idempotency
		WHERE plugin_slug = $1 AND idempotency_key = $2`
	var record integrationhttp.IdempotencyRecord
	var body []byte
	err = r.db.QueryRowContext(ctx, selectQ, plugin, key).Scan(
		&record.RequestHash, &record.StatusCode, &body, &record.Completed, &record.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, false, integrationhttp.ErrIdempotencyInProgress
	}
	if err != nil {
		return nil, false, fmt.Errorf("integration idempotency select: %w", err)
	}
	record.ResponseBody = append([]byte(nil), body...)
	if record.Completed {
		if record.RequestHash != requestHash {
			return nil, false, integrationhttp.ErrIdempotencyConflict
		}
		return &record, false, nil
	}
	if time.Since(record.CreatedAt) < 5*time.Minute {
		return nil, false, integrationhttp.ErrIdempotencyInProgress
	}
	const resetQ = `UPDATE integration_idempotency
		SET method = $3, path = $4, request_hash = $5, status_code = 0, response_body = '', completed = false, created_at = now(), expires_at = $6
		WHERE plugin_slug = $1 AND idempotency_key = $2 AND completed = false`
	if _, err := r.db.ExecContext(ctx, resetQ, plugin, key, method, path, requestHash, expiresAt.UTC()); err != nil {
		return nil, false, fmt.Errorf("integration idempotency reset stale: %w", err)
	}
	return nil, true, nil
}

// Complete stores the response for a claimed idempotency key.
func (r *IntegrationIdempotencyRepo) Complete(ctx context.Context, plugin, key string, statusCode int, body []byte) error {
	plugin = strings.TrimSpace(plugin)
	key = strings.TrimSpace(key)
	const q = `UPDATE integration_idempotency
		SET completed = true, status_code = $3, response_body = $4
		WHERE plugin_slug = $1 AND idempotency_key = $2 AND completed = false`
	_, err := r.db.ExecContext(ctx, q, plugin, key, statusCode, body)
	if err != nil {
		return fmt.Errorf("integration idempotency complete: %w", err)
	}
	return nil
}
