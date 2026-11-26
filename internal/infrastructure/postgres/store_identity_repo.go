package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

// Compile-time check that StoreIdentityRepo implements store.IdentityRepository.
var _ store.IdentityRepository = (*StoreIdentityRepo)(nil)

// StoreIdentityRepo persists store identity, addresses, and tax identifiers.
type StoreIdentityRepo struct {
	db *sql.DB
}

// NewStoreIdentityRepo returns a StoreIdentityRepo backed by db.
func NewStoreIdentityRepo(db *sql.DB) (*StoreIdentityRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewStoreIdentityRepo: nil *sql.DB")
	}
	return &StoreIdentityRepo{db: db}, nil
}

func mapStoreIdentityExecError(err error) error {
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
		return apperror.Validation("store identity constraint violation")
	default:
		return err
	}
}

// GetIdentity returns the identity row, or (nil, nil) when missing.
func (r *StoreIdentityRepo) GetIdentity(ctx context.Context, storeID string) (*store.Identity, error) {
	if storeID == "" {
		return nil, fmt.Errorf("store_identity_repo: get identity: empty store id")
	}
	const q = `
SELECT store_id, legal_name, phone, hours, email_general, email_sales, email_support, updated_at
FROM store_identities WHERE store_id = $1`
	var (
		sid, legal, phone, hours, eg, es, eu string
		updatedAt                            time.Time
	)
	err := r.db.QueryRowContext(ctx, q, storeID).Scan(
		&sid, &legal, &phone, &hours, &eg, &es, &eu, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store_identity_repo: get identity: %w", err)
	}
	return store.NewIdentityFromDB(sid, legal, phone, hours, eg, es, eu, updatedAt), nil
}

// SaveIdentity upserts the identity row.
func (r *StoreIdentityRepo) SaveIdentity(ctx context.Context, id store.Identity) error {
	const q = `
INSERT INTO store_identities (
    store_id, legal_name, phone, hours, email_general, email_sales, email_support, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (store_id) DO UPDATE SET
    legal_name = EXCLUDED.legal_name,
    phone = EXCLUDED.phone,
    hours = EXCLUDED.hours,
    email_general = EXCLUDED.email_general,
    email_sales = EXCLUDED.email_sales,
    email_support = EXCLUDED.email_support,
    updated_at = EXCLUDED.updated_at`
	_, err := r.db.ExecContext(ctx, q,
		id.StoreID, id.LegalName, id.Phone, id.Hours,
		id.EmailGeneral, id.EmailSales, id.EmailSupport, id.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store_identity_repo: save identity: %w", mapStoreIdentityExecError(err))
	}
	return nil
}

// GetAddress returns one role address, or (nil, nil) when missing.
func (r *StoreIdentityRepo) GetAddress(ctx context.Context, storeID string, role store.AddressRole) (*store.Address, error) {
	if storeID == "" {
		return nil, fmt.Errorf("store_identity_repo: get address: empty store id")
	}
	const q = `
SELECT store_id, role, line1, line2, city, region, postcode, country, phone
FROM store_addresses WHERE store_id = $1 AND role = $2`
	var (
		sid, roleStr, line1, line2, city, region, postcode, country, phone string
	)
	err := r.db.QueryRowContext(ctx, q, storeID, string(role)).Scan(
		&sid, &roleStr, &line1, &line2, &city, &region, &postcode, &country, &phone,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store_identity_repo: get address: %w", err)
	}
	parsed, err := store.ParseAddressRole(roleStr)
	if err != nil {
		return nil, fmt.Errorf("store_identity_repo: get address: %w", err)
	}
	return store.NewAddressFromDB(sid, parsed, line1, line2, city, region, postcode, country, phone), nil
}

