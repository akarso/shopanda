package tax

import "context"

// RateRepository defines persistence operations for tax rates.
type RateRepository interface {
	// FindByCountryClassAndStore returns the rate for a country+class+store tuple.
	// An empty storeID means the global/default rate.
	// Returns (nil, nil) when no rate exists.
	FindByCountryClassAndStore(ctx context.Context, country, class, storeID string) (*TaxRate, error)

	// ListByCountry returns all rates for a country, ordered by class.
	ListByCountry(ctx context.Context, country string) ([]TaxRate, error)

	// ListByClass returns every rate with the given class across countries
	// and store scopes, ordered by country then store_id.
	ListByClass(ctx context.Context, class string) ([]TaxRate, error)

	// Upsert creates or updates a rate for a country+class+store tuple.
	Upsert(ctx context.Context, r *TaxRate) error

	// CreateIfNotExists inserts a rate and leaves an existing
	// (country, class, store_id) row untouched. Returns true when inserted.
	CreateIfNotExists(ctx context.Context, r *TaxRate) (bool, error)

	// Delete removes a tax rate by ID.
	Delete(ctx context.Context, id string) error
}
