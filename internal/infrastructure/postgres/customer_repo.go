package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/lib/pq"
)

// Compile-time check that CustomerRepo implements customer.CustomerRepository.
var _ customer.CustomerRepository = (*CustomerRepo)(nil)

// CustomerRepo implements customer.CustomerRepository using PostgreSQL.
type CustomerRepo struct {
	db *sql.DB
	tx *sql.Tx
}

// NewCustomerRepo returns a new CustomerRepo backed by db.
func NewCustomerRepo(db *sql.DB) *CustomerRepo {
	return &CustomerRepo{db: db, tx: nil}
}

// WithTx returns a repo bound to the given transaction.
func (r *CustomerRepo) WithTx(tx *sql.Tx) customer.CustomerRepository {
	return &CustomerRepo{db: r.db, tx: tx}
}

// FindByID returns a customer by its ID.
// Returns (nil, nil) when not found.
func (r *CustomerRepo) FindByID(ctx context.Context, id string) (*customer.Customer, error) {
	const q = `SELECT id, email, first_name, last_name, password_hash, status, created_at, updated_at
		FROM customers WHERE id = $1`

	row := r.queryRow(ctx, q, id)
	c, err := scanCustomer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("customer_repo: find by id: %w", err)
	}
	return c, nil
}

// FindByEmail returns a customer by email address.
// Returns (nil, nil) when not found.
func (r *CustomerRepo) FindByEmail(ctx context.Context, email string) (*customer.Customer, error) {
	const q = `SELECT id, email, first_name, last_name, password_hash, status, created_at, updated_at
		FROM customers WHERE email = $1`

	row := r.queryRow(ctx, q, email)
	c, err := scanCustomer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("customer_repo: find by email: %w", err)
	}
	return c, nil
}

// Create persists a new customer.
func (r *CustomerRepo) Create(ctx context.Context, c *customer.Customer) error {
	const q = `INSERT INTO customers (id, email, first_name, last_name, password_hash, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := r.exec(ctx, q,
		c.ID, c.Email, c.FirstName, c.LastName,
		c.PasswordHash, string(c.Status),
		c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			if pqErr.Constraint == "customers_email_key" {
				return apperror.Conflict("customer with this email already exists")
			}
			return apperror.Conflict("customer with this id already exists")
		}
		return fmt.Errorf("customer_repo: create: %w", err)
	}
	return nil
}

// Update persists changes to an existing customer.
func (r *CustomerRepo) Update(ctx context.Context, c *customer.Customer) error {
	updatedAt := time.Now().UTC()

	const q = `UPDATE customers
		SET email = $1, first_name = $2, last_name = $3,
			password_hash = $4, status = $5, updated_at = $6
		WHERE id = $7`

	result, err := r.exec(ctx, q,
		c.Email, c.FirstName, c.LastName,
		c.PasswordHash, string(c.Status),
		updatedAt, c.ID,
	)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return apperror.Conflict("customer with this email already exists")
		}
		return fmt.Errorf("customer_repo: update: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("customer_repo: update rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("customer not found")
	}
	c.UpdatedAt = updatedAt
	return nil
}

// queryRow delegates to tx or db.
func (r *CustomerRepo) queryRow(ctx context.Context, q string, args ...interface{}) *sql.Row {
	if r.tx != nil {
		return r.tx.QueryRowContext(ctx, q, args...)
	}
	return r.db.QueryRowContext(ctx, q, args...)
}

// exec delegates to tx or db.
func (r *CustomerRepo) exec(ctx context.Context, q string, args ...interface{}) (sql.Result, error) {
	if r.tx != nil {
		return r.tx.ExecContext(ctx, q, args...)
	}
	return r.db.ExecContext(ctx, q, args...)
}

// scanCustomer reads a customer from a row scanner.
func scanCustomer(s interface{ Scan(...interface{}) error }) (*customer.Customer, error) {
	var c customer.Customer
	var status string

	err := s.Scan(
		&c.ID, &c.Email, &c.FirstName, &c.LastName,
		&c.PasswordHash, &status, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	st := customer.Status(status)
	if !st.IsValid() {
		return nil, fmt.Errorf("customer_repo: invalid status from database: %q", status)
	}
	c.Status = st
	return &c, nil
}
