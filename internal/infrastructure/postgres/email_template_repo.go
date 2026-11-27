package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/jackc/pgx/v5/pgconn"
)

var _ mail.EmailTemplateRepository = (*EmailTemplateRepo)(nil)

// EmailTemplateRepo persists merchant email template overrides.
type EmailTemplateRepo struct {
	db *sql.DB
}

// NewEmailTemplateRepo returns a repo backed by db.
func NewEmailTemplateRepo(db *sql.DB) (*EmailTemplateRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewEmailTemplateRepo: nil *sql.DB")
	}
	return &EmailTemplateRepo{db: db}, nil
}

func mapEmailTemplateExecError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23503":
		return apperror.NotFound("store not found")
	case "23514":
		msg := "email template constraint violation"
		if pgErr.ConstraintName != "" {
			msg = "email template constraint violation: " + pgErr.ConstraintName
		}
		return apperror.Validation(msg)
	case "23505":
		return apperror.Conflict("email template already exists")
	default:
		return err
	}
}

// Get returns the override or (nil, nil) when missing.
func (r *EmailTemplateRepo) Get(ctx context.Context, storeID, name, locale string) (*mail.EmailTemplate, error) {
	storeID = strings.TrimSpace(storeID)
	name = strings.TrimSpace(name)
	locale, err := mail.ParseTemplateLocale(locale)
	if err != nil {
		return nil, apperror.Validation(err.Error())
	}
	if storeID == "" || name == "" {
		return nil, nil
	}
	const q = `
SELECT id, store_id, name, locale, subject, body, updated_at, updated_by
FROM email_templates
WHERE store_id = $1 AND name = $2 AND locale = $3`
	var (
		row       mail.EmailTemplate
		updatedBy sql.NullString
		updatedAt time.Time
	)
	err = r.db.QueryRowContext(ctx, q, storeID, name, locale).Scan(
		&row.ID, &row.StoreID, &row.Name, &row.Locale, &row.Subject, &row.Body, &updatedAt, &updatedBy,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("email_template_repo: get: %w", err)
	}
	row.UpdatedAt = updatedAt
	if updatedBy.Valid {
		row.UpdatedBy = updatedBy.String
	}
	return &row, nil
}

// Upsert inserts or replaces an override for (store_id, name, locale).
func (r *EmailTemplateRepo) Upsert(ctx context.Context, t *mail.EmailTemplate) error {
	if t == nil {
		return apperror.Validation("email template must not be nil")
	}
	if err := t.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	if strings.TrimSpace(t.ID) == "" {
		t.ID = id.New()
	}
	// Both insert and conflict update use Postgres now() so updated_at cannot
	// move backwards under Go/DB clock skew.
	const q = `
INSERT INTO email_templates (id, store_id, name, locale, subject, body, updated_at, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, now(), NULLIF($7, ''))
ON CONFLICT (store_id, name, locale) DO UPDATE SET
	subject = EXCLUDED.subject,
	body = EXCLUDED.body,
	updated_at = now(),
	updated_by = EXCLUDED.updated_by
RETURNING id, updated_at`
	err := r.db.QueryRowContext(ctx, q,
		t.ID, t.StoreID, t.Name, t.Locale, t.Subject, t.Body, t.UpdatedBy,
	).Scan(&t.ID, &t.UpdatedAt)
	if err != nil {
		if mapped := mapEmailTemplateExecError(err); mapped != err {
			return mapped
		}
		return fmt.Errorf("email_template_repo: upsert: %w", err)
	}
	return nil
}

// Delete removes the override. Missing rows are a no-op.
func (r *EmailTemplateRepo) Delete(ctx context.Context, storeID, name, locale string) error {
	storeID = strings.TrimSpace(storeID)
	name = strings.TrimSpace(name)
	locale, err := mail.ParseTemplateLocale(locale)
	if err != nil {
		return apperror.Validation(err.Error())
	}
	if storeID == "" || name == "" {
		return nil
	}
	const q = `DELETE FROM email_templates WHERE store_id = $1 AND name = $2 AND locale = $3`
	_, err = r.db.ExecContext(ctx, q, storeID, name, locale)
	if err != nil {
		return fmt.Errorf("email_template_repo: delete: %w", err)
	}
	return nil
}

// ListByStore returns all overrides for a store, newest first.
func (r *EmailTemplateRepo) ListByStore(ctx context.Context, storeID string) ([]mail.EmailTemplate, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, nil
	}
	const q = `
SELECT id, store_id, name, locale, subject, body, updated_at, updated_by
FROM email_templates
WHERE store_id = $1
ORDER BY updated_at DESC, name ASC, locale ASC`
	rows, err := r.db.QueryContext(ctx, q, storeID)
	if err != nil {
		return nil, fmt.Errorf("email_template_repo: list: %w", err)
	}
	defer rows.Close()

	var out []mail.EmailTemplate
	for rows.Next() {
		var (
			row       mail.EmailTemplate
			updatedBy sql.NullString
			updatedAt time.Time
		)
		if err := rows.Scan(
			&row.ID, &row.StoreID, &row.Name, &row.Locale, &row.Subject, &row.Body, &updatedAt, &updatedBy,
		); err != nil {
			return nil, fmt.Errorf("email_template_repo: list scan: %w", err)
		}
		row.UpdatedAt = updatedAt
		if updatedBy.Valid {
			row.UpdatedBy = updatedBy.String
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("email_template_repo: list rows: %w", err)
	}
	return out, nil
}

// ListMetaByStore returns name/locale/updated metadata without subject/body.
func (r *EmailTemplateRepo) ListMetaByStore(ctx context.Context, storeID string) ([]mail.EmailTemplateMeta, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, nil
	}
	const q = `
SELECT name, locale, updated_at, updated_by
FROM email_templates
WHERE store_id = $1
ORDER BY name ASC, locale ASC`
	rows, err := r.db.QueryContext(ctx, q, storeID)
	if err != nil {
		return nil, fmt.Errorf("email_template_repo: list meta: %w", err)
	}
	defer rows.Close()

	var out []mail.EmailTemplateMeta
	for rows.Next() {
		var (
			row       mail.EmailTemplateMeta
			updatedBy sql.NullString
			updatedAt time.Time
		)
		if err := rows.Scan(&row.Name, &row.Locale, &updatedAt, &updatedBy); err != nil {
			return nil, fmt.Errorf("email_template_repo: list meta scan: %w", err)
		}
		row.UpdatedAt = updatedAt
		if updatedBy.Valid {
			row.UpdatedBy = updatedBy.String
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("email_template_repo: list meta rows: %w", err)
	}
	return out, nil
}
