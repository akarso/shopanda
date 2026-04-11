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

// Compile-time check that CollectionRepo implements catalog.CollectionRepository.
var _ catalog.CollectionRepository = (*CollectionRepo)(nil)

// CollectionRepo implements catalog.CollectionRepository using PostgreSQL.
type CollectionRepo struct {
	db *sql.DB
}

// NewCollectionRepo returns a new CollectionRepo backed by db.
func NewCollectionRepo(db *sql.DB) (*CollectionRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewCollectionRepo: nil *sql.DB")
	}
	return &CollectionRepo{db: db}, nil
}

const collectionColumns = `id, name, slug, type, rules, meta, created_at, updated_at`

func hydrateCollection(scan func(dest ...interface{}) error) (*catalog.Collection, error) {
	var id, name, slug, collType string
	var rulesJSON, metaJSON []byte
	var createdAt, updatedAt time.Time

	err := scan(&id, &name, &slug, &collType, &rulesJSON, &metaJSON, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	rules := make(map[string]interface{})
	if len(rulesJSON) > 0 {
		if err := json.Unmarshal(rulesJSON, &rules); err != nil {
			return nil, fmt.Errorf("collection_repo: unmarshal rules: %w", err)
		}
	}

	meta := make(map[string]interface{})
	if len(metaJSON) > 0 {
		if err := json.Unmarshal(metaJSON, &meta); err != nil {
			return nil, fmt.Errorf("collection_repo: unmarshal meta: %w", err)
		}
	}

	return catalog.NewCollectionFromDB(id, name, slug, catalog.CollectionType(collType), rules, meta, createdAt, updatedAt), nil
}

// FindByID returns a collection by its ID.
// Returns (nil, nil) when not found.
func (r *CollectionRepo) FindByID(ctx context.Context, id string) (*catalog.Collection, error) {
	if id == "" {
		return nil, fmt.Errorf("collection_repo: find: empty id")
	}
	q := `SELECT ` + collectionColumns + ` FROM collections WHERE id = $1`
	c, err := hydrateCollection(r.db.QueryRowContext(ctx, q, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("collection_repo: find by id: %w", err)
	}
	return c, nil
}

// FindBySlug returns a collection by its slug.
// Returns (nil, nil) when not found.
func (r *CollectionRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Collection, error) {
	if slug == "" {
		return nil, fmt.Errorf("collection_repo: find by slug: empty slug")
	}
	q := `SELECT ` + collectionColumns + ` FROM collections WHERE slug = $1`
	c, err := hydrateCollection(r.db.QueryRowContext(ctx, q, slug).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("collection_repo: find by slug: %w", err)
	}
	return c, nil
}

// List returns all collections ordered by name asc.
func (r *CollectionRepo) List(ctx context.Context) ([]catalog.Collection, error) {
	q := `SELECT ` + collectionColumns + ` FROM collections ORDER BY name ASC`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("collection_repo: list: %w", err)
	}
	defer rows.Close()

	var collections []catalog.Collection
	for rows.Next() {
		c, err := hydrateCollection(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("collection_repo: list: scan: %w", err)
		}
		collections = append(collections, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("collection_repo: list: rows: %w", err)
	}
	return collections, nil
}

// Create persists a new collection.
func (r *CollectionRepo) Create(ctx context.Context, c *catalog.Collection) error {
	if c == nil {
		return fmt.Errorf("collection_repo: create: collection must not be nil")
	}
	rulesJSON, err := json.Marshal(c.Rules)
	if err != nil {
		return fmt.Errorf("collection_repo: create: marshal rules: %w", err)
	}
	metaJSON, err := json.Marshal(c.Meta)
	if err != nil {
		return fmt.Errorf("collection_repo: create: marshal meta: %w", err)
	}
	const q = `INSERT INTO collections (id, name, slug, type, rules, meta, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	_, err = r.db.ExecContext(ctx, q,
		c.ID, c.Name, c.Slug, string(c.Type), rulesJSON, metaJSON,
		c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			switch pqErr.Constraint {
			case "collections_slug_key":
				return apperror.Conflict("collection with this slug already exists")
			case "collections_pkey":
				return apperror.Conflict("collection with this id already exists")
			default:
				return apperror.Conflict("collection unique constraint violation")
			}
		}
		return fmt.Errorf("collection_repo: create: %w", err)
	}
	return nil
}

// Update persists changes to an existing collection.
func (r *CollectionRepo) Update(ctx context.Context, c *catalog.Collection) error {
	if c == nil {
		return fmt.Errorf("collection_repo: update: collection must not be nil")
	}
	rulesJSON, err := json.Marshal(c.Rules)
	if err != nil {
		return fmt.Errorf("collection_repo: update: marshal rules: %w", err)
	}
	metaJSON, err := json.Marshal(c.Meta)
	if err != nil {
		return fmt.Errorf("collection_repo: update: marshal meta: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("collection_repo: update: begin tx: %w", err)
	}
	defer tx.Rollback()

	// If switching to dynamic, refuse when manual assignments exist.
	if c.Type == catalog.CollectionDynamic {
		var count int
		err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM collection_products WHERE collection_id = $1`, c.ID,
		).Scan(&count)
		if err != nil {
			return fmt.Errorf("collection_repo: update: count assignments: %w", err)
		}
		if count > 0 {
			return apperror.Validation("cannot change to dynamic: collection has manual product assignments")
		}
	}

	newUpdatedAt := time.Now().UTC()
	const q = `UPDATE collections SET name = $1, slug = $2, type = $3, rules = $4, meta = $5, updated_at = $6 WHERE id = $7`
	res, err := tx.ExecContext(ctx, q,
		c.Name, c.Slug, string(c.Type), rulesJSON, metaJSON,
		newUpdatedAt, c.ID,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) {
			if pqErr.Code == "23505" {
				switch pqErr.Constraint {
				case "collections_slug_key":
					return apperror.Conflict("collection with this slug already exists")
				default:
					return apperror.Conflict("collection unique constraint violation")
				}
			}
		}
		return fmt.Errorf("collection_repo: update: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("collection_repo: update: rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("collection not found")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("collection_repo: update: commit: %w", err)
	}
	c.UpdatedAt = newUpdatedAt
	return nil
}

