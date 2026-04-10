package tax

import (
	"context"
	"database/sql"
)

// RateRepository defines persistence operations for tax rates.
type RateRepository interface {
	// FindByCountryAndClass returns the rate for a country+class pair.
	// Returns (nil, nil) when no rate exists.
	FindByCountryAndClass(ctx context.Context, country, class string) (*TaxRate, error)

	// ListByCountry returns all rates for a country, ordered by class.
	ListByCountry(ctx context.Context, country string) ([]TaxRate, error)

	// Upsert creates or updates a rate for a country+class pair.
	Upsert(ctx context.Context, r *TaxRate) error

	// Delete removes a tax rate by ID.
	Delete(ctx context.Context, id string) error

	// WithTx returns a repository bound to the given transaction.
	WithTx(tx *sql.Tx) RateRepository
}
