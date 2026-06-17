package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// Compile-time check that CustomerAddressRepo implements customer.AddressRepository.
var _ customer.AddressRepository = (*CustomerAddressRepo)(nil)

// CustomerAddressRepo implements customer.AddressRepository using PostgreSQL.
type CustomerAddressRepo struct {
	db *sql.DB
}

// NewCustomerAddressRepo returns a new CustomerAddressRepo backed by db.
func NewCustomerAddressRepo(db *sql.DB) (*CustomerAddressRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewCustomerAddressRepo: nil *sql.DB")
	}
	return &CustomerAddressRepo{db: db}, nil
}

const customerAddressColumns = `id, customer_id, label, recipient, street, city, postcode, country, is_default, created_at, updated_at`

// ListByCustomer returns the customer's saved addresses, default first.
func (r *CustomerAddressRepo) ListByCustomer(ctx context.Context, customerID string) ([]customer.Address, error) {
	const q = `SELECT ` + customerAddressColumns + `
		FROM customer_addresses
		WHERE customer_id = $1
		ORDER BY is_default DESC, created_at ASC`

	rows, err := r.db.QueryContext(ctx, q, customerID)
	if err != nil {
		return nil, fmt.Errorf("customer_address_repo: list: %w", err)
	}
	defer rows.Close()

	var out []customer.Address
	for rows.Next() {
		a, err := scanCustomerAddress(rows)
		if err != nil {
			return nil, fmt.Errorf("customer_address_repo: list scan: %w", err)
		}
		out = append(out, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("customer_address_repo: list rows: %w", err)
	}
	return out, nil
}

// FindByID returns an address by its ID. Returns (nil, nil) when not found.
func (r *CustomerAddressRepo) FindByID(ctx context.Context, id string) (*customer.Address, error) {
	const q = `SELECT ` + customerAddressColumns + ` FROM customer_addresses WHERE id = $1`

	a, err := scanCustomerAddress(r.db.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("customer_address_repo: find by id: %w", err)
	}
	return a, nil
}

// FindDefault returns the customer's default address, or (nil, nil) when none.
func (r *CustomerAddressRepo) FindDefault(ctx context.Context, customerID string) (*customer.Address, error) {
	const q = `SELECT ` + customerAddressColumns + `
		FROM customer_addresses WHERE customer_id = $1 AND is_default LIMIT 1`

	a, err := scanCustomerAddress(r.db.QueryRowContext(ctx, q, customerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("customer_address_repo: find default: %w", err)
	}
	return a, nil
}

// Create persists a new address. The first address a customer saves, or any
// address created with IsDefault set, becomes their default.
func (r *CustomerAddressRepo) Create(ctx context.Context, a *customer.Address) error {
	if a == nil {
		return fmt.Errorf("customer_address_repo: create: address must not be nil")
	}
	if err := a.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	return r.withTx(ctx, func(tx *sql.Tx) error {
		// Serialize default management per customer so concurrent first-address
		// inserts cannot both decide they are the default.
		if err := lockCustomerForUpdate(ctx, tx, a.CustomerID); err != nil {
			return err
		}
		makeDefault := a.IsDefault
		if !makeDefault {
			var existing int
			if err := tx.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM customer_addresses WHERE customer_id = $1`, a.CustomerID,
			).Scan(&existing); err != nil {
				return fmt.Errorf("count: %w", err)
			}
			makeDefault = existing == 0
		}
		if makeDefault {
			if err := clearCustomerDefaultAddress(ctx, tx, a.CustomerID); err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		const q = `INSERT INTO customer_addresses (` + customerAddressColumns + `)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
		if _, err := tx.ExecContext(ctx, q,
			a.ID, a.CustomerID, a.Label, a.Recipient, a.Street, a.City, a.Postcode, a.Country, makeDefault, now, now,
		); err != nil {
			return fmt.Errorf("insert: %w", err)
		}
		a.IsDefault = makeDefault
		a.CreatedAt = now
		a.UpdatedAt = now
		return nil
	})
}

// Update persists changes to an existing address.
func (r *CustomerAddressRepo) Update(ctx context.Context, a *customer.Address) error {
	if a == nil {
		return fmt.Errorf("customer_address_repo: update: address must not be nil")
	}
	if err := a.Validate(); err != nil {
		return apperror.Validation(err.Error())
	}
	return r.withTx(ctx, func(tx *sql.Tx) error {
		if err := lockCustomerForUpdate(ctx, tx, a.CustomerID); err != nil {
			return err
		}
		if a.IsDefault {
			if err := clearCustomerDefaultAddress(ctx, tx, a.CustomerID); err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		const q = `UPDATE customer_addresses
			SET label = $3, recipient = $4, street = $5, city = $6, postcode = $7,
				country = $8, is_default = $9, updated_at = $10
			WHERE id = $1 AND customer_id = $2`
		res, err := tx.ExecContext(ctx, q,
			a.ID, a.CustomerID, a.Label, a.Recipient, a.Street, a.City, a.Postcode, a.Country, a.IsDefault, now,
		)
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("rows affected: %w", err)
		}
		if affected == 0 {
			return apperror.NotFound("address not found")
		}
		a.UpdatedAt = now
		return nil
	})
}

// SetDefault marks one address as the customer's default and clears the rest.
func (r *CustomerAddressRepo) SetDefault(ctx context.Context, customerID, addressID string) error {
	return r.withTx(ctx, func(tx *sql.Tx) error {
		if err := lockCustomerForUpdate(ctx, tx, customerID); err != nil {
			return err
		}
		if err := clearCustomerDefaultAddress(ctx, tx, customerID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE customer_addresses SET is_default = TRUE, updated_at = NOW()
				WHERE id = $1 AND customer_id = $2`,
			addressID, customerID,
		)
		if err != nil {
			return fmt.Errorf("set default: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("rows affected: %w", err)
		}
		if affected == 0 {
			return apperror.NotFound("address not found")
		}
		return nil
	})
}

// Delete removes an address owned by the customer.
func (r *CustomerAddressRepo) Delete(ctx context.Context, customerID, addressID string) error {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM customer_addresses WHERE id = $1 AND customer_id = $2`,
		addressID, customerID,
	)
	if err != nil {
		return fmt.Errorf("customer_address_repo: delete: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("customer_address_repo: delete rows affected: %w", err)
	}
	if affected == 0 {
		return apperror.NotFound("address not found")
	}
	return nil
}

// lockCustomerForUpdate takes a row lock on the parent customer, serializing all
// default-address management for that customer within concurrent transactions.
func lockCustomerForUpdate(ctx context.Context, tx *sql.Tx, customerID string) error {
	var locked string
	err := tx.QueryRowContext(ctx, `SELECT id FROM customers WHERE id = $1 FOR UPDATE`, customerID).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return apperror.NotFound("customer not found")
	}
	if err != nil {
		return fmt.Errorf("lock customer: %w", err)
	}
	return nil
}

func clearCustomerDefaultAddress(ctx context.Context, tx *sql.Tx, customerID string) error {
	if _, err := tx.ExecContext(ctx,
		`UPDATE customer_addresses SET is_default = FALSE, updated_at = NOW()
			WHERE customer_id = $1 AND is_default`, customerID,
	); err != nil {
		return fmt.Errorf("clear default: %w", err)
	}
	return nil
}

func (r *CustomerAddressRepo) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("customer_address_repo: begin tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return wrapCustomerAddressTxError(err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("customer_address_repo: commit: %w", err)
	}
	return nil
}

func wrapCustomerAddressTxError(err error) error {
	if apperror.Is(err, apperror.CodeNotFound) || apperror.Is(err, apperror.CodeValidation) {
		return err
	}
	return fmt.Errorf("customer_address_repo: %w", err)
}

func scanCustomerAddress(s interface{ Scan(...interface{}) error }) (*customer.Address, error) {
	var a customer.Address
	if err := s.Scan(
		&a.ID, &a.CustomerID, &a.Label, &a.Recipient, &a.Street, &a.City,
		&a.Postcode, &a.Country, &a.IsDefault, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &a, nil
}
