package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/routing"
	"github.com/lib/pq"
)

// Compile-time check that RewriteRepo implements routing.RewriteRepository.
var _ routing.RewriteRepository = (*RewriteRepo)(nil)

// RewriteRepo implements routing.RewriteRepository using PostgreSQL.
type RewriteRepo struct {
	db *sql.DB
}

// NewRewriteRepo returns a new RewriteRepo backed by db.
func NewRewriteRepo(db *sql.DB) (*RewriteRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewRewriteRepo: nil *sql.DB")
	}
	return &RewriteRepo{db: db}, nil
}

func hydrateRewrite(scan func(dest ...interface{}) error) (*routing.URLRewrite, error) {
	var path, typ, entityID string
	if err := scan(&path, &typ, &entityID); err != nil {
		return nil, err
	}
	return routing.NewURLRewriteFromDB(path, typ, entityID), nil
}

// FindByPath returns a URL rewrite for the given path.
// Returns (nil, nil) when not found.
func (r *RewriteRepo) FindByPath(ctx context.Context, path string) (*routing.URLRewrite, error) {
	if path == "" {
		return nil, fmt.Errorf("rewrite_repo: find: empty path")
	}
	q := `SELECT path, type, entity_id FROM url_rewrites WHERE path = $1`
	rw, err := hydrateRewrite(r.db.QueryRowContext(ctx, q, path).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("rewrite_repo: find by path: %w", err)
	}
	return rw, nil
}

// Save inserts or updates a URL rewrite.
func (r *RewriteRepo) Save(ctx context.Context, rw *routing.URLRewrite) error {
	if rw == nil {
		return fmt.Errorf("rewrite_repo: save: nil rewrite")
	}
	q := `INSERT INTO url_rewrites (path, type, entity_id)
	      VALUES ($1, $2, $3)
	      ON CONFLICT (path) DO UPDATE SET type = $2, entity_id = $3`
	_, err := r.db.ExecContext(ctx, q, rw.Path(), rw.Type(), rw.EntityID())
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) {
			return fmt.Errorf("rewrite_repo: save: %s", pqErr.Message)
		}
		return fmt.Errorf("rewrite_repo: save: %w", err)
	}
	return nil
}

// Delete removes the URL rewrite for the given path.
func (r *RewriteRepo) Delete(ctx context.Context, path string) error {
	if path == "" {
		return fmt.Errorf("rewrite_repo: delete: empty path")
	}
	q := `DELETE FROM url_rewrites WHERE path = $1`
	_, err := r.db.ExecContext(ctx, q, path)
	if err != nil {
		return fmt.Errorf("rewrite_repo: delete: %w", err)
	}
	return nil
}