// AddProduct assigns a product to a manual collection.
func (r *CollectionRepo) AddProduct(ctx context.Context, collectionID, productID string) error {
	if collectionID == "" {
		return fmt.Errorf("collection_repo: add product: empty collection id")
	}
	if productID == "" {
		return fmt.Errorf("collection_repo: add product: empty product id")
	}
	const q = `INSERT INTO collection_products (collection_id, product_id)
		SELECT $1, $2 FROM collections WHERE id = $1 AND type = 'manual'`
	res, err := r.db.ExecContext(ctx, q, collectionID, productID)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) {
			if pqErr.Code == "23505" {
				return apperror.Conflict("product already in collection")
			}
			if pqErr.Code == "23503" {
				return apperror.NotFound("product not found")
			}
		}
		return fmt.Errorf("collection_repo: add product: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("collection_repo: add product: rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.Validation("collection not found or not a manual collection")
	}
	return nil
}

// RemoveProduct removes a product from a manual collection.
func (r *CollectionRepo) RemoveProduct(ctx context.Context, collectionID, productID string) error {
	if collectionID == "" {
		return fmt.Errorf("collection_repo: remove product: empty collection id")
	}
	if productID == "" {
		return fmt.Errorf("collection_repo: remove product: empty product id")
	}
	const q = `DELETE FROM collection_products WHERE collection_id = $1 AND product_id = $2`
	res, err := r.db.ExecContext(ctx, q, collectionID, productID)
	if err != nil {
		return fmt.Errorf("collection_repo: remove product: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("collection_repo: remove product: rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("product not in collection")
	}
	return nil
}

// ListProductIDs returns the product IDs assigned to a manual collection,
// ordered by product_id asc.
func (r *CollectionRepo) ListProductIDs(ctx context.Context, collectionID string) ([]string, error) {
	if collectionID == "" {
		return nil, fmt.Errorf("collection_repo: list products: empty collection id")
	}
	const q = `SELECT product_id FROM collection_products WHERE collection_id = $1 ORDER BY product_id ASC`
	rows, err := r.db.QueryContext(ctx, q, collectionID)
	if err != nil {
		return nil, fmt.Errorf("collection_repo: list products: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("collection_repo: list products: scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("collection_repo: list products: rows: %w", err)
	}
	return ids, nil
}
