package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/lib/pq"
)

// Compile-time check that StoreRepo implements store.StoreRepository.
var _ store.StoreRepository = (*StoreRepo)(nil)

// StoreRepo implements store.StoreRepository using PostgreSQL.
type StoreRepo struct {
	db *sql.DB
}

// NewStoreRepo returns a new StoreRepo backed by db.
func NewStoreRepo(db *sql.DB) (*StoreRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewStoreRepo: nil *sql.DB")
	}
	return &StoreRepo{db: db}, nil
}

const storeColumns = `id, code, name, currency, country, domain, is_default, created_at, updated_at`

func hydrateStore(scan func(dest ...interface{}) error) (*store.Store, error) {
	var id, code, name, currency, country, domain string
	var isDefault bool
	var createdAt, updatedAt time.Time

	err := scan(&id, &code, &name, &currency, &country, &domain, &isDefault, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	return store.NewStoreFromDB(id, code, name, currency, country, domain, isDefault, createdAt, updatedAt), nil
}

// FindByID returns a store by its ID.
func (r *StoreRepo) FindByID(ctx context.Context, id string) (*store.Store, error) {
	if id == "" {
		return nil, fmt.Errorf("store_repo: find by id: empty id")
	}
	q := `SELECT ` + storeColumns + ` FROM stores WHERE id = $1`
	s, err := hydrateStore(r.db.QueryRowContext(ctx, q, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store_repo: find by id: %w", err)
	}
	return s, nil
}

// FindByCode returns a store by its unique code.
func (r *StoreRepo) FindByCode(ctx context.Context, code string) (*store.Store, error) {
	if code == "" {
		return nil, fmt.Errorf("store_repo: find by code: empty code")
	}
	q := `SELECT ` + storeColumns + ` FROM stores WHERE code = $1`
	s, err := hydrateStore(r.db.QueryRowContext(ctx, q, code).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store_repo: find by code: %w", err)
	}
	return s, nil
}

// FindByDomain returns a store by its domain.
func (r *StoreRepo) FindByDomain(ctx context.Context, domain string) (*store.Store, error) {
	if domain == "" {
		return nil, fmt.Errorf("store_repo: find by domain: empty domain")
	}
	q := `SELECT ` + storeColumns + ` FROM stores WHERE domain = $1`
	s, err := hydrateStore(r.db.QueryRowContext(ctx, q, domain).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store_repo: find by domain: %w", err)
	}
	return s, nil
}

// FindDefault returns the default store.
func (r *StoreRepo) FindDefault(ctx context.Context) (*store.Store, error) {
	q := `SELECT ` + storeColumns + ` FROM stores WHERE is_default = TRUE LIMIT 1`
	s, err := hydrateStore(r.db.QueryRowContext(ctx, q).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store_repo: find default: %w", err)
	}
	return s, nil
}

// FindAll returns all stores ordered by name asc.
func (r *StoreRepo) FindAll(ctx context.Context) ([]store.Store, error) {
	q := `SELECT ` + storeColumns + ` FROM stores ORDER BY name ASC`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("store_repo: find all: %w", err)
	}
	defer rows.Close()

	var stores []store.Store
	for rows.Next() {
		s, err := hydrateStore(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("store_repo: find all: scan: %w", err)
		}
		stores = append(stores, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store_repo: find all: rows: %w", err)
	}
	return stores, nil
}

// Create persists a new store.
func (r *StoreRepo) Create(ctx context.Context, s *store.Store) error {
	if s == nil {
		return fmt.Errorf("store_repo: create: store must not be nil")
	}
	const q = `INSERT INTO stores (id, code, name, currency, country, domain, is_default, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	_, err := r.db.ExecContext(ctx, q,
		s.ID, s.Code, s.Name, s.Currency, s.Country, s.Domain, s.IsDefault,
		s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			switch pqErr.Constraint {
			case "stores_code_key":
				return apperror.Conflict("store with this code already exists")
			case "stores_pkey":
				return apperror.Conflict("store with this id already exists")
			case "idx_stores_domain":
				return apperror.Conflict("store with this domain already exists")
			case "idx_stores_default":
				return apperror.Conflict("a default store already exists")
			default:
				return apperror.Conflict("store unique constraint violation")
			}
		}
		return fmt.Errorf("store_repo: create: %w", err)
	}
	return nil
}

// Update persists changes to an existing store.
func (r *StoreRepo) Update(ctx context.Context, s *store.Store) error {
	if s == nil {
		return fmt.Errorf("store_repo: update: store must not be nil")
	}
	newUpdatedAt := time.Now().UTC()
	const q = `UPDATE stores SET code = $1, name = $2, currency = $3, country = $4, domain = $5, is_default = $6, updated_at = $7 WHERE id = $8`
	res, err := r.db.ExecContext(ctx, q,
		s.Code, s.Name, s.Currency, s.Country, s.Domain, s.IsDefault,
		newUpdatedAt, s.ID,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			switch pqErr.Constraint {
			case "stores_code_key":
				return apperror.Conflict("store with this code already exists")
			case "idx_stores_domain":
				return apperror.Conflict("store with this domain already exists")
			case "idx_stores_default":
				return apperror.Conflict("a default store already exists")
			default:
				return apperror.Conflict("store unique constraint violation")
			}
		}
		return fmt.Errorf("store_repo: update: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store_repo: update: rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("store not found")
	}
	s.UpdatedAt = newUpdatedAt
	return nil
}
