package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	domainwebhook "github.com/akarso/shopanda/internal/domain/webhook"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

var _ domainwebhook.Repository = (*WebhookEndpointRepo)(nil)

// WebhookEndpointRepo implements domainwebhook.Repository.
type WebhookEndpointRepo struct {
	db *sql.DB
}

// NewWebhookEndpointRepo returns a WebhookEndpointRepo backed by db.
func NewWebhookEndpointRepo(db *sql.DB) (*WebhookEndpointRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewWebhookEndpointRepo: nil *sql.DB")
	}
	return &WebhookEndpointRepo{db: db}, nil
}

func (r *WebhookEndpointRepo) List(ctx context.Context) ([]domainwebhook.Endpoint, error) {
	const q = `SELECT id, url, secret, events, active, description, created_at, updated_at
		FROM webhook_endpoints ORDER BY created_at DESC, id DESC`
	return r.queryEndpoints(ctx, q)
}

func (r *WebhookEndpointRepo) ListActive(ctx context.Context) ([]domainwebhook.Endpoint, error) {
	const q = `SELECT id, url, secret, events, active, description, created_at, updated_at
		FROM webhook_endpoints WHERE active = true ORDER BY created_at DESC, id DESC`
	return r.queryEndpoints(ctx, q)
}

func (r *WebhookEndpointRepo) FindByID(ctx context.Context, endpointID string) (*domainwebhook.Endpoint, error) {
	endpointID = strings.TrimSpace(endpointID)
	if endpointID == "" {
		return nil, nil
	}
	const q = `SELECT id, url, secret, events, active, description, created_at, updated_at
		FROM webhook_endpoints WHERE id = $1`
	rows, err := r.queryEndpoints(ctx, q, endpointID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (r *WebhookEndpointRepo) Create(ctx context.Context, endpoint *domainwebhook.Endpoint) error {
	if endpoint == nil {
		return fmt.Errorf("webhook_endpoint_repo: nil endpoint")
	}
	endpointID := endpoint.ID
	if endpointID == "" {
		endpointID = id.New()
	}
	now := time.Now().UTC()
	if endpoint.CreatedAt.IsZero() {
		endpoint.CreatedAt = now
	}
	endpoint.UpdatedAt = now

	const q = `INSERT INTO webhook_endpoints (id, url, secret, events, active, description, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`
	_, err := r.db.ExecContext(ctx, q,
		endpointID, endpoint.URL, endpoint.Secret, endpoint.Events,
		endpoint.Active, endpoint.Description, endpoint.CreatedAt, endpoint.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("webhook_endpoint_repo: create: %w", err)
	}
	endpoint.ID = endpointID
	return nil
}

func (r *WebhookEndpointRepo) Update(ctx context.Context, endpoint *domainwebhook.Endpoint) error {
	if endpoint == nil || strings.TrimSpace(endpoint.ID) == "" {
		return fmt.Errorf("webhook_endpoint_repo: endpoint id is required")
	}
	endpoint.UpdatedAt = time.Now().UTC()
	const q = `UPDATE webhook_endpoints
		SET url = $1, secret = $2, events = $3, active = $4, description = $5, updated_at = $6
		WHERE id = $7`
	res, err := r.db.ExecContext(ctx, q,
		endpoint.URL, endpoint.Secret, endpoint.Events,
		endpoint.Active, endpoint.Description, endpoint.UpdatedAt, endpoint.ID,
	)
	if err != nil {
		return fmt.Errorf("webhook_endpoint_repo: update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("webhook_endpoint_repo: update rows: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("webhook endpoint not found")
	}
	return nil
}

func (r *WebhookEndpointRepo) Delete(ctx context.Context, endpointID string) error {
	endpointID = strings.TrimSpace(endpointID)
	if endpointID == "" {
		return fmt.Errorf("webhook_endpoint_repo: endpoint id is required")
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM webhook_endpoints WHERE id = $1`, endpointID)
	if err != nil {
		return fmt.Errorf("webhook_endpoint_repo: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("webhook_endpoint_repo: delete rows: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("webhook endpoint not found")
	}
	return nil
}

func (r *WebhookEndpointRepo) queryEndpoints(ctx context.Context, q string, args ...interface{}) ([]domainwebhook.Endpoint, error) {
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("webhook_endpoint_repo: query: %w", err)
	}
	defer rows.Close()

	out := make([]domainwebhook.Endpoint, 0)
	for rows.Next() {
		var ep domainwebhook.Endpoint
		if err := rows.Scan(
			&ep.ID, &ep.URL, &ep.Secret, pgTypeScanner(&ep.Events), &ep.Active,
			&ep.Description, &ep.CreatedAt, &ep.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("webhook_endpoint_repo: scan: %w", err)
		}
		out = append(out, ep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("webhook_endpoint_repo: rows: %w", err)
	}
	return out, nil
}
