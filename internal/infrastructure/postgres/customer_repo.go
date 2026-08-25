package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/adminuser"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/jackc/pgx/v5/pgconn"
)

// Compile-time check that CustomerRepo implements customer and adminuser repositories.
var (
	_ customer.CustomerRepository = (*CustomerRepo)(nil)
	_ adminuser.Repository        = (*CustomerRepo)(nil)
)

// CustomerRepo implements customer.CustomerRepository using PostgreSQL.
type CustomerRepo struct {
	db *sql.DB
	tx *sql.Tx
}

// NewCustomerRepo returns a new CustomerRepo backed by db.
func NewCustomerRepo(db *sql.DB) (*CustomerRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewCustomerRepo: nil *sql.DB")
	}
	return &CustomerRepo{db: db, tx: nil}, nil
}

// WithTx returns a repo bound to the given transaction.
func (r *CustomerRepo) WithTx(tx *sql.Tx) customer.CustomerRepository {
	return &CustomerRepo{db: r.db, tx: tx}
}

// FindByID returns a customer by its ID.
// Returns (nil, nil) when not found.
func (r *CustomerRepo) FindByID(ctx context.Context, id string) (*customer.Customer, error) {
	const q = `SELECT id, email, first_name, last_name, password_hash, token_generation, email_verified_at, role, status, pending_email_nonce, created_at, updated_at
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

// FindByEmail returns a customer by email address (case-insensitive).
// Returns (nil, nil) when not found.
func (r *CustomerRepo) FindByEmail(ctx context.Context, email string) (*customer.Customer, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, nil
	}
	const q = `SELECT id, email, first_name, last_name, password_hash, token_generation, email_verified_at, role, status, pending_email_nonce, created_at, updated_at
		FROM customers WHERE LOWER(email) = $1`

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
	if !c.Role.IsValid() {
		return apperror.Validation("invalid customer role")
	}
	const q = `INSERT INTO customers (id, email, first_name, last_name, password_hash, token_generation, email_verified_at, role, status, pending_email_nonce, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

	_, err := r.exec(ctx, q,
		c.ID, c.Email, c.FirstName, c.LastName,
		c.PasswordHash, c.TokenGeneration, c.EmailVerifiedAt, string(c.Role), string(c.Status), c.PendingEmailNonce,
		c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if pgErr.ConstraintName == "customers_email_key" {
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
	if !c.Role.IsValid() {
		return apperror.Validation("invalid customer role")
	}
	updatedAt := time.Now().UTC()

	const q = `UPDATE customers
		SET email = $1, first_name = $2, last_name = $3,
			password_hash = $4, token_generation = $5, email_verified_at = $6, role = $7, status = $8, pending_email_nonce = $9, updated_at = $10
		WHERE id = $11`

	result, err := r.exec(ctx, q,
		c.Email, c.FirstName, c.LastName,
		c.PasswordHash, c.TokenGeneration, c.EmailVerifiedAt, string(c.Role), string(c.Status), c.PendingEmailNonce,
		updatedAt, c.ID,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
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

// ListCustomers returns a paginated slice of customers ordered by email.
func (r *CustomerRepo) ListCustomers(ctx context.Context, offset, limit int) ([]customer.Customer, error) {
	if offset < 0 {
		return nil, fmt.Errorf("customer_repo: list customers: negative offset")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("customer_repo: list customers: non-positive limit")
	}
	if limit > 100 {
		limit = 100
	}

	const q = `SELECT id, email, first_name, last_name, token_generation, email_verified_at, role, status, created_at, updated_at
		FROM customers ORDER BY email LIMIT $1 OFFSET $2`

	rows, err := r.query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("customer_repo: list customers: %w", err)
	}
	defer rows.Close()

	var customers []customer.Customer
	for rows.Next() {
		c, err := scanCustomerList(rows)
		if err != nil {
			return nil, fmt.Errorf("customer_repo: list customers: scan: %w", err)
		}
		customers = append(customers, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("customer_repo: list customers: rows: %w", err)
	}
	return customers, nil
}

// ListAdminUsers returns admin-capable users ordered by email.
func (r *CustomerRepo) ListAdminUsers(ctx context.Context, offset, limit int) ([]customer.Customer, error) {
	if offset < 0 {
		return nil, fmt.Errorf("customer_repo: list admin users: negative offset")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("customer_repo: list admin users: non-positive limit")
	}
	if limit > 100 {
		limit = 100
	}

	const q = `SELECT id, email, first_name, last_name, token_generation, email_verified_at, role, status, created_at, updated_at
		FROM customers
		WHERE role <> 'customer'
		ORDER BY email
		LIMIT $1 OFFSET $2`

	rows, err := r.query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("customer_repo: list admin users: %w", err)
	}
	defer rows.Close()

	var users []customer.Customer
	for rows.Next() {
		c, err := scanCustomerList(rows)
		if err != nil {
			return nil, fmt.Errorf("customer_repo: list admin users: scan: %w", err)
		}
		users = append(users, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("customer_repo: list admin users: rows: %w", err)
	}
	return users, nil
}

// HasActiveAdmin reports whether at least one active admin user exists.
func (r *CustomerRepo) HasActiveAdmin(ctx context.Context) (bool, error) {
	const q = `SELECT EXISTS(
		SELECT 1 FROM customers WHERE role = 'admin' AND status = 'active'
	)`

	var exists bool
	if err := r.queryRow(ctx, q).Scan(&exists); err != nil {
		return false, fmt.Errorf("customer_repo: has active admin: %w", err)
	}
	return exists, nil
}

// UpdateAdminUser atomically updates an admin user and enforces last-active-admin rules.
func (r *CustomerRepo) UpdateAdminUser(ctx context.Context, c *customer.Customer, priorRole customer.Role, priorStatus customer.Status, revokeSessions bool) error {
	if !c.Role.IsValid() {
		return apperror.Validation("invalid customer role")
	}
	updatedAt := time.Now().UTC()

	const q = `UPDATE customers SET
		first_name = $1,
		last_name = $2,
		role = $3,
		status = $4,
		token_generation = token_generation + CASE WHEN $5 THEN 1 ELSE 0 END,
		updated_at = $6
	WHERE id = $7
	AND (
		$8 <> 'admin' OR $9 <> 'active'
		OR ($3 = 'admin' AND $4 = 'active')
		OR (SELECT COUNT(*) FROM customers WHERE role = 'admin' AND status = 'active') > 1
	)`

	result, err := r.exec(ctx, q,
		c.FirstName, c.LastName, string(c.Role), string(c.Status),
		revokeSessions, updatedAt, c.ID,
		string(priorRole), string(priorStatus),
	)
	if err != nil {
		return fmt.Errorf("customer_repo: update admin user: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("customer_repo: update admin user rows affected: %w", err)
	}
	if rows == 0 {
		if priorRole == customer.RoleAdmin && priorStatus == customer.StatusActive &&
			(c.Role != customer.RoleAdmin || c.Status != customer.StatusActive) {
			return apperror.Validation("cannot remove the last active admin user")
		}
		return apperror.NotFound("customer not found")
	}
	c.UpdatedAt = updatedAt
	if revokeSessions {
		c.TokenGeneration++
	}
	return nil
}

// BumpTokenGeneration atomically increments the customer's token generation.
func (r *CustomerRepo) BumpTokenGeneration(ctx context.Context, customerID string) error {
	const q = `UPDATE customers SET token_generation = token_generation + 1, updated_at = $1 WHERE id = $2`

	result, err := r.exec(ctx, q, time.Now().UTC(), customerID)
	if err != nil {
		return fmt.Errorf("customer_repo: bump token generation: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("customer_repo: bump token generation rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("customer not found")
	}
	return nil
}

// ChangePasswordAndBumpTokenGeneration atomically updates the password hash and
// invalidates previously issued tokens.
func (r *CustomerRepo) ChangePasswordAndBumpTokenGeneration(ctx context.Context, customerID, passwordHash string) error {
	const q = `UPDATE customers
		SET password_hash = $1, token_generation = token_generation + 1, updated_at = $2
		WHERE id = $3`

	result, err := r.exec(ctx, q, passwordHash, time.Now().UTC(), customerID)
	if err != nil {
		return fmt.Errorf("customer_repo: change password and bump token generation: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("customer_repo: change password and bump token generation rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("customer not found")
	}
	return nil
}

// Delete removes a customer by ID.
func (r *CustomerRepo) Delete(ctx context.Context, id string) error {
	const q = `DELETE FROM customers WHERE id = $1`

	result, err := r.exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("customer_repo: delete: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("customer_repo: delete rows affected: %w", err)
	}
	if rows == 0 {
		return apperror.NotFound("customer not found")
	}
	return nil
}

// queryRow delegates to tx or db.
func (r *CustomerRepo) queryRow(ctx context.Context, q string, args ...interface{}) *sql.Row {
	if r.tx != nil {
		return r.tx.QueryRowContext(ctx, q, args...)
	}
	return r.db.QueryRowContext(ctx, q, args...)
}

// query delegates to tx or db.
func (r *CustomerRepo) query(ctx context.Context, q string, args ...interface{}) (*sql.Rows, error) {
	if r.tx != nil {
		return r.tx.QueryContext(ctx, q, args...)
	}
	return r.db.QueryContext(ctx, q, args...)
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
	var emailVerifiedAt sql.NullTime
	var role string
	var status string

	err := s.Scan(
		&c.ID, &c.Email, &c.FirstName, &c.LastName,
		&c.PasswordHash, &c.TokenGeneration, &emailVerifiedAt, &role, &status, &c.PendingEmailNonce, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if emailVerifiedAt.Valid {
		verifiedAt := emailVerifiedAt.Time.UTC()
		c.EmailVerifiedAt = &verifiedAt
	}

	if err := decodeCustomerEnums(&c, role, status); err != nil {
		return nil, err
	}
	return &c, nil
}

// scanCustomerList scans a row without password_hash (used by ListCustomers).
func scanCustomerList(s interface{ Scan(...interface{}) error }) (*customer.Customer, error) {
	var c customer.Customer
	var emailVerifiedAt sql.NullTime
	var role string
	var status string

	err := s.Scan(
		&c.ID, &c.Email, &c.FirstName, &c.LastName,
		&c.TokenGeneration, &emailVerifiedAt, &role, &status, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if emailVerifiedAt.Valid {
		verifiedAt := emailVerifiedAt.Time.UTC()
		c.EmailVerifiedAt = &verifiedAt
	}

	if err := decodeCustomerEnums(&c, role, status); err != nil {
		return nil, err
	}
	return &c, nil
}

// decodeCustomerEnums validates and sets Role and Status on c.
func decodeCustomerEnums(c *customer.Customer, role, status string) error {
	rl := customer.Role(role)
	if !rl.IsValid() {
		return fmt.Errorf("customer_repo: invalid role from database: %q", role)
	}
	c.Role = rl

	st := customer.Status(status)
	if !st.IsValid() {
		return fmt.Errorf("customer_repo: invalid status from database: %q", status)
	}
	c.Status = st
	return nil
}
