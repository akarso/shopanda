package store

import "context"

// StoreRepository defines persistence operations for stores.
type StoreRepository interface {
	// FindByID returns a store by its ID.
	// Returns (nil, nil) when not found.
	FindByID(ctx context.Context, id string) (*Store, error)

	// FindByCode returns a store by its unique code.
	// Returns (nil, nil) when not found.
	FindByCode(ctx context.Context, code string) (*Store, error)

	// FindByDomain returns a store by its domain.
	// Returns (nil, nil) when not found.
	FindByDomain(ctx context.Context, domain string) (*Store, error)

	// FindDefault returns the default store.
	// Returns (nil, nil) when no default is configured.
	FindDefault(ctx context.Context) (*Store, error)

	// FindAll returns all stores ordered by name asc.
	FindAll(ctx context.Context) ([]Store, error)

	// Create persists a new store.
	Create(ctx context.Context, s *Store) error

	// Update persists changes to an existing store.
	Update(ctx context.Context, s *Store) error
}
