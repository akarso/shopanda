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
		msg := "store identity constraint violation"
		if pgErr.ConstraintName != "" {
			msg = "store identity constraint violation: " + pgErr.ConstraintName
		}
		return apperror.Validation(msg)
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

type identityExec interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// SaveIdentity upserts the identity row.
func (r *StoreIdentityRepo) SaveIdentity(ctx context.Context, id store.Identity) error {
	return saveIdentity(ctx, r.db, id)
}

func saveIdentity(ctx context.Context, ex identityExec, id store.Identity) error {
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
	_, err := ex.ExecContext(ctx, q,
		id.StoreID, id.LegalName, id.Phone, id.Hours,
		id.EmailGeneral, id.EmailSales, id.EmailSupport, id.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store_identity_repo: save identity: %w", mapStoreIdentityExecError(err))
	}
	return nil
}

// MutateIdentity locks the identity row, applies fn, and saves in one transaction.
func (r *StoreIdentityRepo) MutateIdentity(ctx context.Context, storeID string, fn func(current *store.Identity) (store.Identity, error)) (store.Identity, error) {
	if storeID == "" {
		return store.Identity{}, fmt.Errorf("store_identity_repo: mutate identity: empty store id")
	}
	if fn == nil {
		return store.Identity{}, fmt.Errorf("store_identity_repo: mutate identity: nil fn")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Identity{}, fmt.Errorf("store_identity_repo: mutate identity: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Prove the store exists under the same tx before insert/upsert.
	var exists string
	err = tx.QueryRowContext(ctx, `SELECT id FROM stores WHERE id = $1 FOR SHARE`, storeID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Identity{}, apperror.NotFound("store not found")
	}
	if err != nil {
		return store.Identity{}, fmt.Errorf("store_identity_repo: mutate identity: store: %w", err)
	}

	const q = `
SELECT store_id, legal_name, phone, hours, email_general, email_sales, email_support, updated_at
FROM store_identities WHERE store_id = $1 FOR UPDATE`
	var (
		sid, legal, phone, hours, eg, es, eu string
		updatedAt                            time.Time
	)
	err = tx.QueryRowContext(ctx, q, storeID).Scan(
		&sid, &legal, &phone, &hours, &eg, &es, &eu, &updatedAt,
	)
	var current *store.Identity
	if errors.Is(err, sql.ErrNoRows) {
		current = nil
	} else if err != nil {
		return store.Identity{}, fmt.Errorf("store_identity_repo: mutate identity: lock: %w", err)
	} else {
		current = store.NewIdentityFromDB(sid, legal, phone, hours, eg, es, eu, updatedAt)
	}

	next, err := fn(current)
	if err != nil {
		return store.Identity{}, err
	}
	if err := saveIdentity(ctx, tx, next); err != nil {
		return store.Identity{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.Identity{}, fmt.Errorf("store_identity_repo: mutate identity: commit: %w", err)
	}
	return next, nil
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
	return saveAddress(ctx, r.db, addr)
}

func saveAddress(ctx context.Context, ex identityExec, addr store.Address) error {
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
	_, err := ex.ExecContext(ctx, q,
		addr.StoreID, string(addr.Role),
		addr.Line1, addr.Line2, addr.City, addr.Region, addr.Postcode, addr.Country, addr.Phone,
	)
	if err != nil {
		return fmt.Errorf("store_identity_repo: save address: %w", mapStoreIdentityExecError(err))
	}
	return nil
}

// MutateAddress locks the address row for role, applies fn, and saves in one transaction.
func (r *StoreIdentityRepo) MutateAddress(ctx context.Context, storeID string, role store.AddressRole, fn func(current *store.Address) (store.Address, error)) (store.Address, error) {
	if storeID == "" {
		return store.Address{}, fmt.Errorf("store_identity_repo: mutate address: empty store id")
	}
	if fn == nil {
		return store.Address{}, fmt.Errorf("store_identity_repo: mutate address: nil fn")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Address{}, fmt.Errorf("store_identity_repo: mutate address: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists string
	err = tx.QueryRowContext(ctx, `SELECT id FROM stores WHERE id = $1 FOR SHARE`, storeID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Address{}, apperror.NotFound("store not found")
	}
	if err != nil {
		return store.Address{}, fmt.Errorf("store_identity_repo: mutate address: store: %w", err)
	}

	const q = `
SELECT store_id, role, line1, line2, city, region, postcode, country, phone
FROM store_addresses WHERE store_id = $1 AND role = $2 FOR UPDATE`
	var (
		sid, roleStr, line1, line2, city, region, postcode, country, phone string
	)
	err = tx.QueryRowContext(ctx, q, storeID, string(role)).Scan(
		&sid, &roleStr, &line1, &line2, &city, &region, &postcode, &country, &phone,
	)
	var current *store.Address
	if errors.Is(err, sql.ErrNoRows) {
		current = nil
	} else if err != nil {
		return store.Address{}, fmt.Errorf("store_identity_repo: mutate address: lock: %w", err)
	} else {
		parsed, perr := store.ParseAddressRole(roleStr)
		if perr != nil {
			return store.Address{}, fmt.Errorf("store_identity_repo: mutate address: %w", perr)
		}
		current = store.NewAddressFromDB(sid, parsed, line1, line2, city, region, postcode, country, phone)
	}

	next, err := fn(current)
	if err != nil {
		return store.Address{}, err
	}
	if err := saveAddress(ctx, tx, next); err != nil {
		return store.Address{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.Address{}, fmt.Errorf("store_identity_repo: mutate address: commit: %w", err)
	}
	return next, nil
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

// DeleteTaxIdentifier removes a type for the store.
func (r *StoreIdentityRepo) DeleteTaxIdentifier(ctx context.Context, storeID, typ string) error {
	if storeID == "" {
		return fmt.Errorf("store_identity_repo: delete tax identifier: empty store id")
	}
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM store_tax_identifiers WHERE store_id = $1 AND type = $2`,
		storeID, typ,
	)
	if err != nil {
		return fmt.Errorf("store_identity_repo: delete tax identifier: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store_identity_repo: delete tax identifier: rows affected: %w", err)
	}
	if n == 0 {
		return apperror.NotFound("tax identifier not found")
	}
	return nil
}
