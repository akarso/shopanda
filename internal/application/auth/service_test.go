package auth_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/password"
)

// ── mock logger ──────────────────────────────────────────────────────────

type testLogger struct{}

func (l testLogger) Info(_ string, _ map[string]interface{})           {}
func (l testLogger) Warn(_ string, _ map[string]interface{})           {}
func (l testLogger) Error(_ string, _ error, _ map[string]interface{}) {}

// ── mock repo ────────────────────────────────────────────────────────────

type mockCustomerRepo struct {
	customers map[string]*customer.Customer
	byEmail   map[string]*customer.Customer
}

func newMockRepo() *mockCustomerRepo {
	return &mockCustomerRepo{
		customers: make(map[string]*customer.Customer),
		byEmail:   make(map[string]*customer.Customer),
	}
}

func (r *mockCustomerRepo) FindByID(_ context.Context, id string) (*customer.Customer, error) {
	c := r.customers[id]
	return c, nil
}

func (r *mockCustomerRepo) FindByEmail(_ context.Context, email string) (*customer.Customer, error) {
	c := r.byEmail[email]
	return c, nil
}

func (r *mockCustomerRepo) Create(_ context.Context, c *customer.Customer) error {
	r.customers[c.ID] = c
	r.byEmail[c.Email] = c
	return nil
}

func (r *mockCustomerRepo) Update(_ context.Context, c *customer.Customer) error {
	r.customers[c.ID] = c
	r.byEmail[c.Email] = c
	return nil
}

func (r *mockCustomerRepo) WithTx(_ *sql.Tx) customer.CustomerRepository {
	return r
}

// ── helpers ──────────────────────────────────────────────────────────────

func newTestService(repo *mockCustomerRepo) *auth.Service {
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	return auth.NewService(repo, issuer, testLogger{})
}

// ── Register tests ───────────────────────────────────────────────────────

func TestRegister_Success(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email:     "alice@example.com",
		Password:  "password123",
		FirstName: "Alice",
		LastName:  "Smith",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if out.CustomerID == "" {
		t.Error("expected non-empty customer ID")
	}
	if out.Token == "" {
		t.Error("expected non-empty token")
	}

	// Verify customer was persisted.
	c := repo.customers[out.CustomerID]
	if c == nil {
		t.Fatal("customer not in repo")
	}
	if c.Email != "alice@example.com" {
		t.Errorf("Email = %q, want alice@example.com", c.Email)
	}
	if c.FirstName != "Alice" {
		t.Errorf("FirstName = %q, want Alice", c.FirstName)
	}
	if c.PasswordHash == "" {
		t.Error("expected non-empty password hash")
	}
}

func TestRegister_EmptyEmail(t *testing.T) {
	svc := newTestService(newMockRepo())
	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "", Password: "password123",
	})
	if err == nil {
		t.Fatal("expected error for empty email")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeValidation {
		t.Errorf("expected validation error, got %v", err)
	}
}

func TestRegister_ShortPassword(t *testing.T) {
	svc := newTestService(newMockRepo())
	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "alice@example.com", Password: "short",
	})
	if err == nil {
		t.Fatal("expected error for short password")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeValidation {
		t.Errorf("expected validation error, got %v", err)
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	_, _ = svc.Register(context.Background(), auth.RegisterInput{
		Email: "dup@example.com", Password: "password123",
	})
	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "dup@example.com", Password: "password123",
	})
	if err == nil {
		t.Fatal("expected error for duplicate email")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeConflict {
		t.Errorf("expected conflict error, got %v", err)
	}
}

// ── Login tests ──────────────────────────────────────────────────────────

func TestLogin_Success(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	_, _ = svc.Register(context.Background(), auth.RegisterInput{
		Email: "bob@example.com", Password: "password123",
	})

	out, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "bob@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if out.Token == "" {
		t.Error("expected non-empty token")
	}
	if out.CustomerID == "" {
		t.Error("expected non-empty customer ID")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	_, _ = svc.Register(context.Background(), auth.RegisterInput{
		Email: "bob@example.com", Password: "password123",
	})

	_, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "bob@example.com", Password: "wrongpass",
	})
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeUnauthorized {
		t.Errorf("expected unauthorized error, got %v", err)
	}
}

func TestLogin_NonExistent(t *testing.T) {
	svc := newTestService(newMockRepo())

	_, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "nobody@example.com", Password: "password123",
	})
	if err == nil {
		t.Fatal("expected error for non-existent user")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeUnauthorized {
		t.Errorf("expected unauthorized error, got %v", err)
	}
}

func TestLogin_DisabledAccount(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, _ := svc.Register(context.Background(), auth.RegisterInput{
		Email: "disabled@example.com", Password: "password123",
	})
	c := repo.customers[out.CustomerID]
	_ = c.Disable()

	_, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "disabled@example.com", Password: "password123",
	})
	if err == nil {
		t.Fatal("expected error for disabled account")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeUnauthorized {
		t.Errorf("expected unauthorized error, got %v", err)
	}
}

func TestLogin_EmptyEmail(t *testing.T) {
	svc := newTestService(newMockRepo())
	_, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "", Password: "password123",
	})
	if err == nil {
		t.Fatal("expected error for empty email")
	}
}

func TestLogin_EmptyPassword(t *testing.T) {
	svc := newTestService(newMockRepo())
	_, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "bob@example.com", Password: "",
	})
	if err == nil {
		t.Fatal("expected error for empty password")
	}
}

// ── Me tests ─────────────────────────────────────────────────────────────

func TestMe_Success(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, _ := svc.Register(context.Background(), auth.RegisterInput{
		Email: "me@example.com", Password: "password123",
	})

	c, err := svc.Me(context.Background(), out.CustomerID)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if c.Email != "me@example.com" {
		t.Errorf("Email = %q, want me@example.com", c.Email)
	}
}

func TestMe_NotFound(t *testing.T) {
	svc := newTestService(newMockRepo())
	_, err := svc.Me(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent customer")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeNotFound {
		t.Errorf("expected not_found error, got %v", err)
	}
}

// ── Verify password is hashed ────────────────────────────────────────────

func TestRegister_PasswordIsHashed(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, _ := svc.Register(context.Background(), auth.RegisterInput{
		Email: "hash@example.com", Password: "password123",
	})

	c := repo.customers[out.CustomerID]
	if c.PasswordHash == "password123" {
		t.Error("password hash should not equal plaintext")
	}
	if err := password.Compare(c.PasswordHash, "password123"); err != nil {
		t.Errorf("password hash verification failed: %v", err)
	}
}