// ListAddresses returns persisted addresses only (no placeholders).
func (r *StoreIdentityRepo) ListAddresses(ctx context.Context, storeID string) ([]store.Address, error) {
	if storeID == "" {
		return nil, fmt.Errorf("store_identity_repo: list addresses: empty store id")
	}
	const q = `
SELECT store_id, role, line1, line2, city, region, postcode, country, phone
FROM store_addresses WHERE store_id = $1 ORDER BY role`
	rows, err := r.db.QueryContext(ctx, q, storeID)
	if err != nil {
		return nil, fmt.Errorf("store_identity_repo: list addresses: %w", err)
	}
	defer rows.Close()

	out := make([]store.Address, 0, 4)
	for rows.Next() {
		var (
			sid, roleStr, line1, line2, city, region, postcode, country, phone string
		)
		if err := rows.Scan(&sid, &roleStr, &line1, &line2, &city, &region, &postcode, &country, &phone); err != nil {
			return nil, fmt.Errorf("store_identity_repo: list addresses: scan: %w", err)
		}
		parsed, err := store.ParseAddressRole(roleStr)
		if err != nil {
			return nil, fmt.Errorf("store_identity_repo: list addresses: %w", err)
		}
		addr := store.NewAddressFromDB(sid, parsed, line1, line2, city, region, postcode, country, phone)
		out = append(out, *addr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store_identity_repo: list addresses: %w", err)
	}
	return out, nil
}

// SaveAddress upserts one role address.
func (r *StoreIdentityRepo) SaveAddress(ctx context.Context, addr store.Address) error {
	const q = `
INSERT INTO store_addresses (
    store_id, role, line1, line2, city, region, postcode, country, phone
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (store_id, role) DO UPDATE SET
    line1 = EXCLUDED.line1,
    line2 = EXCLUDED.line2,
    city = EXCLUDED.city,
    region = EXCLUDED.region,
    postcode = EXCLUDED.postcode,
    country = EXCLUDED.country,
    phone = EXCLUDED.phone`
	_, err := r.db.ExecContext(ctx, q,
		addr.StoreID, string(addr.Role),
		addr.Line1, addr.Line2, addr.City, addr.Region, addr.Postcode, addr.Country, addr.Phone,
	)
	if err != nil {
		return fmt.Errorf("store_identity_repo: save address: %w", mapStoreIdentityExecError(err))
	}
	return nil
}

// ListTaxIdentifiers returns all tax IDs for a store.
func (r *StoreIdentityRepo) ListTaxIdentifiers(ctx context.Context, storeID string) ([]store.TaxIdentifier, error) {
	if storeID == "" {
		return nil, fmt.Errorf("store_identity_repo: list tax identifiers: empty store id")
	}
	const q = `
SELECT store_id, type, value FROM store_tax_identifiers
WHERE store_id = $1 ORDER BY type`
	rows, err := r.db.QueryContext(ctx, q, storeID)
	if err != nil {
		return nil, fmt.Errorf("store_identity_repo: list tax identifiers: %w", err)
	}
	defer rows.Close()

	out := make([]store.TaxIdentifier, 0)
	for rows.Next() {
		var sid, typ, value string
		if err := rows.Scan(&sid, &typ, &value); err != nil {
			return nil, fmt.Errorf("store_identity_repo: list tax identifiers: scan: %w", err)
		}
		ti := store.NewTaxIdentifierFromDB(sid, typ, value)
		out = append(out, *ti)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store_identity_repo: list tax identifiers: %w", err)
	}
	return out, nil
}

// UpsertTaxIdentifier inserts or updates by (store_id, type).
func (r *StoreIdentityRepo) UpsertTaxIdentifier(ctx context.Context, ti store.TaxIdentifier) error {
	const q = `
INSERT INTO store_tax_identifiers (store_id, type, value)
VALUES ($1, $2, $3)
ON CONFLICT (store_id, type) DO UPDATE SET value = EXCLUDED.value`
	_, err := r.db.ExecContext(ctx, q, ti.StoreID, ti.Type, ti.Value)
	if err != nil {
		return fmt.Errorf("store_identity_repo: upsert tax identifier: %w", mapStoreIdentityExecError(err))
	}
	return nil
}

// DeleteTaxIdentifier removes a type for the store (no-op if missing).
func (r *StoreIdentityRepo) DeleteTaxIdentifier(ctx context.Context, storeID, typ string) error {
	if storeID == "" {
		return fmt.Errorf("store_identity_repo: delete tax identifier: empty store id")
	}
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM store_tax_identifiers WHERE store_id = $1 AND type = $2`,
		storeID, typ,
	)
	if err != nil {
		return fmt.Errorf("store_identity_repo: delete tax identifier: %w", err)
	}
	return nil
}
