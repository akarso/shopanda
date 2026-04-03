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
	"github.com/akarso/shopanda/internal/platform/event"
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

func (r *mockCustomerRepo) BumpTokenGeneration(_ context.Context, customerID string) error {
	c := r.customers[customerID]
	if c == nil {
		return apperror.NotFound("customer not found")
	}
	c.BumpTokenGeneration()
	return nil
}

func (r *mockCustomerRepo) WithTx(_ *sql.Tx) customer.CustomerRepository {
	return r
}

// ── mock reset repo ──────────────────────────────────────────────────────

type mockResetRepo struct {
	tokens map[string]*customer.PasswordResetToken // keyed by token_hash
}

func newMockResetRepo() *mockResetRepo {
	return &mockResetRepo{tokens: make(map[string]*customer.PasswordResetToken)}
}

func (r *mockResetRepo) Create(_ context.Context, t *customer.PasswordResetToken) error {
	r.tokens[t.TokenHash] = t
	return nil
}

func (r *mockResetRepo) FindByTokenHash(_ context.Context, hash string) (*customer.PasswordResetToken, error) {
	return r.tokens[hash], nil
}

func (r *mockResetRepo) MarkUsed(_ context.Context, id string) error {
	for _, t := range r.tokens {
		if t.ID == id {
			now := time.Now().UTC()
			t.UsedAt = &now
			return nil
		}
	}
	return apperror.NotFound("reset token not found")
}

// ── helpers ──────────────────────────────────────────────────────────────

func newTestService(repo *mockCustomerRepo) *auth.Service {
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	bus := event.NewBus(testLogger{})
	return auth.NewService(repo, newMockResetRepo(), issuer, bus, testLogger{}, time.Hour)
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

// ── Register: ExpiresAt ──────────────────────────────────────────────────

func TestRegister_ExpiresAt(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "exp@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if out.ExpiresAt.IsZero() {
		t.Error("expected non-zero ExpiresAt")
	}
	if !out.ExpiresAt.After(time.Now()) {
		t.Error("ExpiresAt should be in the future")
	}
}

// ── Login: ExpiresAt ─────────────────────────────────────────────────────

func TestLogin_ExpiresAt(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	_, _ = svc.Register(context.Background(), auth.RegisterInput{
		Email: "exp@example.com", Password: "password123",
	})
	out, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "exp@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if out.ExpiresAt.IsZero() {
		t.Error("expected non-zero ExpiresAt")
	}
}

// ── Logout tests ─────────────────────────────────────────────────────────

func TestLogout_Success(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	out, _ := svc.Register(context.Background(), auth.RegisterInput{
		Email: "logout@example.com", Password: "password123",
	})

	err := svc.Logout(context.Background(), out.CustomerID)
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}

	// TokenGeneration should be bumped.
	c := repo.customers[out.CustomerID]
	if c.TokenGeneration != 1 {
		t.Errorf("TokenGeneration = %d, want 1", c.TokenGeneration)
	}
}

func TestLogout_NotFound(t *testing.T) {
	svc := newTestService(newMockRepo())
	err := svc.Logout(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent customer")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeNotFound {
		t.Errorf("expected not_found error, got %v", err)
	}
}

// ── RequestPasswordReset tests ───────────────────────────────────────────

func TestRequestPasswordReset_Success(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)

	_, _ = svc.Register(context.Background(), auth.RegisterInput{
		Email: "reset@example.com", Password: "password123",
	})

	err := svc.RequestPasswordReset(context.Background(), "reset@example.com")
	if err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
}

func TestRequestPasswordReset_NonExistent_NoError(t *testing.T) {
	svc := newTestService(newMockRepo())
	err := svc.RequestPasswordReset(context.Background(), "nobody@example.com")
	if err != nil {
		t.Fatalf("expected no error for non-existent email, got %v", err)
	}
}

func TestRequestPasswordReset_EmptyEmail(t *testing.T) {
	svc := newTestService(newMockRepo())
	err := svc.RequestPasswordReset(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty email")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeValidation {
		t.Errorf("expected validation error, got %v", err)
	}
}

// ── ConfirmPasswordReset tests ───────────────────────────────────────────

func TestConfirmPasswordReset_EmptyToken(t *testing.T) {
	svc := newTestService(newMockRepo())
	err := svc.ConfirmPasswordReset(context.Background(), auth.ConfirmPasswordResetInput{
		Token: "", NewPassword: "newpassword123",
	})
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestConfirmPasswordReset_ShortPassword(t *testing.T) {
	svc := newTestService(newMockRepo())
	err := svc.ConfirmPasswordReset(context.Background(), auth.ConfirmPasswordResetInput{
		Token: "some-token", NewPassword: "short",
	})
	if err == nil {
		t.Fatal("expected error for short password")
	}
}

func TestConfirmPasswordReset_InvalidToken(t *testing.T) {
	svc := newTestService(newMockRepo())
	err := svc.ConfirmPasswordReset(context.Background(), auth.ConfirmPasswordResetInput{
		Token: "invalid-token", NewPassword: "newpassword123",
	})
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeUnauthorized {
		t.Errorf("expected unauthorized error, got %v", err)
	}
}

func TestConfirmPasswordReset_Success(t *testing.T) {
	repo := newMockRepo()
	resetRepo := newMockResetRepo()
	issuer, _ := jwt.NewIssuer("test-secret", time.Hour)
	bus := event.NewBus(testLogger{})
	svc := auth.NewService(repo, resetRepo, issuer, bus, testLogger{}, time.Hour)

	// Register a customer.
	out, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "reset@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	c := repo.customers[out.CustomerID]
	oldHash := c.PasswordHash

	// Seed a valid reset token directly into the mock repo.
	plaintext := "test-reset-token"
	hash := customer.HashToken(plaintext)
	rt := &customer.PasswordResetToken{
		ID:         "rt-1",
		CustomerID: out.CustomerID,
		TokenHash:  hash,
		ExpiresAt:  time.Now().UTC().Add(time.Hour),
		CreatedAt:  time.Now().UTC(),
	}
	resetRepo.tokens[hash] = rt

	// Confirm the reset.
	err = svc.ConfirmPasswordReset(context.Background(), auth.ConfirmPasswordResetInput{
		Token:       plaintext,
		NewPassword: "new-password-123",
	})
	if err != nil {
		t.Fatalf("ConfirmPasswordReset: %v", err)
	}

	// Verify password was changed.
	c = repo.customers[out.CustomerID]
	if c.PasswordHash == oldHash {
		t.Error("expected password hash to change")
	}
	if err := password.Compare(c.PasswordHash, "new-password-123"); err != nil {
		t.Error("new password should verify")
	}

	// Verify token was marked used.
	if rt.UsedAt == nil {
		t.Error("expected reset token to be marked used")
	}

	// Verify token generation was bumped.
	if c.TokenGeneration < 1 {
		t.Errorf("TokenGeneration = %d, want >= 1", c.TokenGeneration)
	}
}
