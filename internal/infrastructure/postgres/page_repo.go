package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/lib/pq"
)

// Compile-time check that PageRepo implements cms.PageRepository.
var _ cms.PageRepository = (*PageRepo)(nil)

// PageRepo implements cms.PageRepository using PostgreSQL.
type PageRepo struct {
	db *sql.DB
}

// NewPageRepo returns a new PageRepo backed by db.
func NewPageRepo(db *sql.DB) (*PageRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewPageRepo: nil *sql.DB")
	}
	return &PageRepo{db: db}, nil
}

const pageColumns = `id, slug, title, content, is_active, created_at, updated_at`

func hydratePage(scan func(dest ...interface{}) error) (*cms.Page, error) {
	var id, slug, title, content string
	var isActive bool
	var createdAt, updatedAt time.Time

	if err := scan(&id, &slug, &title, &content, &isActive, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	return cms.NewPageFromDB(id, slug, title, content, isActive, createdAt, updatedAt), nil
}

// FindByID returns a page by its ID.
// Returns (nil, nil) when not found.
func (r *PageRepo) FindByID(ctx context.Context, id string) (*cms.Page, error) {
	if id == "" {
		return nil, fmt.Errorf("page_repo: find: empty id")
	}
	q := `SELECT ` + pageColumns + ` FROM pages WHERE id = $1`
	p, err := hydratePage(r.db.QueryRowContext(ctx, q, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("page_repo: find by id: %w", err)
	}
	return p, nil
}

// FindBySlug returns a page by its slug regardless of active status.
// Returns (nil, nil) when not found.
func (r *PageRepo) FindBySlug(ctx context.Context, slug string) (*cms.Page, error) {
	if slug == "" {
		return nil, fmt.Errorf("page_repo: find by slug: empty slug")
	}
	q := `SELECT ` + pageColumns + ` FROM pages WHERE slug = $1`
	p, err := hydratePage(r.db.QueryRowContext(ctx, q, slug).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("page_repo: find by slug: %w", err)
	}
	return p, nil
}

// FindActiveBySlug returns an active page by its slug.
// Returns (nil, nil) when not found or inactive.
func (r *PageRepo) FindActiveBySlug(ctx context.Context, slug string) (*cms.Page, error) {
	if slug == "" {
		return nil, fmt.Errorf("page_repo: find active by slug: empty slug")
	}
	q := `SELECT ` + pageColumns + ` FROM pages WHERE slug = $1 AND is_active = true`
	p, err := hydratePage(r.db.QueryRowContext(ctx, q, slug).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("page_repo: find active by slug: %w", err)
	}
	return p, nil
}

// List returns pages ordered by created_at desc with pagination.
func (r *PageRepo) List(ctx context.Context, offset, limit int) ([]*cms.Page, error) {
	q := `SELECT ` + pageColumns + ` FROM pages ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := r.db.QueryContext(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("page_repo: list: %w", err)
	}
	defer rows.Close()

	var pages []*cms.Page
	for rows.Next() {
		p, err := hydratePage(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("page_repo: list: scan: %w", err)
		}
		pages = append(pages, p)
	}
	return pages, rows.Err()
}

// Create inserts a new page.
func (r *PageRepo) Create(ctx context.Context, p *cms.Page) error {
	if p == nil {
		return fmt.Errorf("page_repo: create: nil page")
	}
	q := `INSERT INTO pages (id, slug, title, content, is_active, created_at, updated_at)
	      VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.db.ExecContext(ctx, q, p.ID(), p.Slug(), p.Title(), p.Content(), p.IsActive(), p.CreatedAt(), p.UpdatedAt())
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			if pqErr.Constraint == "pages_slug_key" {
				return apperror.Conflict("page with this slug already exists")
			}
			return apperror.Conflict("page with this id already exists")
		}
		return fmt.Errorf("page_repo: create: %w", err)
	}
	return nil
}

// Update saves changes to an existing page.
func (r *PageRepo) Update(ctx context.Context, p *cms.Page) error {
	if p == nil {
		return fmt.Errorf("page_repo: update: nil page")
	}
	q := `UPDATE pages SET slug = $2, title = $3, content = $4, is_active = $5, updated_at = $6
	      WHERE id = $1`
	result, err := r.db.ExecContext(ctx, q, p.ID(), p.Slug(), p.Title(), p.Content(), p.IsActive(), p.UpdatedAt())
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return apperror.Conflict("page with this slug already exists")
		}
		return fmt.Errorf("page_repo: update: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("page_repo: update: rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("page not found")
	}
	return nil
}

// Delete removes a page by its ID.
func (r *PageRepo) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("page_repo: delete: empty id")
	}
	q := `DELETE FROM pages WHERE id = $1`
	result, err := r.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("page_repo: delete: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("page_repo: delete: rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("page not found")
	}
	return nil
}
