package admin_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// authEnvelope duplicates the storefront auth_test.go response envelope.
type authEnvelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// authTestLogger, authMockCustomerRepo, and authMockResetRepo duplicate the
// storefront auth_test.go stubs — unexported, so they can't be shared across
// the http_test/admin_test package boundary created by the admin package
// split.

type authTestLogger struct{}

func (l authTestLogger) Info(_ string, _ map[string]interface{})           {}
func (l authTestLogger) Warn(_ string, _ map[string]interface{})           {}
func (l authTestLogger) Error(_ string, _ error, _ map[string]interface{}) {}

type authMockCustomerRepo struct {
	customers map[string]*customer.Customer
	byEmail   map[string]*customer.Customer
}

func newAuthMockRepo() *authMockCustomerRepo {
	return &authMockCustomerRepo{
		customers: make(map[string]*customer.Customer),
		byEmail:   make(map[string]*customer.Customer),
	}
}

func (r *authMockCustomerRepo) FindByID(_ context.Context, id string) (*customer.Customer, error) {
	return r.customers[id], nil
}

func (r *authMockCustomerRepo) FindByEmail(_ context.Context, email string) (*customer.Customer, error) {
	return r.byEmail[email], nil
}

func (r *authMockCustomerRepo) Create(_ context.Context, c *customer.Customer) error {
	r.customers[c.ID] = c
	r.byEmail[c.Email] = c
	return nil
}

func (r *authMockCustomerRepo) Update(_ context.Context, c *customer.Customer) error {
	r.customers[c.ID] = c
	r.byEmail[c.Email] = c
	return nil
}

func (r *authMockCustomerRepo) ListCustomers(_ context.Context, _, _ int) ([]customer.Customer, error) {
	return nil, nil
}

func (r *authMockCustomerRepo) BumpTokenGeneration(_ context.Context, customerID string) error {
	c := r.customers[customerID]
	if c == nil {
		return apperror.NotFound("customer not found")
	}
	c.BumpTokenGeneration()
	return nil
}

func (r *authMockCustomerRepo) ChangePasswordAndBumpTokenGeneration(_ context.Context, customerID, passwordHash string) error {
	c := r.customers[customerID]
	if c == nil {
		return apperror.NotFound("customer not found")
	}
	c.PasswordHash = passwordHash
	c.BumpTokenGeneration()
	return nil
}

func (r *authMockCustomerRepo) WithTx(_ *sql.Tx) customer.CustomerRepository {
	return r
}

func (r *authMockCustomerRepo) Delete(_ context.Context, id string) error {
	c := r.customers[id]
	if c != nil {
		delete(r.byEmail, c.Email)
	}
	delete(r.customers, id)
	return nil
}

type authMockResetRepo struct {
	tokens map[string]*customer.PasswordResetToken
}

func newAuthMockResetRepo() *authMockResetRepo {
	return &authMockResetRepo{tokens: make(map[string]*customer.PasswordResetToken)}
}

func (r *authMockResetRepo) Create(_ context.Context, t *customer.PasswordResetToken) error {
	r.tokens[t.TokenHash] = t
	return nil
}

func (r *authMockResetRepo) FindByTokenHash(_ context.Context, hash string) (*customer.PasswordResetToken, error) {
	return r.tokens[hash], nil
}

func (r *authMockResetRepo) MarkUsed(_ context.Context, id string) error {
	for _, t := range r.tokens {
		if t.ID == id {
			now := time.Now().UTC()
			t.UsedAt = &now
			return nil
		}
	}
	return apperror.NotFound("reset token not found")
}
