package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/lib/pq"
)

// Compile-time check that CategoryRepo implements catalog.CategoryRepository.
var _ catalog.CategoryRepository = (*CategoryRepo)(nil)

// CategoryRepo implements catalog.CategoryRepository using PostgreSQL.
type CategoryRepo struct {
	db *sql.DB
}

// NewCategoryRepo returns a new CategoryRepo backed by db.
func NewCategoryRepo(db *sql.DB) *CategoryRepo {
	return &CategoryRepo{db: db}
}

const categoryColumns = `id, parent_id, name, slug, position, meta, created_at, updated_at`

func hydrateCategory(scan func(dest ...interface{}) error) (*catalog.Category, error) {
	var id, name, slug string
	var parentID sql.NullString
	var position int
	var metaJSON []byte
	var createdAt, updatedAt time.Time

	err := scan(&id, &parentID, &name, &slug, &position, &metaJSON, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	var pID *string
	if parentID.Valid {
		pID = &parentID.String
	}

	meta := make(map[string]interface{})
	if len(metaJSON) > 0 {
		if err := json.Unmarshal(metaJSON, &meta); err != nil {
			return nil, fmt.Errorf("category_repo: unmarshal meta: %w", err)
		}
	}

	return catalog.NewCategoryFromDB(id, pID, name, slug, position, meta, createdAt, updatedAt), nil
}

// FindByID returns a category by its ID.
// Returns (nil, nil) when not found.
func (r *CategoryRepo) FindByID(ctx context.Context, id string) (*catalog.Category, error) {
	if id == "" {
		return nil, fmt.Errorf("category_repo: find: empty id")
	}
	q := `SELECT ` + categoryColumns + ` FROM categories WHERE id = $1`
	c, err := hydrateCategory(r.db.QueryRowContext(ctx, q, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("category_repo: find by id: %w", err)
	}
	return c, nil
}

// FindBySlug returns a category by its slug.
// Returns (nil, nil) when not found.
func (r *CategoryRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Category, error) {
	if slug == "" {
		return nil, fmt.Errorf("category_repo: find by slug: empty slug")
	}
	q := `SELECT ` + categoryColumns + ` FROM categories WHERE slug = $1`
	c, err := hydrateCategory(r.db.QueryRowContext(ctx, q, slug).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("category_repo: find by slug: %w", err)
	}
	return c, nil
}

// FindByParentID returns child categories of the given parent,
// ordered by position asc, then name asc.
// Pass nil parentID to get root categories.
func (r *CategoryRepo) FindByParentID(ctx context.Context, parentID *string) ([]catalog.Category, error) {
	var rows *sql.Rows
	var err error

	if parentID == nil {
		q := `SELECT ` + categoryColumns + ` FROM categories WHERE parent_id IS NULL ORDER BY position ASC, name ASC`
		rows, err = r.db.QueryContext(ctx, q)
	} else {
		q := `SELECT ` + categoryColumns + ` FROM categories WHERE parent_id = $1 ORDER BY position ASC, name ASC`
		rows, err = r.db.QueryContext(ctx, q, *parentID)
	}
	if err != nil {
		return nil, fmt.Errorf("category_repo: find by parent id: %w", err)
	}
	defer rows.Close()

	var categories []catalog.Category
	for rows.Next() {
		c, err := hydrateCategory(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("category_repo: find by parent id: scan: %w", err)
		}
		categories = append(categories, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("category_repo: find by parent id: rows: %w", err)
	}
	return categories, nil
}

// Create persists a new category.
func (r *CategoryRepo) Create(ctx context.Context, c *catalog.Category) error {
	if c == nil {
		return fmt.Errorf("category_repo: create: category must not be nil")
	}
	metaJSON, err := json.Marshal(c.Meta)
	if err != nil {
		return fmt.Errorf("category_repo: create: marshal meta: %w", err)
	}
	var parentID sql.NullString
	if c.ParentID != nil {
		parentID = sql.NullString{String: *c.ParentID, Valid: true}
	}
	const q = `INSERT INTO categories (id, parent_id, name, slug, position, meta, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err = r.db.ExecContext(ctx, q,
		c.ID, parentID, c.Name, c.Slug, c.Position, metaJSON,
		c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			switch pqErr.Constraint {
			case "categories_slug_key":
				return apperror.Conflict("category with this slug already exists")
			case "categories_pkey":
				return apperror.Conflict("category with this id already exists")
			default:
				return apperror.Conflict("category unique constraint violation")
			}
		}
		return fmt.Errorf("category_repo: create: %w", err)
	}
	return nil
}

// Update persists changes to an existing category.
func (r *CategoryRepo) Update(ctx context.Context, c *catalog.Category) error {
	if c == nil {
		return fmt.Errorf("category_repo: update: category must not be nil")
	}
	metaJSON, err := json.Marshal(c.Meta)
	if err != nil {
		return fmt.Errorf("category_repo: update: marshal meta: %w", err)
	}
	var parentID sql.NullString
	if c.ParentID != nil {
		parentID = sql.NullString{String: *c.ParentID, Valid: true}
	}
	newUpdatedAt := time.Now().UTC()
	const q = `UPDATE categories SET parent_id = $1, name = $2, slug = $3, position = $4, meta = $5, updated_at = $6 WHERE id = $7`
	res, err := r.db.ExecContext(ctx, q,
		parentID, c.Name, c.Slug, c.Position, metaJSON,
		newUpdatedAt, c.ID,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			switch pqErr.Constraint {
			case "categories_slug_key":
				return apperror.Conflict("category with this slug already exists")
			default:
				return apperror.Conflict("category unique constraint violation")
			}
		}
		return fmt.Errorf("category_repo: update: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("category_repo: update: rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("category not found")
	}
	c.UpdatedAt = newUpdatedAt
	return nil
}

// FindAll returns all categories ordered by position asc, then name asc.
func (r *CategoryRepo) FindAll(ctx context.Context) ([]catalog.Category, error) {
	q := `SELECT ` + categoryColumns + ` FROM categories ORDER BY position ASC, name ASC`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("category_repo: find all: %w", err)
	}
	defer rows.Close()

	var categories []catalog.Category
	for rows.Next() {
		c, err := hydrateCategory(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("category_repo: find all: scan: %w", err)
		}
		categories = append(categories, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("category_repo: find all: rows: %w", err)
	}
	return categories, nil
}
